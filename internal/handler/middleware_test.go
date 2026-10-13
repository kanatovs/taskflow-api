package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestLogging(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusNoContent} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			request := httptest.NewRequest(http.MethodPost, "/test?token=query-secret", strings.NewReader("body-secret"))
			request.Header.Set("Authorization", "Bearer header-secret")
			request.Header.Set("X-Request-ID", "untrusted-secret")
			type contextKey struct{}
			request = request.WithContext(context.WithValue(request.Context(), contextKey{}, "preserved"))
			writer := httptest.NewRecorder()
			body := ""
			h := requestMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Context().Value(contextKey{}) != "preserved" {
					t.Error("middleware lost the request context")
				}
				if status != http.StatusOK {
					w.WriteHeader(status)
				}
				if status != http.StatusNoContent {
					body = "response-secret"
					_, _ = io.WriteString(w, body)
				}
			}))
			h.ServeHTTP(writer, request)

			entries := decodeLogs(t, &logs)
			if len(entries) != 1 {
				t.Fatalf("log count = %d, want 1", len(entries))
			}
			entry := entries[0]
			requestID := writer.Header().Get("X-Request-ID")
			if requestID == "" || requestID == "untrusted-secret" || entry["request_id"] != requestID {
				t.Errorf("request ID = %q, log ID = %v", requestID, entry["request_id"])
			}
			if entry["method"] != http.MethodPost || entry["path"] != "/test" || entry["status"] != float64(status) {
				t.Errorf("unexpected request fields: %v", entry)
			}
			if entry["response_bytes"] != float64(len(body)) || entry["aborted"] != false {
				t.Errorf("unexpected response fields: %v", entry)
			}
			if duration, ok := entry["duration_ms"].(float64); !ok || duration < 0 {
				t.Errorf("duration_ms = %v, want non-negative number", entry["duration_ms"])
			}
			if entry["level"] != "INFO" || entry["msg"] != "HTTP request completed" {
				t.Errorf("unexpected log event: %v", entry)
			}
			if strings.Contains(logs.String(), "secret") {
				t.Errorf("log contains sensitive request or response data: %s", logs.String())
			}
		})
	}
}

func TestRouterUsesRequestLogging(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	NewRouter(logger, nil).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
	entries := decodeLogs(t, &logs)
	if len(entries) != 1 || entries[0]["status"] != float64(http.StatusOK) {
		t.Fatalf("router logs = %v, want a successful request event", entries)
	}
}

func TestRecoverPanicOverHTTP(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := requestMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/panic" {
			w.Header().Set("Content-Length", "9999")
			w.Header().Set("Content-Encoding", "gzip")
			panic("panic-secret")
		}
		Health(w, r)
	}))
	server := httptest.NewServer(h)
	defer server.Close()
	client := server.Client()
	client.Timeout = 5 * time.Second

	response, err := client.Get(server.URL + "/panic")
	if err != nil {
		t.Fatalf("GET /panic: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatalf("read panic response: %v", err)
	}
	if response.StatusCode != http.StatusInternalServerError || response.Header.Get("Content-Type") != "application/json" {
		t.Errorf("panic response = %d, headers = %v", response.StatusCode, response.Header)
	}
	if strings.TrimSpace(string(body)) != `{"error":{"code":"internal_error","message":"Internal server error","details":{}}}` {
		t.Errorf("panic body = %s", body)
	}
	requestID := response.Header.Get("X-Request-ID")

	response, err = client.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("GET after panic: %v", err)
	}
	_, err = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("server did not survive panic: status = %d, error = %v", response.StatusCode, err)
	}
	server.Close()
	entries := decodeLogs(t, &logs)
	if len(entries) != 3 {
		t.Fatalf("log count = %d, want panic, failed request, successful request", len(entries))
	}
	stack, ok := entries[0]["stack"].(string)
	if entries[0]["level"] != "ERROR" || !ok || stack == "" || entries[0]["request_id"] != requestID {
		t.Errorf("panic log = %v", entries[0])
	}
	if entries[1]["status"] != float64(http.StatusInternalServerError) || entries[1]["request_id"] != requestID {
		t.Errorf("request log = %v", entries[1])
	}
	if response.Header.Get("X-Request-ID") == requestID {
		t.Error("different requests received the same ID")
	}
	if strings.Contains(logs.String(), "panic-secret") {
		t.Error("panic value leaked into logs")
	}
}

func TestInvalidStatusPanicReturnsJSON(t *testing.T) {
	writer := httptest.NewRecorder()
	h := requestMiddleware(slog.New(slog.NewJSONHandler(io.Discard, nil)))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(99)
	}))
	h.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/test", nil))
	assertJSONResponse(t, writer, http.StatusInternalServerError,
		`{"error":{"code":"internal_error","message":"Internal server error","details":{}}}`)
}

func TestPanicAbortsPartialResponseOverHTTP(t *testing.T) {
	h := requestMiddleware(slog.New(slog.NewJSONHandler(io.Discard, nil)))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/panic" {
			_, _ = io.WriteString(w, "partial")
			if err := http.NewResponseController(w).Flush(); err != nil {
				return
			}
			panic("internal failure")
		}
		Health(w, r)
	}))
	server := httptest.NewServer(h)
	defer server.Close()
	client := server.Client()
	client.Timeout = 5 * time.Second
	response, err := client.Get(server.URL + "/panic")
	if err != nil {
		t.Fatalf("GET /panic: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if !errors.Is(err, io.ErrUnexpectedEOF) || string(body) != "partial" || response.StatusCode != http.StatusOK {
		t.Fatalf("partial response was not aborted: status = %d, body = %q, error = %v", response.StatusCode, body, err)
	}
	response, err = client.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("GET after aborted response: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("server failed after abort: status = %d", response.StatusCode)
	}
}

func TestPanicAfterResponseStarts(t *testing.T) {
	tests := []struct {
		name  string
		start func(*testing.T, http.ResponseWriter)
	}{
		{name: "headers", start: func(_ *testing.T, w http.ResponseWriter) { w.WriteHeader(http.StatusOK) }},
		{name: "body", start: func(_ *testing.T, w http.ResponseWriter) { _, _ = io.WriteString(w, "partial") }},
		{name: "flush", start: func(t *testing.T, w http.ResponseWriter) {
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Fatalf("flush: %v", err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			writer := &trackingResponseWriter{ResponseRecorder: httptest.NewRecorder()}
			h := requestMiddleware(slog.New(slog.NewJSONHandler(&logs, nil)))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				tt.start(t, w)
				panic("internal failure")
			}))
			func() {
				defer func() {
					if value := recover(); value != http.ErrAbortHandler {
						t.Errorf("panic = %v, want http.ErrAbortHandler", value)
					}
				}()
				h.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/test", nil))
				t.Error("middleware did not abort the started response")
			}()
			if len(writer.statuses) != 1 || writer.statuses[0] != http.StatusOK || writer.writes > 1 {
				t.Errorf("response was rewritten: statuses = %v, writes = %d", writer.statuses, writer.writes)
			}
			if strings.Contains(writer.Body.String(), "internal_error") {
				t.Error("error JSON was appended to the partial response")
			}
			entries := decodeLogs(t, &logs)
			if len(entries) != 2 || entries[1]["aborted"] != true || entries[1]["status"] != float64(http.StatusOK) {
				t.Errorf("panic logs = %v", entries)
			}
		})
	}
}

func TestIntentionalAbortIsPreserved(t *testing.T) {
	var logs bytes.Buffer
	writer := &trackingResponseWriter{ResponseRecorder: httptest.NewRecorder()}
	h := requestMiddleware(slog.New(slog.NewJSONHandler(&logs, nil)))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	func() {
		defer func() {
			if value := recover(); value != http.ErrAbortHandler {
				t.Errorf("panic = %v, want http.ErrAbortHandler", value)
			}
		}()
		h.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/test", nil))
		t.Error("intentional abort was swallowed")
	}()
	entries := decodeLogs(t, &logs)
	if writer.writes != 0 || len(writer.statuses) != 0 || len(entries) != 1 || entries[0]["aborted"] != true || entries[0]["status"] != float64(0) {
		t.Errorf("intentional abort generated a response or panic log: writer = %+v, logs = %v", writer, entries)
	}
}

func TestResponseErrorsUseRequestLogger(t *testing.T) {
	tests := []struct {
		name         string
		writeFailure bool
	}{
		{name: "serialization"},
		{name: "write", writeFailure: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			writer := &trackingResponseWriter{ResponseRecorder: httptest.NewRecorder()}
			if tt.writeFailure {
				writer.writeErr = errors.New("connection closed")
			}
			h := requestMiddleware(slog.New(slog.NewJSONHandler(&logs, nil)))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var data any = failingMarshaler{}
				if tt.writeFailure {
					data = "ok"
				}
				writeData(w, r, http.StatusOK, data)
			}))
			h.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/test", nil))
			entries := decodeLogs(t, &logs)
			if len(entries) != 2 || entries[0]["level"] != "ERROR" || entries[0]["request_id"] != writer.Header().Get("X-Request-ID") {
				t.Errorf("response failure did not use the request logger: %v", entries)
			}
		})
	}
}

func decodeLogs(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var entries []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(logs.Bytes()))
	for {
		var entry map[string]any
		if err := decoder.Decode(&entry); err != nil {
			if errors.Is(err, io.EOF) {
				return entries
			}
			t.Fatalf("decode structured log: %v", err)
		}
		entries = append(entries, entry)
	}
}
