package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type dataResponse struct {
	Data any `json:"data"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func writeData(w http.ResponseWriter, r *http.Request, status int, data any) {
	writeJSON(w, r, status, dataResponse{Data: data})
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}

	writeJSON(w, r, status, errorResponse{
		Error: errorBody{Code: code, Message: message, Details: details},
	})
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, response any) {
	// Serialize before committing headers so an encoding failure can return 500.
	body, err := json.Marshal(response)
	if err != nil {
		slog.ErrorContext(r.Context(), "encode JSON response", "error", err)
		status = http.StatusInternalServerError
		// A fixed JSON body avoids another serialization failure while handling one.
		body = []byte(`{"error":{"code":"internal_error","message":"Internal server error","details":{}}}`)
	}
	body = append(body, '\n')

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		// The response has started; attempting another response would corrupt it.
		slog.ErrorContext(r.Context(), "write JSON response", "error", err)
	}
}
