package cache

import (
	"context"
	"errors"
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

func TestRedisContextDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Accept a connection but never answer the Redis handshake.
		var buffer [4096]byte
		for {
			if _, err := conn.Read(buffer[:]); err != nil {
				return
			}
		}
	}()
	r := New(config.Redis{Addr: listener.Addr().String(), DialTimeout: 100 * time.Millisecond, OperationTimeout: 50 * time.Millisecond})
	t.Cleanup(func() { r.Close(); listener.Close(); <-done })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, _, err := r.Get(ctx, "test"); err == nil {
		t.Fatal("unresponsive server succeeded")
	}
	if time.Since(start) > time.Second {
		t.Fatal("context deadline did not bound Redis wait")
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatal("parent deadline not reached")
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
	if err := r.Set(ctx, key, []byte("expires"), 25*time.Millisecond); err != nil {
		t.Fatal("set expiring key failed")
	}
	deadline := time.Now().Add(time.Second)
	for {
		_, hit, err := r.Get(ctx, key)
		if err != nil {
			t.Fatal("expiration read failed")
		}
		if !hit {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("TTL did not expire")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
