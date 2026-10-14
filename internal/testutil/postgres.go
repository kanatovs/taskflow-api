// Package testutil provides helpers for tests against disposable local databases.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kanatovs/taskflow-api/internal/platform/database"
)

// Postgres creates a uniquely named database, applies the schema, and drops only
// that database at cleanup. TEST_POSTGRES_URL must target disposable Compose.
func Postgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("set TEST_POSTGRES_URL to disposable local Compose PostgreSQL")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatal("test PostgreSQL must be local")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("connect to disposable test PostgreSQL failed")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	name := "taskflow_test_" + hex.EncodeToString(random[:])
	// PostgreSQL identifiers cannot be query parameters. Sanitize the generated name.
	identifier := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+identifier); err != nil {
		admin.Close(ctx)
		t.Fatal("create disposable database failed")
	}
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		defer admin.Close(cleanupCtx)
		if _, err := admin.Exec(cleanupCtx, "DROP DATABASE "+identifier); err != nil {
			t.Error("drop disposable database failed")
		}
	})
	u.Path = "/" + name
	pool, err = database.New(ctx, u.String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ApplyMigration(t, pool, "up")
	return pool
}

func ApplyMigration(t *testing.T, pool *pgxpool.Pool, direction string) {
	t.Helper()
	if direction != "up" && direction != "down" {
		t.Fatal("invalid migration direction")
	}
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "migrations", "000001_create_users."+direction+".sql")
	sql, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("apply migration %s: %v", direction, err)
	}
}
