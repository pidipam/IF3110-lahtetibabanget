package actor

import (
	"context"
	"sync/atomic"
)

type key struct{}

type holder struct{ id atomic.Int64 }

func Track(ctx context.Context) context.Context {
	if _, ok := ctx.Value(key{}).(*holder); ok {
		return ctx
	}
	return context.WithValue(ctx, key{}, &holder{})
}

func Set(ctx context.Context, id int64) context.Context {
	if h, ok := ctx.Value(key{}).(*holder); ok {
		h.id.Store(id)
		return ctx
	}
	h := &holder{}
	h.id.Store(id)
	return context.WithValue(ctx, key{}, h)
}

func From(ctx context.Context) (int64, bool) {
	h, ok := ctx.Value(key{}).(*holder)
	if !ok {
		return 0, false
	}
	id := h.id.Load()
	return id, id != 0
}
