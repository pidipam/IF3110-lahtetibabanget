package actor

import (
	"context"
	"testing"
)

func TestTrackThenSetVisibleToOuterContext(t *testing.T) {
	outer := Track(context.Background())
	if _, ok := From(outer); ok {
		t.Fatal("wadah kosong belum punya actor")
	}
	inner := Set(outer, 42)
	if id, ok := From(outer); !ok || id != 42 {
		t.Fatalf("middleware luar harus melihat actor: %d %v", id, ok)
	}
	if id, _ := From(inner); id != 42 {
		t.Fatalf("inner: %d", id)
	}
}

func TestSetWithoutTrack(t *testing.T) {
	ctx := Set(context.Background(), 7)
	if id, ok := From(ctx); !ok || id != 7 {
		t.Fatalf("Set tanpa Track: %d %v", id, ok)
	}
	if _, ok := From(context.Background()); ok {
		t.Fatal("context polos tidak punya actor")
	}
}
