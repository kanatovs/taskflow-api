package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/config"
)

func TestRedisConfiguration(t *testing.T) {
	unsetConfigEnv(t)
	t.Setenv("REDIS_ADDR", "localhost:6379")
	t.Setenv("REDIS_PASSWORD", "test-only")
	t.Setenv("REDIS_DB", "2")
	t.Setenv("CACHE_TTL", "30s")
	t.Setenv("REDIS_DIAL_TIMEOUT", "50ms")
	t.Setenv("REDIS_OPERATION_TIMEOUT", "20ms")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := config.Redis{Addr: "localhost:6379", Password: "test-only", DB: 2, TTL: 30 * time.Second, DialTimeout: 50 * time.Millisecond, OperationTimeout: 20 * time.Millisecond}
	if cfg.Redis != want {
		t.Fatal("Redis configuration differs from explicit settings")
	}
}

func TestInvalidRedisConfiguration(t *testing.T) {
	for _, setting := range []struct{ name, value string }{
		{"REDIS_ADDR", "bad"}, {"REDIS_ADDR", "localhost:0"},
		{"REDIS_ADDR", ":6379"},
		{"REDIS_DB", "-1"}, {"REDIS_DB", "16"}, {"REDIS_DB", ""}, {"REDIS_DB", "invalid"},
		{"CACHE_TTL", "0s"}, {"CACHE_TTL", "-1s"}, {"CACHE_TTL", "1ns"}, {"CACHE_TTL", "invalid"},
		{"REDIS_DIAL_TIMEOUT", "0s"}, {"REDIS_OPERATION_TIMEOUT", "-1s"},
	} {
		t.Run(setting.name+"/"+setting.value, func(t *testing.T) {
			unsetConfigEnv(t)
			t.Setenv(setting.name, setting.value)
			_, err := config.Load()
			if err == nil || !strings.Contains(err.Error(), setting.name) {
				t.Fatalf("expected safe %s validation error", setting.name)
			}
		})
	}
}
