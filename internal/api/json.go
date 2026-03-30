package api

import (
	"net/http"

	"wealthflow/backend/internal/respond"
)

func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	respond.JSON(w, status, v)
}

func WriteError(w http.ResponseWriter, status int, msg string) {
	respond.Error(w, status, msg)
}
