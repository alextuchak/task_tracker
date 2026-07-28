package httpkit

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

const statusClientClosed = 499

type ErrorResponse struct {
	Error string `json:"error"`
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		slog.Error("response encode failed", slog.Int("status", status), slog.Any("error", err))
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, ErrorResponse{Error: message})
}

func WriteInternalError(w http.ResponseWriter, r *http.Request, err error) {
	status, message := http.StatusInternalServerError, "internal error"
	if r.Context().Err() != nil {
		status, message = statusClientClosed, "client closed request"
	}
	slog.Error("unhandled error",
		slog.String("method", r.Method), slog.String("path", r.URL.Path),
		slog.Int("status", status), slog.Any("error", err))
	WriteError(w, status, message)
}
