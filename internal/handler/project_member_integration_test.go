package handler_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

func TestMembershipHTTPIntegration(t *testing.T) {
	f := newProjectHTTP(t)
	owner, admin, member, outsider, target := f.users[0].ID, f.users[1].ID, f.users[2].ID, f.users[3].ID, f.users[4].ID
	p := f.create(t, owner, "Members")
	base := fmt.Sprintf("/api/v1/projects/%d/members", p.ID)
	path := func(id int64) string { return fmt.Sprintf("%s/%d", base, id) }
	add := func(actor, user int64, role string, want int) {
		t.Helper()
		f.send(t, "POST", base, fmt.Sprintf(`{"user_id":%d,"role":%q}`, user, role), actor, want)
	}
	f.send(t, "GET", base, "", 0, 401)
	f.send(t, "GET", base, "", outsider, 403)
	add(owner, admin, "admin", 201)
	add(owner, member, "member", 201)
	add(owner, member, "member", 409)
	add(owner, 999999, "member", 404)
	add(owner, target, "owner", 400)
	add(owner, target, "invalid", 400)
	add(owner, 0, "member", 400)
	for _, actor := range []int64{owner, admin, member} {
		f.send(t, "GET", base, "", actor, 200)
	}
	listed := f.send(t, "GET", base+"?page=1&page_size=1", "", member, 200)
	var response struct {
		Data []domain.ProjectMember `json:"data"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(listed, &response); err != nil || len(response.Data) != 1 || response.Meta.Total != 3 {
		t.Fatal("incorrect member pagination")
	}
	add(member, target, "member", 403)
	add(admin, target, "admin", 403)
	add(admin, target, "member", 201)
	f.send(t, "PATCH", path(target), `{"role":"admin"}`, admin, 403)
	f.send(t, "PATCH", path(owner), `{"role":"member"}`, admin, 403)
	f.send(t, "PATCH", path(admin), `{"role":"member"}`, admin, 403)
	f.send(t, "DELETE", path(owner), "", admin, 403)
	f.send(t, "DELETE", path(admin), "", admin, 403)
	f.send(t, "PATCH", path(owner), `{"role":"member"}`, owner, 403)
	f.send(t, "DELETE", path(owner), "", owner, 403)
	f.send(t, "PATCH", path(target), `{"role":"admin"}`, member, 403)
	f.send(t, "DELETE", path(target), "", member, 403)
	f.send(t, "DELETE", path(target), "", admin, 204)
	f.send(t, "DELETE", path(target), "", owner, 404)
	add(owner, target, "member", 201)
	f.send(t, "PATCH", path(target), `{"role":"admin"}`, owner, 200)
	f.send(t, "DELETE", path(target), "", admin, 403)
	f.send(t, "PATCH", path(target), `{"role":"member"}`, owner, 200)
	f.send(t, "DELETE", path(target), "", owner, 204)
	other := f.create(t, outsider, "Other project")
	otherBase := fmt.Sprintf("/api/v1/projects/%d/members", other.ID)
	f.send(t, "POST", otherBase, fmt.Sprintf(`{"user_id":%d,"role":"member"}`, target), outsider, 201)
	f.send(t, "PATCH", path(target), `{"role":"admin"}`, owner, 404)
	f.send(t, "DELETE", path(target), "", owner, 404)
	f.send(t, "DELETE", fmt.Sprintf("%s/%d", otherBase, target), "", owner, 403)
	f.send(t, "GET", otherBase, "", target, 200)
	f.send(t, "PATCH", path(member), `{"role":"admin","user_id":999}`, owner, 400)
	f.send(t, "PATCH", base+"/bad", `{"role":"member"}`, owner, 400)
	f.send(t, "DELETE", path(admin), "", owner, 204)
	f.send(t, "GET", base, "", admin, 403)
}
