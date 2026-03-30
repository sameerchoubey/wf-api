package respond

import (
	"encoding/json"
	"net/http"
)

func JSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type ErrorBody struct {
	Detail string `json:"detail"`
}

func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, ErrorBody{Detail: msg})
}
