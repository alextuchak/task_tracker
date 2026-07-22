package identity

import "context"

type Principal struct {
	UserID int64
}

type ctxKey int

const ctxKeyPrincipal ctxKey = iota

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKeyPrincipal, p)
}

func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKeyPrincipal).(Principal)
	return p, ok
}
