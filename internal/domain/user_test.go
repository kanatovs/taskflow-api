package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserJSONExcludesPasswordHash(t *testing.T) {
	body, err := json.Marshal(User{ID: 1, Email: "user@example.com", PasswordHash: "private-hash"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "password") || strings.Contains(string(body), "private-hash") {
		t.Fatal("password hash exposed")
	}
}
