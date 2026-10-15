package handler_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTaskAndCommentRouteFailuresHTTPIntegration(t *testing.T) {
	f := newProjectHTTP(t)
	owner, outsider := f.users[0].ID, f.users[3].ID
	p := f.create(t, owner, "Route failures")
	base := fmt.Sprintf("/api/v1/projects/%d/tasks", p.ID)
	task := createHTTPTask(t, f, base, owner, `{"title":"Task"}`)
	item := fmt.Sprintf("%s/%d", base, task.ID)
	comments := item + "/comments"
	comment := createHTTPComment(t, f, comments, owner, `{"body":"Comment"}`)
	commentPath := fmt.Sprintf("%s/%d", comments, comment.ID)
	cases := []struct{ method, path, body string }{
		{"POST", base, `{"title":"New"}`}, {"GET", base, ""}, {"GET", item, ""},
		{"PATCH", item, `{"title":"Edit"}`}, {"DELETE", item, ""},
		{"POST", comments, `{"body":"New"}`}, {"GET", comments, ""},
		{"PATCH", commentPath, `{"body":"Edit"}`}, {"DELETE", commentPath, ""},
	}
	for _, tc := range cases {
		f.send(t, tc.method, tc.path, tc.body, 0, 401)
		f.send(t, tc.method, tc.path, tc.body, outsider, 403)
	}
	original := f.tokens[owner]
	f.tokens[owner] = "invalid"
	for _, tc := range cases {
		f.send(t, tc.method, tc.path, tc.body, owner, 401)
	}
	encoded, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject: fmt.Sprint(owner), IssuedAt: jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)), ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
	}).SignedString([]byte("unit-test-secret-at-least-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	f.tokens[owner] = encoded
	for _, tc := range cases {
		f.send(t, tc.method, tc.path, tc.body, owner, 401)
	}
	f.tokens[owner] = original
	f.send(t, "POST", base, `{"title":"`+strings.Repeat("a", 5000)+`"}`, owner, 400)
	f.send(t, "POST", comments, `{"body":"`+strings.Repeat("a", 5000)+`"}`, owner, 400)
	f.send(t, "PUT", item, `{}`, owner, 405)
	f.send(t, "PUT", commentPath, `{}`, owner, 405)
	f.pool.Close()
	for _, tc := range cases {
		body := f.send(t, tc.method, tc.path, tc.body, owner, 500)
		for _, private := range []string{"postgres", "closed", "SQLSTATE", "unit-test-secret"} {
			if strings.Contains(string(body), private) {
				t.Fatalf("internal error exposed: %s", body)
			}
		}
	}
	if strings.Contains(f.logs.String(), original) || strings.Contains(f.logs.String(), encoded) {
		t.Fatal("JWT leaked to logs")
	}
}
