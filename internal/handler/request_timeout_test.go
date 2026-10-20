package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/service"
)

func TestRequestTimeoutPreservesSuccessAndCancelsContext(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusNoContent} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			type contextKey struct{}
			parent := context.WithValue(context.Background(), contextKey{}, "preserved")
			request := httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(parent)
			writer := httptest.NewRecorder()
			var requestContext context.Context
			started := time.Now()
			h := requestTimeout(time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestContext = r.Context()
				if r.Context().Value(contextKey{}) != "preserved" {
					t.Error("request context values were lost")
				}
				deadline, ok := r.Context().Deadline()
				if !ok || deadline.Before(started.Add(time.Minute)) || deadline.After(time.Now().Add(time.Minute)) {
					t.Errorf("request deadline = %v, set = %v", deadline, ok)
				}
				w.Header().Set("X-Test", "preserved")
				if status == http.StatusNoContent {
					w.WriteHeader(status)
					return
				}
				writeData(w, r, status, "ok")
			}))
			h.ServeHTTP(writer, request)
			if writer.Code != status || writer.Header().Get("X-Test") != "preserved" {
				t.Fatalf("successful response changed: status = %d, headers = %v", writer.Code, writer.Header())
			}
			if status == http.StatusNoContent {
				if writer.Body.Len() != 0 {
					t.Fatalf("204 body = %q, want empty", writer.Body.String())
				}
			} else {
				assertJSONResponse(t, writer, status, `{"data":"ok"}`)
			}
			if !errors.Is(requestContext.Err(), context.Canceled) || parent.Err() != nil {
				t.Fatalf("request error = %v, parent error = %v", requestContext.Err(), parent.Err())
			}
		})
	}
}

func TestRequestTimeoutSharesDeadlineAcrossOperations(t *testing.T) {
	writer := httptest.NewRecorder()
	h := requestTimeout(50 * time.Millisecond)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		if !ok {
			t.Fatal("request has no deadline")
		}
		first, cancelFirst := context.WithTimeout(r.Context(), time.Hour)
		firstDeadline, _ := first.Deadline()
		cancelFirst()
		second, cancelSecond := context.WithTimeout(r.Context(), time.Hour)
		defer cancelSecond()
		secondDeadline, _ := second.Deadline()
		if !firstDeadline.Equal(deadline) || !secondDeadline.Equal(deadline) {
			t.Fatalf("operation deadlines = %v and %v, want shared %v", firstDeadline, secondDeadline, deadline)
		}
		<-second.Done()
		if !errors.Is(second.Err(), context.DeadlineExceeded) || !errors.Is(r.Context().Err(), context.DeadlineExceeded) {
			t.Fatalf("operation error = %v, request error = %v", second.Err(), r.Context().Err())
		}
	}))
	h.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/test", nil))
	assertRequestTimeoutResponse(t, writer)
}

func TestRequestTimeoutPreservesParentDeadline(t *testing.T) {
	deadline := time.Now().Add(time.Hour)
	parent, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	writer := httptest.NewRecorder()
	h := requestTimeout(2 * time.Hour)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := r.Context().Deadline()
		if !ok || !got.Equal(deadline) {
			t.Fatalf("request deadline = %v, want parent deadline %v", got, deadline)
		}
		writeData(w, r, http.StatusOK, "ok")
	}))
	h.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(parent))
	assertJSONResponse(t, writer, http.StatusOK, `{"data":"ok"}`)
	if parent.Err() != nil {
		t.Fatalf("middleware canceled the parent: %v", parent.Err())
	}
}

func TestRequestTimeoutPreservesClientCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	writer := httptest.NewRecorder()
	h := requestTimeout(time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !errors.Is(r.Context().Err(), context.Canceled) {
			t.Fatalf("request error = %v, want client cancellation", r.Context().Err())
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	h.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(parent))
	if writer.Code != http.StatusNoContent || writer.Body.Len() != 0 {
		t.Fatalf("client cancellation was changed to a timeout: status = %d, body = %q", writer.Code, writer.Body.String())
	}
}

func TestRequestTimeoutRejectsLateResponses(t *testing.T) {
	tests := []struct {
		name  string
		write func(*testing.T, http.ResponseWriter, *http.Request)
	}{
		{name: "created JSON", write: func(_ *testing.T, w http.ResponseWriter, r *http.Request) {
			writeData(w, r, http.StatusCreated, "late-result-secret")
		}},
		{name: "no content", write: func(_ *testing.T, w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}},
		{name: "body", write: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
			if n, err := io.WriteString(w, "late-result-secret"); n != 0 || !errors.Is(err, http.ErrHandlerTimeout) {
				t.Errorf("late write = %d, %v; want 0, ErrHandlerTimeout", n, err)
			}
		}},
		{name: "empty handler", write: func(_ *testing.T, _ http.ResponseWriter, _ *http.Request) {}},
		{name: "flush", write: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
			if err := http.NewResponseController(w).Flush(); !errors.Is(err, http.ErrHandlerTimeout) {
				t.Errorf("late flush error = %v, want ErrHandlerTimeout", err)
			}
		}},
		{name: "repeated writes", write: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			w.WriteHeader(http.StatusNoContent)
			for range 2 {
				if n, err := w.Write([]byte("late-result-secret")); n != 0 || !errors.Is(err, http.ErrHandlerTimeout) {
					t.Errorf("late write = %d, %v; want 0, ErrHandlerTimeout", n, err)
				}
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			defer cancel()
			var logs bytes.Buffer
			writer := httptest.NewRecorder()
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			h := requestMiddleware(logger)(requestTimeout(time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !errors.Is(r.Context().Err(), context.DeadlineExceeded) {
					t.Fatal("expired parent deadline was not preserved")
				}
				w.Header().Set("Content-Length", "9999")
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Set("ETag", "stale-secret")
				w.Header().Set("WWW-Authenticate", "Bearer")
				w.Header().Set("Retry-After", "60")
				w.Header().Set("Cache-Control", "public, max-age=60")
				tt.write(t, w, r)
			})))
			h.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(parent))
			assertRequestTimeoutResponse(t, writer)
			for _, name := range []string{"Content-Length", "Content-Encoding", "ETag", "WWW-Authenticate", "Retry-After"} {
				if got := writer.Header().Get(name); got != "" {
					t.Errorf("timeout retained stale %s = %q", name, got)
				}
			}
			requestID := writer.Header().Get("X-Request-ID")
			entries := decodeLogs(t, &logs)
			if len(entries) == 0 {
				t.Fatal("timeout request produced no completion log")
			}
			entry := entries[len(entries)-1]
			if requestID == "" || entry["request_id"] != requestID || entry["status"] != float64(http.StatusServiceUnavailable) || entry["aborted"] != false {
				t.Errorf("timeout completion log = %v, response request ID = %q", entry, requestID)
			}
			if strings.Contains(writer.Body.String(), "secret") || strings.Contains(logs.String(), "secret") {
				t.Error("late response data or stale headers leaked into timeout output")
			}
		})
	}
}

func TestRequestTimeoutAbortsStartedResponse(t *testing.T) {
	for _, tt := range []struct {
		name  string
		start func(*testing.T, http.ResponseWriter)
		late  func(http.ResponseWriter)
	}{
		{name: "headers", start: func(_ *testing.T, w http.ResponseWriter) { w.WriteHeader(http.StatusOK) },
			late: func(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }},
		{name: "body", start: func(_ *testing.T, w http.ResponseWriter) { _, _ = io.WriteString(w, "partial") },
			late: func(w http.ResponseWriter) { _, _ = io.WriteString(w, "late") }},
		{name: "flush", start: func(t *testing.T, w http.ResponseWriter) {
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Fatalf("initial flush: %v", err)
			}
		}, late: func(w http.ResponseWriter) { _ = http.NewResponseController(w).Flush() }},
		{name: "return", start: func(_ *testing.T, w http.ResponseWriter) { _, _ = io.WriteString(w, "partial") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			writer := httptest.NewRecorder()
			h := requestMiddleware(slog.New(slog.NewJSONHandler(&logs, nil)))(requestTimeout(250 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tt.start(t, w)
				<-r.Context().Done()
				if tt.late != nil {
					tt.late(w)
				}
				// Returning must also abort when the handler does not write again.
			})))
			func() {
				defer func() {
					if value := recover(); value != http.ErrAbortHandler {
						t.Errorf("panic = %v, want ErrAbortHandler", value)
					}
				}()
				h.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/test", nil))
				t.Error("middleware did not abort the started response")
			}()
			if writer.Code != http.StatusOK || strings.Contains(writer.Body.String(), "request_timeout") {
				t.Errorf("started response was rewritten: status = %d, body = %q", writer.Code, writer.Body.String())
			}
			entries := decodeLogs(t, &logs)
			if len(entries) != 1 || entries[0]["status"] != float64(http.StatusOK) || entries[0]["aborted"] != true {
				t.Errorf("aborted request logs = %v", entries)
			}
		})
	}
}

func TestRequestTimeoutPreservesPanics(t *testing.T) {
	for _, tt := range []struct {
		name    string
		expired bool
		value   any
	}{
		{name: "panic before deadline", value: "panic-secret"},
		{name: "panic after deadline", expired: true, value: "panic-secret"},
		{name: "abort before deadline", value: http.ErrAbortHandler},
		{name: "abort after deadline", expired: true, value: http.ErrAbortHandler},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parent := context.Background()
			if tt.expired {
				var cancel context.CancelFunc
				parent, cancel = context.WithDeadline(parent, time.Now().Add(-time.Second))
				defer cancel()
			}
			h := requestTimeout(time.Minute)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(tt.value) }))
			defer func() {
				if got := recover(); got != tt.value {
					t.Errorf("panic = %v, want original %v", got, tt.value)
				}
			}()
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(parent))
			t.Error("middleware swallowed the original panic")
		})
	}
}

func TestRequestTimeoutKeepsPanicRecoveryAfterDeadline(t *testing.T) {
	parent, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	var logs bytes.Buffer
	writer := httptest.NewRecorder()
	h := requestMiddleware(slog.New(slog.NewJSONHandler(&logs, nil)))(requestTimeout(time.Minute)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("panic-secret")
	})))
	h.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(parent))
	assertJSONResponse(t, writer, http.StatusInternalServerError,
		`{"error":{"code":"internal_error","message":"Internal server error","details":{}}}`)
	entries := decodeLogs(t, &logs)
	if len(entries) != 2 || entries[0]["level"] != "ERROR" || entries[1]["status"] != float64(http.StatusInternalServerError) || entries[1]["aborted"] != false {
		t.Errorf("panic recovery logs = %v", entries)
	}
	if strings.Contains(logs.String(), "panic-secret") || strings.Contains(writer.Body.String(), "panic-secret") {
		t.Error("panic value leaked into the response or logs")
	}
}

func TestRequestTimeoutKeepsLocalOperationTimeoutAsInternalError(t *testing.T) {
	var logs bytes.Buffer
	writer := httptest.NewRecorder()
	h := requestMiddleware(slog.New(slog.NewJSONHandler(&logs, nil)))(requestTimeout(time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		operation, cancel := context.WithDeadline(r.Context(), time.Now().Add(-time.Second))
		defer cancel()
		if r.Context().Err() != nil {
			t.Fatalf("HTTP context expired before the local operation: %v", r.Context().Err())
		}
		writeServiceError(w, r, "test operation", operation.Err())
	})))
	h.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/test", nil))
	assertJSONResponse(t, writer, http.StatusInternalServerError,
		`{"error":{"code":"internal_error","message":"Internal server error","details":{}}}`)
	entries := decodeLogs(t, &logs)
	if len(entries) != 2 || entries[0]["error_kind"] != "deadline_exceeded" || entries[1]["status"] != float64(http.StatusInternalServerError) {
		t.Errorf("local operation timeout logs = %v", entries)
	}
}

type timeoutAuthService struct{}

func (*timeoutAuthService) Register(context.Context, service.RegisterInput) (domain.User, error) {
	return domain.User{ID: 1}, nil
}

func (*timeoutAuthService) Login(ctx context.Context, _ service.LoginInput) (domain.AccessToken, error) {
	<-ctx.Done()
	return domain.AccessToken{}, ctx.Err()
}

func (*timeoutAuthService) Me(context.Context, int64) (domain.User, error) {
	return domain.User{ID: 1}, nil
}

func TestRequestTimeoutOverHTTP(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	router := NewRouter(50*time.Millisecond, logger, nil, NewAuthHandler(&timeoutAuthService{}, nil), nil, nil)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 5 * time.Second
	response, err := client.Post(server.URL+"/api/v1/auth/login", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read timeout response = %v, close = %v", readErr, closeErr)
	}
	if response.StatusCode != http.StatusServiceUnavailable || response.Header.Get("Content-Type") != "application/json" || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Request-ID") == "" {
		t.Fatalf("timeout status = %d, headers = %v", response.StatusCode, response.Header)
	}
	if strings.TrimSpace(string(body)) != `{"error":{"code":"request_timeout","message":"Request processing timed out","details":{}}}` {
		t.Fatalf("timeout body = %s", body)
	}
	response, err = client.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("health request after timeout: %v", err)
	}
	_, readErr = io.Copy(io.Discard, response.Body)
	closeErr = response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("health after timeout: status = %d, read = %v, close = %v", response.StatusCode, readErr, closeErr)
	}
}

func assertRequestTimeoutResponse(t *testing.T, writer *httptest.ResponseRecorder) {
	t.Helper()
	assertJSONResponse(t, writer, http.StatusServiceUnavailable,
		`{"error":{"code":"request_timeout","message":"Request processing timed out","details":{}}}`)
	if got := writer.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("timeout Cache-Control = %q, want no-store", got)
	}
}
