package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func newAuthorizedProjectRequest(f *projectHTTP, method, path, body string, userID int64) (*http.Request, error) {
	req, err := http.NewRequest(method, f.server.URL+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.tokens[userID])
	return req, nil
}

func TestProjectEndpointEdgesIntegration(t *testing.T) {
	f := newProjectHTTP(t)
	owner, target := f.users[0].ID, f.users[4].ID
	p := f.create(t, owner, "Edges")
	path := fmt.Sprintf("/api/v1/projects/%d", p.ID)
	members := path + "/members"
	outsider := f.users[3].ID
	for _, tt := range []struct{ method, path, body string }{
		{"GET", path, ""}, {"PATCH", path, `{"name":"Forbidden"}`}, {"DELETE", path, ""},
		{"GET", members, ""}, {"POST", members, fmt.Sprintf(`{"user_id":%d,"role":"member"}`, target)},
		{"PATCH", fmt.Sprintf("%s/%d", members, owner), `{"role":"member"}`}, {"DELETE", fmt.Sprintf("%s/%d", members, owner), ""},
	} {
		f.send(t, tt.method, tt.path, tt.body, outsider, 403)
	}
	f.tokens[-1] = "invalid"
	for _, tt := range []struct{ method, path, body string }{
		{"POST", "/api/v1/projects", `{"name":"Valid"}`}, {"GET", "/api/v1/projects", ""},
		{"GET", path, ""}, {"PATCH", path, `{"name":"Valid"}`}, {"DELETE", path, ""},
		{"GET", members, ""}, {"POST", members, fmt.Sprintf(`{"user_id":%d,"role":"member"}`, target)},
		{"PATCH", fmt.Sprintf("%s/%d", members, target), `{"role":"member"}`}, {"DELETE", fmt.Sprintf("%s/%d", members, target), ""},
	} {
		f.send(t, tt.method, tt.path, tt.body, 0, 401)
		f.send(t, tt.method, tt.path, tt.body, -1, 401)
	}
	for _, body := range []string{"null", "[]", "{", `{"name":12}`, `{"name":"bad\u0000name"}`, `{"name":"` + strings.Repeat("x", 101) + `"}`, `{"name":"Valid","description":"` + strings.Repeat("x", 4096) + `"}`} {
		f.send(t, "POST", "/api/v1/projects", body, owner, 400)
	}
	f.send(t, "POST", "/api/v1/projects", `{"name":"`+strings.Repeat("я", 100)+`"}`, owner, 201)
	for _, query := range []string{"page=%ZZ", "page=1;page_size=2", "page=2147483648", "page_size=", "page=-1", "page_size=2&page_size=3"} {
		f.send(t, "GET", "/api/v1/projects?"+query, "", owner, 400)
		f.send(t, "GET", members+"?"+query, "", owner, 400)
	}
	f.send(t, "PATCH", path, `{"description":""}`, owner, 200)
	f.send(t, "PATCH", path, `{"name":null}`, owner, 400)
	f.send(t, "POST", members, `{"user_id":1}`, owner, 400)
	f.send(t, "POST", members, `{"user_id":1.5,"role":"member"}`, owner, 400)
	f.send(t, "POST", members, `{"email":"target@example.com","role":"member"}`, owner, 400)
	f.send(t, "PATCH", fmt.Sprintf("%s/%d", members, owner), `{"role":"owner"}`, owner, 400)
	f.send(t, "GET", "/api/v1/projects/999999/members", "", owner, 404)
	f.send(t, "DELETE", members+"/0", "", owner, 400)
	// Deleting an unrelated actor makes its still-valid token unusable.
	if _, err := f.pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, target); err != nil {
		t.Fatal(err)
	}
	f.send(t, "POST", "/api/v1/projects", `{"name":"Deleted actor"}`, target, 401)
	f.send(t, "GET", path, "", target, 401)
	f.send(t, "GET", members, "", target, 401)
	f.pool.Close()
	for _, tt := range []struct{ method, path, body string }{
		{"GET", path, ""}, {"PATCH", path, `{"name":"Valid"}`}, {"DELETE", path, ""},
		{"GET", members, ""}, {"POST", members, `{"user_id":1,"role":"member"}`},
		{"PATCH", members + "/1", `{"role":"member"}`}, {"DELETE", members + "/1", ""},
	} {
		body := f.send(t, tt.method, tt.path, tt.body, owner, 500)
		if !strings.Contains(string(body), `"code":"internal_error"`) {
			t.Fatal("unexpected error code")
		}
	}
	for _, sensitive := range []string{"password_hash", "private-test-hash"} {
		if strings.Contains(f.logs.String(), sensitive) {
			t.Fatal("sensitive log")
		}
	}
}

func TestConcurrentMembershipAndPatchHTTPIntegration(t *testing.T) {
	f := newProjectHTTP(t)
	owner, target := f.users[0].ID, f.users[4].ID
	p := f.create(t, owner, "Concurrent")
	path := fmt.Sprintf("/api/v1/projects/%d", p.ID)
	// Concurrent registration of the same membership must produce one conflict.
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	errors := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := newAuthorizedProjectRequest(f, "POST", path+"/members", fmt.Sprintf(`{"user_id":%d,"role":"member"}`, target), owner)
			if err != nil {
				errors <- err
				return
			}
			resp, err := f.server.Client().Do(req)
			if err != nil {
				errors <- err
				return
			}
			defer resp.Body.Close()
			statuses <- resp.StatusCode
		}()
	}
	wg.Wait()
	close(errors)
	close(statuses)
	for err := range errors {
		t.Fatal(err)
	}
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[201] != 1 || counts[409] != 1 {
		t.Fatalf("concurrent add: %v", counts)
	}
	// Independent PATCH fields must survive concurrent writes.
	statuses = make(chan int, 2)
	errors = make(chan error, 2)
	for _, body := range []string{`{"name":"Concurrent name"}`, `{"description":"Concurrent description"}`} {
		wg.Add(1)
		go func(body string) {
			defer wg.Done()
			req, err := newAuthorizedProjectRequest(f, "PATCH", path, body, owner)
			if err != nil {
				errors <- err
				return
			}
			resp, err := f.server.Client().Do(req)
			if err != nil {
				errors <- err
				return
			}
			defer resp.Body.Close()
			statuses <- resp.StatusCode
		}(body)
	}
	wg.Wait()
	close(errors)
	close(statuses)
	for err := range errors {
		t.Fatal(err)
	}
	for status := range statuses {
		if status != 200 {
			t.Fatalf("concurrent patch status=%d", status)
		}
	}
	var name, description string
	if err := f.pool.QueryRow(context.Background(), `SELECT name,description FROM projects WHERE id=$1`, p.ID).Scan(&name, &description); err != nil || name != "Concurrent name" || description != "Concurrent description" {
		t.Fatalf("lost patch fields: %v", err)
	}
	var count int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM project_members WHERE project_id=$1 AND user_id=$2`, p.ID, target).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate membership persisted")
	}
}
