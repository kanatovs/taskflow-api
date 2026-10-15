package handler_test

import (
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
