package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	response := map[string]map[string]string{
		"data": {"status": "ok"},
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("write health response", "error", err)
	}
}
