package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
)

func TestWriteData(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	recorder := httptest.NewRecorder()
	recorder.Header().Set("X-Test", "preserved")

	writeData(recorder, request, http.StatusCreated, map[string]string{
		"message": "quoted \"text\"\nnext line",
	})

	assertJSONResponse(t, recorder, http.StatusCreated, `{"data":{"message":"quoted \"text\"\nnext line"}}`)
	if got := recorder.Header().Get("X-Test"); got != "preserved" {
		t.Errorf("X-Test = %q, want preserved", got)
	}
}

func TestWriteError(t *testing.T) {
	tests := []struct {
		name    string
		details map[string]any
		want    string
	}{
		{
			name: "nil details become an empty object",
			want: `{"error":{"code":"not_found","message":"Route not found","details":{}}}`,
		},
		{
			name:    "empty details remain an object",
			details: map[string]any{},
			want:    `{"error":{"code":"not_found","message":"Route not found","details":{}}}`,
		},
		{
			name:    "provided details are preserved",
			details: map[string]any{"resource": "route"},
			want:    `{"error":{"code":"not_found","message":"Route not found","details":{"resource":"route"}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/unknown", nil)
			recorder := httptest.NewRecorder()

			writeError(recorder, request, http.StatusNotFound, "not_found", "Route not found", tt.details)

			assertJSONResponse(t, recorder, http.StatusNotFound, tt.want)
		})
	}
}

func TestResponseSerializationFailure(t *testing.T) {
	tests := []struct {
		name  string
		write func(http.ResponseWriter, *http.Request)
	}{
		{
			name: "unsupported data type",
			write: func(w http.ResponseWriter, r *http.Request) {
				writeData(w, r, http.StatusOK, map[string]any{"value": func() {}})
			},
		},
		{
			name: "custom marshaler failure",
			write: func(w http.ResponseWriter, r *http.Request) {
				writeData(w, r, http.StatusCreated, failingMarshaler{})
			},
		},
		{
			name: "unsupported error details",
			write: func(w http.ResponseWriter, r *http.Request) {
				writeError(w, r, http.StatusNotFound, "not_found", "Route not found", map[string]any{"value": func() {}})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/health", nil)
			writer := &trackingResponseWriter{ResponseRecorder: httptest.NewRecorder()}

			tt.write(writer, request)

			assertJSONResponse(t, writer.ResponseRecorder, http.StatusInternalServerError,
				`{"error":{"code":"internal_error","message":"Internal server error","details":{}}}`)
			if !reflect.DeepEqual(writer.statuses, []int{http.StatusInternalServerError}) {
				t.Errorf("status writes = %v, want [500]", writer.statuses)
			}
			if writer.writes != 1 {
				t.Errorf("body writes = %d, want 1", writer.writes)
			}
		})
	}
}

func TestResponseWriteFailure(t *testing.T) {
	for _, partialBytes := range []int{0, 5} {
		t.Run(strconv.Itoa(partialBytes)+" bytes written", func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/health", nil)
			writer := &trackingResponseWriter{
				ResponseRecorder: httptest.NewRecorder(),
				writeErr:         errors.New("connection closed"),
				partialBytes:     partialBytes,
			}

			writeData(writer, request, http.StatusOK, map[string]string{"status": "ok"})

			if !reflect.DeepEqual(writer.statuses, []int{http.StatusOK}) {
				t.Errorf("status writes = %v, want [200]", writer.statuses)
			}
			if writer.writes != 1 {
				t.Errorf("body writes = %d, want 1; failed writes must not be retried", writer.writes)
			}
			if got := writer.Body.Len(); got != partialBytes {
				t.Errorf("body length = %d, want %d", got, partialBytes)
			}
		})
	}
}

func assertJSONResponse(t *testing.T, recorder *httptest.ResponseRecorder, status int, wantBody string) {
	t.Helper()
	if recorder.Code != status {
		t.Errorf("status = %d, want %d", recorder.Code, status)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var got, want any
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if err := json.Unmarshal([]byte(wantBody), &want); err != nil {
		t.Fatalf("decode expected response: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %#v, want %#v", got, want)
	}
}

type failingMarshaler struct{}

func (failingMarshaler) MarshalJSON() ([]byte, error) {
	return nil, errors.New("internal encoder detail")
}

type trackingResponseWriter struct {
	*httptest.ResponseRecorder
	statuses     []int
	writes       int
	writeErr     error
	partialBytes int
}

func (w *trackingResponseWriter) WriteHeader(status int) {
	w.statuses = append(w.statuses, status)
	w.ResponseRecorder.WriteHeader(status)
}

func (w *trackingResponseWriter) Write(body []byte) (int, error) {
	w.writes++
	if w.writeErr != nil {
		n, _ := w.ResponseRecorder.Write(body[:w.partialBytes])
		return n, w.writeErr
	}
	return w.ResponseRecorder.Write(body)
}
