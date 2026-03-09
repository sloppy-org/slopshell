package web

import (
	"encoding/json"
	"net/http"
)

func writeJSONStatus(w http.ResponseWriter, status int, payload any) {
	if status == 0 {
		status = http.StatusOK
	}
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSON(w http.ResponseWriter, payload any) {
	writeJSONStatus(w, http.StatusOK, payload)
}

func writeAPIData(w http.ResponseWriter, status int, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	payload := map[string]any{
		"ok":   true,
		"data": data,
	}
	for key, value := range data {
		payload[key] = value
	}
	writeJSONStatus(w, status, payload)
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSONStatus(w, status, map[string]any{
		"error": message,
	})
}

func writeNoContent(w http.ResponseWriter) {
	writeJSONStatus(w, http.StatusNoContent, nil)
}
