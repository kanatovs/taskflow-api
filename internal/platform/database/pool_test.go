package database

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestNewInvalidConfiguration(t *testing.T) {
	pool, err := New(context.Background(), "postgres://private%ZZ@host/db", time.Second)
	if pool != nil || err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("unsafe configuration error: %v", err)
	}
}

func TestPoolDoesNotRequireDatabaseAtStartup(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	pool, err := New(context.Background(), "postgres://test:test@"+address+"/test?sslmode=disable", 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if pool.Ping(ctx) == nil {
		t.Fatal("unavailable database unexpectedly ready")
	}
}
