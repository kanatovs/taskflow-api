package handler_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"testing"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

func taskList(t *testing.T, f *projectHTTP, path string, actor int64) ([]domain.Task, int) {
	t.Helper()
	body := f.send(t, "GET", path, "", actor, 200)
	var response struct {
		Data []domain.Task `json:"data"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Data == nil {
		t.Fatal("invalid task collection")
	}
	return response.Data, response.Meta.Total
}

func TestTaskQueryHTTPIntegration(t *testing.T) {
	f := newProjectHTTP(t)
	owner, member := f.users[0].ID, f.users[2].ID
	p := f.create(t, owner, "Query tasks")
	base := fmt.Sprintf("/api/v1/projects/%d", p.ID)
	f.send(t, "POST", base+"/members", fmt.Sprintf(`{"user_id":%d,"role":"member"}`, member), owner, 201)
	path := base + "/tasks"
	a := createHTTPTask(t, f, path, owner, fmt.Sprintf(`{"title":"Alpha","description":"Needle in description","priority":"high","assignee_id":%d,"due_date":"2026-10-21T00:00:00Z"}`, member))
	b := createHTTPTask(t, f, path, member, `{"title":"Beta","priority":"low","due_date":"2026-10-23T00:00:00Z"}`)
	c := createHTTPTask(t, f, path, owner, `{"title":"100%_literal","priority":"high"}`)
	f.send(t, "PATCH", fmt.Sprintf("%s/%d", path, b.ID), `{"status":"in_progress"}`, member, 200)
	other := f.create(t, owner, "Other")
	createHTTPTask(t, f, fmt.Sprintf("/api/v1/projects/%d/tasks", other.ID), owner, `{"title":"Alpha outside","priority":"high"}`)
	for _, tc := range []struct {
		query string
		want  []int64
	}{
		{"status=in_progress", []int64{b.ID}},
		{"priority=low", []int64{b.ID}},
		{fmt.Sprintf("assignee_id=%d", member), []int64{a.ID}},
		{fmt.Sprintf("creator_id=%d", owner), []int64{a.ID, c.ID}},
		{"due_from=2026-10-21T00:00:00Z&due_to=2026-10-21T00:00:00Z", []int64{a.ID}},
		{"due_from=2026-10-22T00:00:00Z", []int64{b.ID}},
		{"search=nEeDlE", []int64{a.ID}},
		{"search=ALPHA", []int64{a.ID}},
		{"search=" + url.QueryEscape("%_"), []int64{c.ID}},
		{"search=" + url.QueryEscape("'; DROP TABLE tasks; --"), []int64{}},
		{"status=todo&priority=high&sort=priority&order=desc", []int64{c.ID, a.ID}},
		{"sort=due_date&order=desc", []int64{b.ID, a.ID, c.ID}},
		{"sort=priority&order=asc", []int64{b.ID, a.ID, c.ID}},
	} {
		t.Run(tc.query, func(t *testing.T) {
			got, total := taskList(t, f, path+"?"+tc.query, owner)
			if total != len(tc.want) || len(got) != len(tc.want) {
				t.Fatalf("count=%d/%d want=%d", len(got), total, len(tc.want))
			}
			for i, id := range tc.want {
				if got[i].ID != id {
					t.Fatalf("position %d: got %d want %d", i, got[i].ID, id)
				}
			}
		})
	}
	for page, want := range []int64{c.ID, a.ID, 0} {
		got, total := taskList(t, f, fmt.Sprintf("%s?status=todo&priority=high&sort=priority&order=desc&page_size=1&page=%d", path, page+1), owner)
		if total != 2 || (want == 0 && len(got) != 0) || (want != 0 && (len(got) != 1 || got[0].ID != want)) {
			t.Fatal("unstable filtered pages")
		}
	}
	for _, query := range []string{"page=0", "page_size=101", "page=2147483648", "status=closed", "priority=urgent", "assignee_id=-1", "creator_id=9223372036854775808", "due_from=nope", "due_from=2026-10-23T00:00:00Z&due_to=2026-10-21T00:00:00Z", "sort=title%3BDROP%20TABLE%20tasks", "order=sideways", "search=x&search=y", "status=", "unknown=x", "search=%00", "search=%ZZ"} {
		f.send(t, "GET", path+"?"+query, "", owner, 400)
	}
	f.send(t, "GET", path+"?search=Alpha", "", f.users[3].ID, 403)
}
