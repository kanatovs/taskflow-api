package config_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/config"
)

func TestLoad(t *testing.T) {
	defaults := config.Config{
		Redis:                 config.Redis{TTL: time.Minute, DialTimeout: 200 * time.Millisecond, OperationTimeout: 100 * time.Millisecond},
		JWTSecret:             "unit-test-secret-at-least-32-bytes",
		JWTAccessTTL:          15 * time.Minute,
		DatabaseURL:           "postgres://test:test@127.0.0.1:5432/test",
		DatabaseTimeout:       3 * time.Second,
		HTTPAddr:              ":8080",
		HTTPReadHeaderTimeout: 5 * time.Second,
		HTTPReadTimeout:       10 * time.Second,
		HTTPRequestTimeout:    5 * time.Second,
		HTTPWriteTimeout:      10 * time.Second,
		HTTPIdleTimeout:       60 * time.Second,
		HTTPShutdownTimeout:   10 * time.Second,
	}
	partial := defaults
	partial.HTTPAddr = "localhost:8081"
	partial.HTTPWriteTimeout = 1500 * time.Millisecond
	partial.HTTPRequestTimeout = time.Second

	tests := []struct {
		name string
		env  map[string]string
		want config.Config
	}{
		{
			name: "unset variables use defaults",
			want: defaults,
		},
		{
			name: "all settings overridden",
			env: map[string]string{
				"HTTP_ADDR":                "127.0.0.1:9090",
				"HTTP_READ_HEADER_TIMEOUT": "2s",
				"HTTP_READ_TIMEOUT":        "15s",
				"HTTP_REQUEST_TIMEOUT":     "8s",
				"HTTP_WRITE_TIMEOUT":       "20s",
				"HTTP_IDLE_TIMEOUT":        "1m30s",
				"HTTP_SHUTDOWN_TIMEOUT":    "25s",
			},
			want: config.Config{
				Redis:                 config.Redis{TTL: time.Minute, DialTimeout: 200 * time.Millisecond, OperationTimeout: 100 * time.Millisecond},
				JWTSecret:             "unit-test-secret-at-least-32-bytes",
				JWTAccessTTL:          15 * time.Minute,
				DatabaseURL:           "postgres://test:test@127.0.0.1:5432/test",
				DatabaseTimeout:       3 * time.Second,
				HTTPAddr:              "127.0.0.1:9090",
				HTTPReadHeaderTimeout: 2 * time.Second,
				HTTPReadTimeout:       15 * time.Second,
				HTTPRequestTimeout:    8 * time.Second,
				HTTPWriteTimeout:      20 * time.Second,
				HTTPIdleTimeout:       90 * time.Second,
				HTTPShutdownTimeout:   25 * time.Second,
			},
		},
		{
			name: "partial overrides preserve other defaults",
			env: map[string]string{
				"HTTP_ADDR":            "localhost:8081",
				"HTTP_REQUEST_TIMEOUT": "1s",
				"HTTP_WRITE_TIMEOUT":   "1500ms",
			},
			want: partial,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetConfigEnv(t)
			for name, value := range tt.env {
				t.Setenv(name, value)
			}

			got, err := config.Load()
			if err != nil {
				t.Fatalf("Load(): %v", err)
			}
			if got != tt.want {
				t.Errorf("config = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadValidHTTPAddr(t *testing.T) {
	for _, addr := range []string{
		":8080",
		"127.0.0.1:8081",
		"[::1]:8081",
		"localhost:8081",
		":1",
		":65535",
	} {
		t.Run(addr, func(t *testing.T) {
			unsetConfigEnv(t)
			t.Setenv("HTTP_ADDR", addr)

			got, err := config.Load()
			if err != nil {
				t.Fatalf("Load(): %v", err)
			}
			if got.HTTPAddr != addr {
				t.Errorf("HTTPAddr = %q, want %q", got.HTTPAddr, addr)
			}
		})
	}
}

func TestLoadInvalidHTTPAddr(t *testing.T) {
	tests := []struct {
		name string
		addr string
	}{
		{name: "empty", addr: ""},
		{name: "whitespace", addr: " \t"},
		{name: "leading whitespace", addr: " :8080"},
		{name: "trailing whitespace", addr: ":8080 "},
		{name: "host whitespace", addr: "local host:8080"},
		{name: "unicode whitespace", addr: "localhost\u00a0:8080"},
		{name: "missing separator", addr: "localhost"},
		{name: "missing port", addr: "localhost:"},
		{name: "service name", addr: ":http"},
		{name: "zero port", addr: ":0"},
		{name: "negative port", addr: ":-1"},
		{name: "signed port", addr: ":+8080"},
		{name: "port too large", addr: ":65536"},
		{name: "port overflow", addr: ":999999999999999999999"},
		{name: "unbracketed IPv6", addr: "::1:8080"},
		{name: "URL instead of address", addr: "http://localhost:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetConfigEnv(t)
			t.Setenv("HTTP_ADDR", tt.addr)
			assertLoadError(t, "HTTP_ADDR")
		})
	}
}

func TestLoadInvalidTimeout(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "whitespace", value: " "},
		{name: "invalid format", value: "invalid"},
		{name: "missing unit", value: "10"},
		{name: "zero", value: "0s"},
		{name: "negative", value: "-1s"},
		{name: "overflow", value: "999999999999999999999s"},
	}

	for _, name := range []string{
		"HTTP_READ_HEADER_TIMEOUT",
		"HTTP_READ_TIMEOUT",
		"HTTP_REQUEST_TIMEOUT",
		"HTTP_WRITE_TIMEOUT",
		"HTTP_IDLE_TIMEOUT",
		"HTTP_SHUTDOWN_TIMEOUT",
	} {
		t.Run(name, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					unsetConfigEnv(t)
					t.Setenv(name, tt.value)
					assertLoadError(t, name)
				})
			}
		})
	}
}

func TestLoadRequestTimeoutMustBeLessThanWriteTimeout(t *testing.T) {
	for _, requestTimeout := range []string{"10s", "11s"} {
		t.Run(requestTimeout, func(t *testing.T) {
			unsetConfigEnv(t)
			t.Setenv("HTTP_REQUEST_TIMEOUT", requestTimeout)

			got, err := config.Load()
			if err == nil {
				t.Fatal("Load() succeeded with request timeout at least as long as write timeout")
			}
			for _, name := range []string{"HTTP_REQUEST_TIMEOUT", "HTTP_WRITE_TIMEOUT"} {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("error = %q, want variable name %q", err, name)
				}
			}
			if got != (config.Config{}) {
				t.Errorf("config = %+v, want zero config on error", got)
			}
		})
	}
}

func assertLoadError(t *testing.T, name string) {
	t.Helper()
	got, err := config.Load()
	if err == nil {
		t.Fatalf("Load() succeeded with invalid %s", name)
	}
	if !strings.Contains(err.Error(), name) {
		t.Errorf("error = %q, want variable name %q", err, name)
	}
	if got != (config.Config{}) {
		t.Errorf("config = %+v, want zero config on error", got)
	}
}

func unsetConfigEnv(t *testing.T) {
	t.Helper()
	t.Setenv("JWT_SECRET", "unit-test-secret-at-least-32-bytes")
	t.Setenv("JWT_ACCESS_TTL", "15m")
	t.Setenv("DATABASE_URL", "postgres://test:test@127.0.0.1:5432/test")
	t.Setenv("DATABASE_TIMEOUT", "3s")
	for _, name := range []string{
		"HTTP_ADDR",
		"REDIS_ADDR", "REDIS_PASSWORD", "REDIS_DB", "CACHE_TTL", "REDIS_DIAL_TIMEOUT", "REDIS_OPERATION_TIMEOUT",
		"HTTP_READ_HEADER_TIMEOUT",
		"HTTP_READ_TIMEOUT",
		"HTTP_REQUEST_TIMEOUT",
		"HTTP_WRITE_TIMEOUT",
		"HTTP_IDLE_TIMEOUT",
		"HTTP_SHUTDOWN_TIMEOUT",
	} {
		// Setenv registers restoration of the original value before Unsetenv.
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}
}
