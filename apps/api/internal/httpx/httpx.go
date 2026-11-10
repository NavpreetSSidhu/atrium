// Package httpx holds the JSON request/response helpers shared by every module's handlers.
package httpx

import (
	"encoding/json"
	"net/http"
)

// ReadJSON decodes a body of at most 64 KiB into v, writing a 400 and returning false on failure.
func ReadJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(v); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Error writes {"error": msg}. Never pass internal error text: it reaches the client.
func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}
