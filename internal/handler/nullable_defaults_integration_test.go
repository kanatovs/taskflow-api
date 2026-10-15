package handler_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

// Keep the documented nullable create defaults aligned with real HTTP behavior.
func TestNullableCreateDefaultsHTTPIntegration(t *testing.T) {
	f := newProjectHTTP(t)
	owner := f.users[0].ID
	var project struct {
		Data domain.Project `json:"data"`
	}
	body := f.send(t, "POST", "/api/v1/projects", `{"name":"Nullable defaults","description":null}`, owner, 201)
	if err := json.Unmarshal(body, &project); err != nil || project.Data.Description != "" {
		t.Fatal("null project description did not default to empty")
	}
	path := fmt.Sprintf("/api/v1/projects/%d/tasks", project.Data.ID)
	task := createHTTPTask(t, f, path, owner, `{"title":"Defaults","description":null,"status":null,"priority":null,"assignee_id":null,"due_date":null}`)
	if task.Description != "" || task.Status != domain.TaskTodo || task.Priority != domain.PriorityMedium || task.AssigneeID != nil || task.DueDate != nil {
		t.Fatal("nullable create defaults differ from the OpenAPI contract")
	}
}
