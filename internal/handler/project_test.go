package handler

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/platform/token"
	"github.com/kanatovs/taskflow-api/internal/service"
)

type projectHandlerFake struct {
	err            error
	ctx            context.Context
	actor, project int64
}

func (f *projectHandlerFake) record(ctx context.Context, actor, project int64) {
	f.ctx, f.actor, f.project = ctx, actor, project
}
func (f *projectHandlerFake) Create(ctx context.Context, actor int64, _ service.CreateProjectInput) (domain.Project, error) {
	f.record(ctx, actor, 0)
	return domain.Project{ID: 9}, f.err
}
func (f *projectHandlerFake) List(ctx context.Context, actor int64, _, _ int) ([]domain.Project, int, error) {
	f.record(ctx, actor, 0)
	return []domain.Project{}, 0, f.err
}
func (f *projectHandlerFake) Get(ctx context.Context, actor, project int64) (domain.Project, error) {
	f.record(ctx, actor, project)
	return domain.Project{}, f.err
}
func (f *projectHandlerFake) Update(ctx context.Context, actor, project int64, _ domain.ProjectChanges) (domain.Project, error) {
	f.record(ctx, actor, project)
	return domain.Project{}, f.err
}
func (f *projectHandlerFake) Delete(ctx context.Context, actor, project int64) error {
	f.record(ctx, actor, project)
	return f.err
}

type projectMemberHandlerFake struct {
	projectHandlerFake
	target int64
}

func (f *projectMemberHandlerFake) List(ctx context.Context, actor, project int64, _, _ int) ([]domain.ProjectMember, int, error) {
	f.record(ctx, actor, project)
	return []domain.ProjectMember{}, 0, f.err
}
func (f *projectMemberHandlerFake) Add(ctx context.Context, actor, project int64, input service.AddProjectMemberInput) (domain.ProjectMember, error) {
	f.record(ctx, actor, project)
	f.target = input.UserID
	return domain.ProjectMember{UserID: input.UserID}, f.err
}
func (f *projectMemberHandlerFake) Update(ctx context.Context, actor, project, target int64, _ service.UpdateProjectMemberInput) (domain.ProjectMember, error) {
	f.record(ctx, actor, project)
	f.target = target
	return domain.ProjectMember{}, f.err
}
func (f *projectMemberHandlerFake) Delete(ctx context.Context, actor, project, target int64) error {
	f.record(ctx, actor, project)
	f.target = target
	return f.err
}

func TestProjectHandlerErrorMappingAndContext(t *testing.T) {
	issuer, err := token.NewJWT("unit-test-secret-at-least-32-bytes", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	access, err := issuer.Issue(7)
	if err != nil {
		t.Fatal(err)
	}
	type contextKey struct{}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), contextKey{}, "request-value"), time.Minute)
	defer cancel()
	for _, failure := range []struct {
		err    error
		status int
		code   string
	}{
		{domain.ErrNotFound, 404, "not_found"}, {domain.ErrForbidden, 403, "forbidden"}, {domain.ErrConflict, 409, "conflict"},
		{&domain.ValidationError{Fields: map[string]string{"name": "invalid"}}, 400, "validation_error"},
		{errors.New("private repository detail"), 500, "internal_error"}, {context.Canceled, 500, "internal_error"},
	} {
		for _, route := range []struct{ method, path, body string }{
			{"POST", "/api/v1/projects", `{"name":"Valid"}`}, {"GET", "/api/v1/projects", ""},
			{"GET", "/api/v1/projects/9", ""}, {"PATCH", "/api/v1/projects/9", `{"name":"Valid"}`}, {"DELETE", "/api/v1/projects/9", ""},
			{"GET", "/api/v1/projects/9/members", ""}, {"POST", "/api/v1/projects/9/members", `{"user_id":13,"role":"member"}`},
			{"PATCH", "/api/v1/projects/9/members/13", `{"role":"member"}`}, {"DELETE", "/api/v1/projects/9/members/13", ""},
		} {
			var logs bytes.Buffer
			p := &projectHandlerFake{err: failure.err}
			m := &projectMemberHandlerFake{projectHandlerFake: projectHandlerFake{err: failure.err}}
			router := NewRouter(5*time.Second, slog.New(slog.NewJSONHandler(&logs, nil)), nil, NewAuthHandler(nil, issuer), NewProjectHandler(p, m), nil)
			req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body)).WithContext(ctx)
			req.Header.Set("Authorization", "Bearer "+access.AccessToken)
			w := httptest.NewRecorder()
			started := time.Now()
			router.ServeHTTP(w, req)
			if w.Code != failure.status || !strings.Contains(w.Body.String(), `"code":"`+failure.code+`"`) {
				t.Fatalf("%s %s: %d %s", route.method, route.path, w.Code, w.Body.String())
			}
			called := p
			if strings.Contains(route.path, "/members") {
				called = &m.projectHandlerFake
			}
			if called.ctx == nil || called.ctx.Value(contextKey{}) != "request-value" || called.actor != 7 {
				t.Fatal("handler lost context or actor")
			}
			parentDeadline, _ := ctx.Deadline()
			deadline, ok := called.ctx.Deadline()
			if !ok || deadline.After(parentDeadline) || deadline.Before(started.Add(5*time.Second)) || deadline.After(time.Now().Add(5*time.Second)) {
				t.Fatalf("handler deadline=%v, want the shorter HTTP budget within parent deadline=%v", deadline, parentDeadline)
			}
			if strings.Contains(route.path, "/9") && called.project != 9 {
				t.Fatal("handler lost project ID")
			}
			if failure.status == 500 && strings.Contains(route.path, "/9") && !strings.Contains(logs.String(), `"project_id":9`) {
				t.Fatal("project ID missing in service failure log")
			}
			if strings.Contains(route.path, "/13") && m.target != 13 {
				t.Fatal("handler lost member ID")
			}
			for _, private := range []string{"private repository detail", access.AccessToken} {
				if strings.Contains(w.Body.String(), private) || strings.Contains(logs.String(), private) {
					t.Fatal("unsafe error output")
				}
			}
		}
	}
}
