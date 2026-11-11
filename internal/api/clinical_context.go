package api

import "context"

type specialtyContextKey struct{}

// SpecialtyContextID identifies a request's clinical scope, never its authority.
func SpecialtyContextID(ctx context.Context) int64 {
	id, _ := ctx.Value(specialtyContextKey{}).(int64)
	return id
}
func WithSpecialtyContext(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, specialtyContextKey{}, id)
}
