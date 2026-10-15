package cache

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/config"
)

func TestRedisUnavailableAndClose(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	r := New(config.Redis{Addr: addr, DialTimeout: 20 * time.Millisecond, OperationTimeout: 30 * time.Millisecond})
	start := time.Now()
	if _, _, err := r.Get(context.Background(), "test"); err == nil {
		t.Fatal("unavailable Redis succeeded")
	}
	if time.Since(start) > time.Second {
		t.Fatal("Redis call exceeded bounded timeout")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Get(context.Background(), "test"); err == nil {
		t.Fatal("closed client succeeded")
	}
}

func TestRedisIntegration(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set TEST_REDIS_ADDR to isolated local Redis")
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || (host != "127.0.0.1" && host != "localhost") {
		t.Fatal("test Redis must be local")
	}
	r := New(config.Redis{Addr: addr, DialTimeout: time.Second, OperationTimeout: time.Second})
	t.Cleanup(func() { r.Close() })
	ctx := context.Background()
	key := fmt.Sprintf("taskflow:test:%d", time.Now().UnixNano())
	t.Cleanup(func() {
		if err := r.Delete(context.Background(), key); err != nil {
			t.Error("test key cleanup failed")
		}
	})
	if _, hit, err := r.Get(ctx, key); err != nil || hit {
		t.Fatalf("miss: %v", err)
	}
	if err := r.Set(ctx, key, []byte("value"), time.Minute); err != nil {
		t.Fatal("set failed")
	}
	if value, hit, err := r.Get(ctx, key); err != nil || !hit || string(value) != "value" {
		t.Fatal("get failed")
	}
	if ttl, err := r.client.PTTL(ctx, key).Result(); err != nil || ttl <= 0 || ttl > time.Minute {
		t.Fatal("explicit TTL lost")
	}
	if err := r.Delete(ctx, key); err != nil {
		t.Fatal("delete failed")
	}
	if _, hit, err := r.Get(ctx, key); err != nil || hit {
		t.Fatal("deleted key still present")
	}
}
