package handler_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/handler"
)

func TestRouter(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantAllow  string
		wantBody   string
	}{
		{
			name:       "public health endpoint",
			method:     http.MethodGet,
			path:       "/health",
			wantStatus: http.StatusOK,
			wantBody:   `{"data":{"status":"ok"}}`,
		},
		{
			name:       "unsupported method",
			method:     http.MethodPost,
			path:       "/health",
			wantStatus: http.StatusMethodNotAllowed,
			wantAllow:  http.MethodGet,
			wantBody:   `{"error":{"code":"method_not_allowed","message":"Method not allowed","details":{}}}`,
		},
		{
			name:       "method not recognized by Chi",
			method:     "BREW",
			path:       "/health",
			wantStatus: http.StatusMethodNotAllowed,
			wantAllow:  http.MethodGet,
			wantBody:   `{"error":{"code":"method_not_allowed","message":"Method not allowed","details":{}}}`,
		},
		{
			name:       "unknown path",
			method:     http.MethodGet,
			path:       "/unknown",
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":{"code":"not_found","message":"Route not found","details":{}}}`,
		},
		{
			name:       "unsupported method on an unknown path",
			method:     http.MethodPost,
			path:       "/unknown",
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":{"code":"not_found","message":"Route not found","details":{}}}`,
		},
		{
			name:       "health subpath",
			method:     http.MethodGet,
			path:       "/health/extra",
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":{"code":"not_found","message":"Route not found","details":{}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, nil)
			recorder := httptest.NewRecorder()

			handler.NewRouter(5*time.Second, slog.New(slog.NewJSONHandler(io.Discard, nil)), nil, nil, nil, nil).ServeHTTP(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if got := recorder.Header().Get("Allow"); got != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
			}
			if got := recorder.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			assertRouterBody(t, recorder.Body.Bytes(), tt.wantBody)
		})
	}
}

func TestRouterErrorsOverHTTP(t *testing.T) {
	server := httptest.NewServer(handler.NewRouter(5*time.Second, slog.New(slog.NewJSONHandler(io.Discard, nil)), nil, nil, nil, nil))
	defer server.Close()
	client := server.Client()
	client.Timeout = 5 * time.Second

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantAllow  string
		wantBody   string
	}{
		{
			name:       "unknown path",
			method:     http.MethodGet,
			path:       "/unknown",
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":{"code":"not_found","message":"Route not found","details":{}}}`,
		},
		{
			name:       "unsupported method",
			method:     http.MethodPost,
			path:       "/health",
			wantStatus: http.StatusMethodNotAllowed,
			wantAllow:  http.MethodGet,
			wantBody:   `{"error":{"code":"method_not_allowed","message":"Method not allowed","details":{}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request, err := http.NewRequest(tt.method, server.URL+tt.path, nil)
			if err != nil {
				t.Fatalf("create request: %v", err)
			}
			response, err := client.Do(request)
			if err != nil {
				t.Fatalf("send request: %v", err)
			}
			defer response.Body.Close()

			if response.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", response.StatusCode, tt.wantStatus)
			}
			if got := response.Header.Get("Allow"); got != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
			}
			if got := response.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatalf("read response: %v", err)
			}
			assertRouterBody(t, body, tt.wantBody)
		})
	}
}

func assertRouterBody(t *testing.T, body []byte, wantBody string) {
	t.Helper()
	var got, want any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if err := json.Unmarshal([]byte(wantBody), &want); err != nil {
		t.Fatalf("decode expected response: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %#v, want %#v", got, want)
	}
}

func TestRouterHealthOverHTTP(t *testing.T) {
	server := httptest.NewServer(handler.NewRouter(5*time.Second, slog.New(slog.NewJSONHandler(io.Discard, nil)), nil, nil, nil, nil))
	defer server.Close()

	client := server.Client()
	client.Timeout = 5 * time.Second
	response, err := client.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if got := response.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read health response: %v", err)
	}
	var got map[string]map[string]string
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if got["data"]["status"] != "ok" {
		t.Errorf("health status = %q, want ok", got["data"]["status"])
	}
}
