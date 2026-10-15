package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

func createHTTPComment(t *testing.T, f *projectHTTP, path string, actor int64, body string) domain.Comment {
	t.Helper()
	result := f.send(t, "POST", path, body, actor, 201)
	var response struct {
		Data domain.Comment `json:"data"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}

func TestCommentHTTPIntegration(t *testing.T) {
	f := newProjectHTTP(t)
	owner, admin, member, outsider := f.users[0].ID, f.users[1].ID, f.users[2].ID, f.users[3].ID
	p := f.create(t, owner, "Comments")
	base := fmt.Sprintf("/api/v1/projects/%d", p.ID)
	for _, entry := range []struct {
		id   int64
		role string
	}{{admin, "admin"}, {member, "member"}} {
		f.send(t, "POST", base+"/members", fmt.Sprintf(`{"user_id":%d,"role":%q}`, entry.id, entry.role), owner, 201)
	}
	tasks := base + "/tasks"
	task := createHTTPTask(t, f, tasks, owner, `{"title":"Discuss"}`)
	otherTask := createHTTPTask(t, f, tasks, owner, `{"title":"Other"}`)
	path := fmt.Sprintf("%s/%d/comments", tasks, task.ID)
	f.send(t, "POST", path, `{"body":"Anonymous"}`, 0, 401)
	f.send(t, "POST", path, `{"body":"Outside"}`, outsider, 403)
	for _, body := range []string{`{}`, `{"body":" "}`, `{"body":"x","author_id":99}`, `{"body":null}`, `{"body":"\u0000"}`, `{"body":"x"} {}`} {
		f.send(t, "POST", path, body, member, 400)
	}
	c := createHTTPComment(t, f, path, member, `{"body":" First comment "}`)
	if c.AuthorID != member || c.TaskID != task.ID || c.Body != "First comment" || c.CreatedAt.IsZero() {
		t.Fatalf("incorrect comment: %+v", c)
	}
	item := fmt.Sprintf("%s/%d", path, c.ID)
	f.send(t, "PATCH", item, `{"body":"No"}`, owner, 200)
	f.send(t, "PATCH", item, `{"body":"Author edit"}`, member, 200)
	other := createHTTPComment(t, f, path, owner, `{"body":"Owner comment"}`)
	otherItem := fmt.Sprintf("%s/%d", path, other.ID)
	f.send(t, "PATCH", otherItem, `{"body":"No"}`, member, 403)
	f.send(t, "DELETE", otherItem, "", member, 403)
	f.send(t, "PATCH", otherItem, `{"body":"Admin edit"}`, admin, 200)
	f.send(t, "PATCH", item, `{}`, member, 400)
	f.send(t, "PATCH", path+"/nope", `{"body":"Bad ID"}`, member, 400)
	list := f.send(t, "GET", path+"?page_size=1", "", member, 200)
	if !strings.Contains(string(list), `"total":2`) {
		t.Fatal("incorrect comment total")
	}
	wrong := fmt.Sprintf("%s/%d/comments/%d", tasks, otherTask.ID, c.ID)
	f.send(t, "PATCH", wrong, `{"body":"Cross-task"}`, owner, 404)
	f.send(t, "DELETE", wrong, "", owner, 404)
	another := f.create(t, owner, "Another")
	wrongProject := fmt.Sprintf("/api/v1/projects/%d/tasks/%d/comments", another.ID, task.ID)
	f.send(t, "GET", wrongProject, "", owner, 404)
	f.send(t, "POST", wrongProject, `{"body":"Cross-project"}`, owner, 404)
	f.send(t, "DELETE", item, "", member, 204)
	f.send(t, "DELETE", item, "", member, 404)
	f.send(t, "DELETE", fmt.Sprintf("%s/%d", tasks, task.ID), "", owner, 204)
	var count int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM comments WHERE task_id=$1`, task.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("comments not cascaded")
	}
	f.send(t, "GET", path, "", owner, 404)
}
