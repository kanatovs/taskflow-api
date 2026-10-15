package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/handler"
	"github.com/kanatovs/taskflow-api/internal/platform/token"
	"github.com/kanatovs/taskflow-api/internal/repository/postgres"
	"github.com/kanatovs/taskflow-api/internal/service"
	"github.com/kanatovs/taskflow-api/internal/testutil"
)

type lockedLogBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.Write(p)
}
func (b *lockedLogBuffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.String()
}

func TestAuthenticationFlowOverHTTPIntegration(t *testing.T) {
	pool := testutil.Postgres(t)
	repo := postgres.NewUserRepository(pool, time.Second)
	issuer, err := token.NewJWT("unit-test-secret-at-least-32-bytes", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var logs lockedLogBuffer
	router := handler.NewRouter(slog.New(slog.NewJSONHandler(&logs, nil)), handler.NewReadiness(pool, time.Second), handler.NewAuthHandler(service.NewAuthService(repo, issuer), issuer), nil, nil)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 3 * time.Second
	send := func(method, path, body, authorization string, want int) []byte {
		t.Helper()
		request, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		if authorization != "" {
			request.Header.Set("Authorization", authorization)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		result, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s status=%d want=%d", method, path, response.StatusCode, want)
		}
		if response.Header.Get("Content-Type") != "application/json" {
			t.Fatal("missing JSON content type")
		}
		return result
	}
	send("GET", "/ready", "", "", 200)
	registration := `{"email":" USER@Example.com ","password":"strong-password-123","display_name":" User "}`
	created := send("POST", "/api/v1/auth/register", registration, "", 201)
	var registered struct {
		Data domain.User `json:"data"`
	}
	if err := json.Unmarshal(created, &registered); err != nil {
		t.Fatal(err)
	}
	if registered.Data.ID <= 0 || registered.Data.Email != "user@example.com" || registered.Data.DisplayName != "User" {
		t.Fatal("incorrect registered user")
	}
	send("POST", "/api/v1/auth/register", registration, "", 409)
	login := send("POST", "/api/v1/auth/login", `{"email":"user@example.com","password":"strong-password-123"}`, "", 200)
	var access struct {
		Data domain.AccessToken `json:"data"`
	}
	if err := json.Unmarshal(login, &access); err != nil {
		t.Fatal(err)
	}
	if access.Data.TokenType != "Bearer" || access.Data.ExpiresIn != 60 {
		t.Fatal("incorrect token metadata")
	}
	me := send("GET", "/api/v1/users/me", "", "Bearer "+access.Data.AccessToken, 200)
	var current struct {
		Data domain.User `json:"data"`
	}
	if err := json.Unmarshal(me, &current); err != nil {
		t.Fatal(err)
	}
	if current.Data.ID != registered.Data.ID || current.Data.Email != registered.Data.Email {
		t.Fatal("token resolved to wrong user")
	}
	wrongPassword := send("POST", "/api/v1/auth/login", `{"email":"user@example.com","password":"wrong-password"}`, "", 401)
	unknownEmail := send("POST", "/api/v1/auth/login", `{"email":"unknown@example.com","password":"strong-password-123"}`, "", 401)
	if !bytes.Equal(wrongPassword, unknownEmail) {
		t.Fatal("account existence revealed")
	}
	send("GET", "/api/v1/users/me", "", "", 401)
	send("GET", "/api/v1/users/me", "", "Bearer malformed", 401)
	send("GET", "/api/v1/auth/login", "", "", 405)
	send("POST", "/api/v1/auth/login", `{"email":"user@example.com","password":"strong-password-123","unknown":true}`, "", 400)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// Only the uniquely created disposable database is mutated.
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id=$1", registered.Data.ID); err != nil {
		t.Fatal(err)
	}
	send("GET", "/api/v1/users/me", "", "Bearer "+access.Data.AccessToken, 401)
	pool.Close()
	send("GET", "/health", "", "", 200)
	send("GET", "/ready", "", "", 503)
	send("POST", "/api/v1/auth/register", registration, "", 500)
	send("POST", "/api/v1/auth/login", `{"email":"user@example.com","password":"strong-password-123"}`, "", 500)
	send("GET", "/api/v1/users/me", "", "Bearer "+access.Data.AccessToken, 500)
	for _, body := range [][]byte{created, me} {
		if bytes.Contains(body, []byte("\"password")) {
			t.Fatal("password field exposed")
		}
	}
	for _, sensitive := range []string{"strong-password-123", access.Data.AccessToken, "$2a$", "password_hash"} {
		if strings.Contains(logs.String(), sensitive) {
			t.Fatal("authentication secrets logged")
		}
	}
}
