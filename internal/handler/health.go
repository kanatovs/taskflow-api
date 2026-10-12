package handler

import "net/http"

func Health(w http.ResponseWriter, r *http.Request) {
	writeData(w, r, http.StatusOK, map[string]string{"status": "ok"})
}
