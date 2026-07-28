package httpkit

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

const statusClientClosed = 499

type ErrorResponse struct {
	Error string `json:"error"`
}

// DecodeJSON reads the body into v and answers the client itself on failure:
// 413 when the body ran past the limit the middleware set, 400 otherwise. It
// reports whether the handler may carry on.
func DecodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			WriteError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		WriteError(w, http.StatusBadRequest, "invalid json")
		return false
	}
	return true
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
