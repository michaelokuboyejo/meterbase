package respond

import (
	"encoding/json"
	"net/http"
)

type errDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errBody struct {
	Error errDetail `json:"error"`
}

func JSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func Error(w http.ResponseWriter, status int, code, message string) {
	JSON(w, status, errBody{Error: errDetail{Code: code, Message: message}})
}
