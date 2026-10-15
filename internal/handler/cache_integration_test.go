package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/config"
	"github.com/kanatovs/taskflow-api/internal/domain"
	platformcache "github.com/kanatovs/taskflow-api/internal/platform/cache"
	repositorycache "github.com/kanatovs/taskflow-api/internal/repository/cache"
	"github.com/redis/go-redis/v9"
)

func TestCacheHTTPIntegration(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set TEST_REDIS_ADDR and TEST_POSTGRES_URL to isolated local services")
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || (host != "127.0.0.1" && host != "localhost") {
		t.Fatal("test Redis must be local")
	}
	store := platformcache.New(config.Redis{Addr: addr, DialTimeout: time.Second, OperationTimeout: time.Second})
	t.Cleanup(func() { store.Close() })
	inspect := redis.NewClient(&redis.Options{Addr: addr, ReadTimeout: time.Second, WriteTimeout: time.Second, ContextTimeoutEnabled: true, MaxRetries: -1})
	t.Cleanup(func() { inspect.Close() })
	f := newProjectHTTP(t, store)
	owner, member, outsider := f.users[0].ID, f.users[2].ID, f.users[3].ID
	var project struct {
		Data domain.Project `json:"data"`
	}
	if err := json.Unmarshal(f.send(t, "POST", "/api/v1/projects", `{"name":"Cached project"}`, owner, 201), &project); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/projects/%d", project.Data.ID)
	f.send(t, "POST", path+"/members", fmt.Sprintf(`{"user_id":%d,"role":"member"}`, member), owner, 201)
	var task struct {
		Data domain.Task `json:"data"`
	}
	if err := json.Unmarshal(f.send(t, "POST", path+"/tasks", `{"title":"Cached task"}`, owner, 201), &task); err != nil {
		t.Fatal(err)
	}
	taskPath := fmt.Sprintf("%s/tasks/%d", path, task.Data.ID)
	projectKey, taskKey := repositorycache.ProjectKey(project.Data.ID), repositorycache.TaskKey(project.Data.ID, task.Data.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// Only these fixture-owned keys are touched; no FLUSHDB or shared-data cleanup.
	if err := inspect.Del(ctx, projectKey, taskKey).Err(); err != nil {
		t.Fatal("initial key cleanup failed")
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := inspect.Del(cleanupCtx, projectKey, taskKey).Err(); err != nil {
			t.Error("cache fixture key cleanup failed")
		}
	})
	assertAbsent := func(key string) {
		t.Helper()
		if n, err := inspect.Exists(ctx, key).Result(); err != nil || n != 0 {
			t.Fatal("invalidated or negative entry still cached")
		}
	}
	for range 2 {
		f.send(t, "GET", path, "", member, 200)
		f.send(t, "GET", taskPath, "", member, 200)
	}
	for _, key := range []string{projectKey, taskKey} {
		value, err := inspect.Get(ctx, key).Result()
		if err != nil {
			t.Fatal("detail cache not populated")
		}
		for _, private := range []string{"password", "private-test-hash", "Authorization", "access_token", `"data"`, `"role"`} {
			if strings.Contains(value, private) {
				t.Fatal("private or non-domain value cached")
			}
		}
		if ttl, err := inspect.PTTL(ctx, key).Result(); err != nil || ttl <= 0 || ttl > time.Minute {
			t.Fatal("invalid cache TTL")
		}
	}
	for _, p := range []string{path, taskPath} {
		f.send(t, "GET", p, "", outsider, 403)
		f.send(t, "GET", p, "", 0, 401)
	}
	if err := inspect.Set(ctx, projectKey, "{malformed-private-payload", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	f.send(t, "GET", path, "", owner, 200)
	wrong := task.Data
	wrong.ProjectID++
	encoded, _ := json.Marshal(wrong)
	if err := inspect.Set(ctx, taskKey, encoded, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(f.send(t, "GET", taskPath, "", owner, 200), &task); err != nil || task.Data.ProjectID != project.Data.ID {
		t.Fatal("cross-project cached task escaped")
	}
	f.send(t, "PATCH", path, `{"name":"Updated project"}`, owner, 200)
	assertAbsent(projectKey)
	if body := f.send(t, "GET", path, "", owner, 200); !strings.Contains(string(body), "Updated project") {
		t.Fatal("stale project")
	}
	for _, body := range []string{`{"title":"Updated task"}`, `{"status":"in_progress"}`, `{"priority":"high"}`, `{"due_date":"2026-10-20T10:00:00Z"}`, fmt.Sprintf(`{"assignee_id":%d}`, member), `{"assignee_id":null,"due_date":null}`} {
		f.send(t, "PATCH", taskPath, body, owner, 200)
		assertAbsent(taskKey)
		f.send(t, "GET", taskPath, "", owner, 200)
	}
	f.send(t, "POST", taskPath+"/comments", `{"body":"Not derived in task details"}`, member, 201)
	if exists, err := inspect.Exists(ctx, taskKey).Result(); err != nil || exists != 1 {
		t.Fatal("comment mutation invalidated task detail")
	}
	f.send(t, "GET", path+"/tasks?search=Updated", "", owner, 200)
	f.send(t, "DELETE", fmt.Sprintf("%s/members/%d", path, member), "", owner, 204)
	for _, p := range []string{path, taskPath} {
		f.send(t, "GET", p, "", member, 403)
	}
	f.send(t, "DELETE", taskPath, "", owner, 204)
	assertAbsent(taskKey)
	f.send(t, "GET", taskPath, "", owner, 404)
	assertAbsent(taskKey)
	f.send(t, "DELETE", path, "", owner, 204)
	assertAbsent(projectKey)
	f.send(t, "GET", path, "", owner, 404)
	assertAbsent(projectKey)
	if strings.Contains(f.logs.String(), "malformed-private-payload") {
		t.Fatal("cached payload leaked into logs")
	}
}
