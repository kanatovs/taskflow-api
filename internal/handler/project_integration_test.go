package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/handler"
	"github.com/kanatovs/taskflow-api/internal/platform/token"
	"github.com/kanatovs/taskflow-api/internal/repository/postgres"
	"github.com/kanatovs/taskflow-api/internal/service"
	"github.com/kanatovs/taskflow-api/internal/testutil"
)

type projectHTTP struct {
	pool   *pgxpool.Pool
	server *httptest.Server
	users  []domain.User
	tokens map[int64]string
	logs   *lockedLogBuffer
}

func newProjectHTTP(t *testing.T) *projectHTTP {
	t.Helper()
	pool := testutil.Postgres(t)
	users := postgres.NewUserRepository(pool, time.Second)
	issuer, err := token.NewJWT("unit-test-secret-at-least-32-bytes", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	f := &projectHTTP{pool: pool, tokens: make(map[int64]string), logs: &lockedLogBuffer{}}
	for _, name := range []string{"owner", "admin", "member", "outsider", "target"} {
		u, err := users.Create(context.Background(), domain.User{Email: name + "@example.com", DisplayName: name, PasswordHash: "private-test-hash"})
		if err != nil {
			t.Fatal(err)
		}
		access, err := issuer.Issue(u.ID)
		if err != nil {
			t.Fatal(err)
		}
		f.users = append(f.users, u)
		f.tokens[u.ID] = access.AccessToken
	}
	projects := postgres.NewProjectRepository(pool, time.Second)
	members := postgres.NewProjectMemberRepository(pool, time.Second)
	projectService := service.NewProjectService(projects, members, users)
	router := handler.NewRouter(slog.New(slog.NewJSONHandler(f.logs, nil)), handler.NewReadiness(pool, time.Second), handler.NewAuthHandler(service.NewAuthService(users, issuer), issuer), handler.NewProjectHandler(projectService, service.NewProjectMemberService(projectService, members)))
	f.server = httptest.NewServer(router)
	f.server.Client().Timeout = 5 * time.Second
	t.Cleanup(f.server.Close)
	return f
}

func (f *projectHTTP) send(t *testing.T, method, path, body string, userID int64, want int) []byte {
	t.Helper()
	req, err := http.NewRequest(method, f.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if userID != 0 {
		req.Header.Set("Authorization", "Bearer "+f.tokens[userID])
	}
	resp, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	result, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != want {
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, resp.StatusCode, want, result)
	}
	if want == 204 {
		if len(result) != 0 {
			t.Fatal("204 has a body")
		}
	} else if resp.Header.Get("Content-Type") != "application/json" || !json.Valid(result) {
		t.Fatal("response is not valid JSON")
	}
	for _, private := range []string{"password_hash", "private-test-hash", "$2a$"} {
		if strings.Contains(string(result), private) {
			t.Fatal("private data in response")
		}
	}
	return result
}

func (f *projectHTTP) create(t *testing.T, userID int64, name string) domain.Project {
	t.Helper()
	body, err := json.Marshal(service.CreateProjectInput{Name: name, Description: "Initial description"})
	if err != nil {
		t.Fatal(err)
	}
	result := f.send(t, "POST", "/api/v1/projects", string(body), userID, 201)
	var response struct {
		Data domain.Project `json:"data"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}

func TestProjectCRUDHTTPIntegration(t *testing.T) {
	f := newProjectHTTP(t)
	owner, admin, member, outsider := f.users[0].ID, f.users[1].ID, f.users[2].ID, f.users[3].ID
	f.send(t, "POST", "/api/v1/projects", `{"name":"Anonymous"}`, 0, 401)
	f.send(t, "POST", "/api/v1/projects", `{"name":" "}`, owner, 400)
	f.send(t, "POST", "/api/v1/projects", `{"name":"Valid","owner_id":99}`, owner, 400)
	f.send(t, "POST", "/api/v1/projects", `{"name":"Valid"} {}`, owner, 400)
	p := f.create(t, owner, " Portfolio ")
	if p.Name != "Portfolio" || p.OwnerID != owner {
		t.Fatal("incorrect created project")
	}
	var role string
	if err := f.pool.QueryRow(context.Background(), `SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2`, p.ID, owner).Scan(&role); err != nil || role != "owner" {
		t.Fatal("owner membership missing")
	}
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'admin'),($1,$3,'member')`, p.ID, admin, member); err != nil {
		t.Fatal(err)
	}
	f.create(t, outsider, "Other project")
	path := fmt.Sprintf("/api/v1/projects/%d", p.ID)
	for _, user := range []int64{owner, admin, member} {
		f.send(t, "GET", path, "", user, 200)
	}
	f.send(t, "GET", path, "", outsider, 403)
	f.send(t, "GET", path, "", 0, 401)
	f.send(t, "GET", "/api/v1/projects/999999", "", owner, 404)
	f.send(t, "GET", "/api/v1/projects/nope", "", owner, 400)
	list := f.send(t, "GET", "/api/v1/projects?page=1&page_size=1", "", member, 200)
	var listed struct {
		Data []domain.Project `json:"data"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(list, &listed); err != nil || len(listed.Data) != 1 || listed.Data[0].ID != p.ID || listed.Meta.Total != 1 {
		t.Fatal("list includes another user's projects")
	}
	for _, query := range []string{"page=0", "page_size=101", "page=1&page=2", "search=portfolio"} {
		f.send(t, "GET", "/api/v1/projects?"+query, "", owner, 400)
	}
	f.send(t, "PATCH", path, `{"name":"Member edit"}`, member, 403)
	f.send(t, "PATCH", path, `{"description":"Admin edit"}`, admin, 200)
	updated := f.send(t, "PATCH", path, `{"name":"Owner edit"}`, owner, 200)
	var changed struct {
		Data domain.Project `json:"data"`
	}
	if err := json.Unmarshal(updated, &changed); err != nil || changed.Data.Description != "Admin edit" || changed.Data.Name != "Owner edit" {
		t.Fatal("patch replaced omitted fields")
	}
	f.send(t, "PATCH", path, `{}`, owner, 400)
	f.send(t, "PATCH", path, `{"owner_id":2}`, owner, 400)
	f.send(t, "DELETE", path, "", admin, 403)
	f.send(t, "DELETE", path, "", member, 403)
	f.send(t, "DELETE", path, "", owner, 204)
	f.send(t, "GET", path, "", owner, 404)
	empty := f.send(t, "GET", "/api/v1/projects", "", member, 200)
	if !strings.Contains(string(empty), `"data":[]`) {
		t.Fatal("empty collection is not an array")
	}
	f.pool.Close()
	f.send(t, "GET", "/api/v1/projects", "", owner, 500)
	for _, secret := range f.tokens {
		if strings.Contains(f.logs.String(), secret) {
			t.Fatal("JWT logged")
		}
	}
}
