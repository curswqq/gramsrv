package domain

import "context"

type clientIPCtxKey struct{}

// WithClientIP injects the client remote IP address into context.
func WithClientIP(ctx context.Context, ip string) context.Context {
	if ip == "" {
		return ctx
	}
	return context.WithValue(ctx, clientIPCtxKey{}, ip)
}

// ClientIPFrom extracts the client remote IP address from context.
func ClientIPFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(clientIPCtxKey{}).(string); ok {
		return v
	}
	return ""
}
