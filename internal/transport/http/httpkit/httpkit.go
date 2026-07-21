package httpkit

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
)

type ErrorResponse struct {
	Error string `json:"error"`
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, ErrorResponse{Error: message})
}

func WriteInternalError(w http.ResponseWriter, err error) {
	slog.Error("unhandled error", slog.Any("error", err))
	WriteError(w, http.StatusInternalServerError, "internal error")
}

type ctxKey int

const ctxKeyUserID ctxKey = iota

func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, ctxKeyUserID, userID)
}

func UserIDFromContext(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(ctxKeyUserID).(int64)
	return id, ok
}
