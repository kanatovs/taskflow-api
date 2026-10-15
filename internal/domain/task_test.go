package domain

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestTaskTransitionMatrix(t *testing.T) {
	states := []TaskStatus{TaskTodo, TaskInProgress, TaskDone, "invalid"}
	allowed := map[[2]TaskStatus]bool{
		{TaskTodo, TaskTodo}: true, {TaskTodo, TaskInProgress}: true,
		{TaskInProgress, TaskTodo}: true, {TaskInProgress, TaskInProgress}: true, {TaskInProgress, TaskDone}: true,
		{TaskDone, TaskInProgress}: true, {TaskDone, TaskDone}: true,
	}
	for _, from := range states {
		for _, to := range states {
			if got := CanTransitionTask(from, to); got != allowed[[2]TaskStatus{from, to}] {
				t.Fatalf("%s -> %s: %v", from, to, got)
			}
		}
	}
}

func TestTaskUpdatePermissions(t *testing.T) {
	assignee := int64(2)
	task := Task{CreatorID: 1, AssigneeID: &assignee, Status: TaskTodo}
	status := TaskInProgress
	title := "Edit"
	for _, tc := range []struct {
		role    ProjectRole
		actor   int64
		changes TaskChanges
		want    error
	}{
		{RoleOwner, 3, TaskChanges{Title: &title}, nil},
		{RoleAdmin, 3, TaskChanges{Title: &title}, nil},
		{RoleMember, 1, TaskChanges{Title: &title}, nil},
		{RoleMember, 2, TaskChanges{Status: &status}, nil},
		{RoleMember, 2, TaskChanges{Title: &title}, ErrForbidden},
		{RoleMember, 3, TaskChanges{Status: &status}, ErrForbidden},
	} {
		if err := ValidateTaskUpdate(tc.role, tc.actor, task, tc.changes); !errors.Is(err, tc.want) {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	done := TaskDone
	if err := ValidateTaskUpdate(RoleOwner, 1, task, TaskChanges{Status: &done}); !errors.Is(err, ErrConflict) {
		t.Fatal("invalid transition accepted")
	}
}

func TestNullableTaskPatch(t *testing.T) {
	for _, tc := range []struct {
		body  string
		set   bool
		value *int64
	}{
		{`{}`, false, nil}, {`{"assignee_id":null}`, true, nil},
	} {
		var patch TaskChanges
		if err := json.Unmarshal([]byte(tc.body), &patch); err != nil || patch.AssigneeID.Set != tc.set || patch.AssigneeID.Value != tc.value {
			t.Fatal("nullable semantics lost")
		}
	}
	var patch TaskChanges
	if err := json.Unmarshal([]byte(`{"assignee_id":42}`), &patch); err != nil || !patch.AssigneeID.Set || patch.AssigneeID.Value == nil || *patch.AssigneeID.Value != 42 {
		t.Fatal("assignment not decoded")
	}
}
