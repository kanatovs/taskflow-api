package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

func createHTTPTask(t *testing.T, f *projectHTTP, path string, actor int64, body string) domain.Task {
	t.Helper()
	result := f.send(t, "POST", path, body, actor, 201)
	var response struct {
		Data domain.Task `json:"data"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}

func TestTaskAssignmentAndPermissionsHTTPIntegration(t *testing.T) {
	f := newProjectHTTP(t)
	owner, admin, member, outsider, otherMember := f.users[0].ID, f.users[1].ID, f.users[2].ID, f.users[3].ID, f.users[4].ID
	p := f.create(t, owner, "Permissions")
	base := fmt.Sprintf("/api/v1/projects/%d", p.ID)
	for _, entry := range []struct {
		id   int64
		role string
	}{{admin, "admin"}, {member, "member"}, {otherMember, "member"}} {
		f.send(t, "POST", base+"/members", fmt.Sprintf(`{"user_id":%d,"role":%q}`, entry.id, entry.role), owner, 201)
	}
	path := base + "/tasks"
	f.send(t, "POST", path, fmt.Sprintf(`{"title":"Invalid assignment","assignee_id":%d}`, outsider), owner, 400)
	f.send(t, "POST", path, `{"title":"Invalid assignment","assignee_id":999999}`, owner, 400)
	f.send(t, "POST", path, `{"title":"Already done","status":"done"}`, owner, 400)
	task := createHTTPTask(t, f, path, owner, fmt.Sprintf(`{"title":"Assigned","assignee_id":%d}`, member))
	item := fmt.Sprintf("%s/%d", path, task.ID)
	f.send(t, "GET", item, "", otherMember, 200)
	f.send(t, "PATCH", item, `{"title":"No"}`, member, 403)
	f.send(t, "PATCH", item, `{"status":"in_progress"}`, otherMember, 403)
	f.send(t, "PATCH", item, `{"status":"done"}`, member, 409)
	f.send(t, "PATCH", item, `{"status":"in_progress"}`, member, 200)
	f.send(t, "PATCH", item, `{"status":"done"}`, member, 200)
	f.send(t, "PATCH", item, `{"status":"todo"}`, admin, 409)
	f.send(t, "PATCH", item, `{"status":"in_progress"}`, admin, 200)
	f.send(t, "PATCH", item, fmt.Sprintf(`{"assignee_id":%d}`, outsider), owner, 400)
	f.send(t, "PATCH", item, `{"assignee_id":null}`, admin, 200)
	f.send(t, "PATCH", item, `{"status":"todo"}`, member, 403)
	f.send(t, "DELETE", item, "", member, 403)
	created := createHTTPTask(t, f, path, member, `{"title":"Member's own task"}`)
	owned := fmt.Sprintf("%s/%d", path, created.ID)
	f.send(t, "PATCH", owned, `{"title":"Creator edit"}`, member, 200)
	f.send(t, "DELETE", owned, "", otherMember, 403)
	f.send(t, "DELETE", owned, "", member, 204)
	f.send(t, "PATCH", item, fmt.Sprintf(`{"assignee_id":%d}`, member), owner, 200)
	f.send(t, "DELETE", fmt.Sprintf("%s/members/%d", base, member), "", owner, 204)
	f.send(t, "PATCH", item, `{"status":"todo"}`, member, 403)
	// Historical attribution survives membership removal, but no longer grants access.
	var assignee int64
	if err := f.pool.QueryRow(context.Background(), `SELECT assignee_id FROM tasks WHERE id=$1`, task.ID).Scan(&assignee); err != nil || assignee != member {
		t.Fatal("historical assignment lost")
	}
	f.send(t, "PATCH", item, `{"assignee_id":null}`, owner, 200)
	f.send(t, "DELETE", item, "", admin, 204)
}

func TestTaskCRUDHTTPIntegration(t *testing.T) {
	f := newProjectHTTP(t)
	owner, outsider := f.users[0].ID, f.users[3].ID
	p := f.create(t, owner, "Tasks")
	other := f.create(t, owner, "Other")
	path := fmt.Sprintf("/api/v1/projects/%d/tasks", p.ID)
	f.send(t, "POST", path, `{"title":"No auth"}`, 0, 401)
	f.send(t, "POST", path, `{"title":"Forbidden"}`, outsider, 403)
	for _, body := range []string{`{}`, `{"title":" "}`, `{"title":"x","priority":"urgent"}`, `{"title":"x","status":"closed"}`, `{"title":"x","creator_id":123}`, `{"title":"x","due_date":"tomorrow"}`, `{"title":"x"} {}`, `{"title":`} {
		f.send(t, "POST", path, body, owner, 400)
	}
	task := createHTTPTask(t, f, path, owner, `{"title":" First task ","description":"Details","due_date":"2026-10-20T15:00:00+05:00"}`)
	if task.Title != "First task" || task.CreatorID != owner || task.ProjectID != p.ID || task.Status != domain.TaskTodo || task.Priority != domain.PriorityMedium || task.DueDate == nil || task.DueDate.Hour() != 10 {
		t.Fatalf("incorrect task: %+v", task)
	}
	item := fmt.Sprintf("%s/%d", path, task.ID)
	f.send(t, "GET", item, "", owner, 200)
	f.send(t, "GET", item, "", outsider, 403)
	wrong := fmt.Sprintf("/api/v1/projects/%d/tasks/%d", other.ID, task.ID)
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		f.send(t, method, wrong, `{"title":"Wrong"}`, owner, 404)
	}
	f.send(t, "GET", path+"/nope", "", owner, 400)
	f.send(t, "PATCH", item, `{}`, owner, 400)
	changed := f.send(t, "PATCH", item, `{"title":"Updated","due_date":null}`, owner, 200)
	var update struct {
		Data domain.Task `json:"data"`
	}
	if err := json.Unmarshal(changed, &update); err != nil || update.Data.DueDate != nil || update.Data.Description != "Details" || update.Data.Title != "Updated" {
		t.Fatal("PATCH did not preserve omitted fields")
	}
	list := f.send(t, "GET", path+"?page_size=1", "", owner, 200)
	if !strings.Contains(string(list), `"total":1`) {
		t.Fatal("incorrect total")
	}
	empty := f.send(t, "GET", path+"?page=2", "", owner, 200)
	if !strings.Contains(string(empty), `"data":[]`) || !strings.Contains(string(empty), `"total":1`) {
		t.Fatal("incorrect empty page")
	}
	f.send(t, "DELETE", item, "", owner, 204)
	f.send(t, "GET", item, "", owner, 404)
	f.pool.Close()
	failure := f.send(t, "GET", path, "", owner, 500)
	if strings.Contains(string(failure), "closed") {
		t.Fatal("repository details exposed")
	}
}
