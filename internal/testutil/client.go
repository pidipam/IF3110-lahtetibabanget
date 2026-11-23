package testutil

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
)

func ExpectStatus(t testing.TB, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status %d, want %d, body: %s", w.Code, want, w.Body.String())
	}
}

// mengembalikan header Set-Cookie untuk session
func SessionSetCookie(w *httptest.ResponseRecorder) string {
	for _, h := range w.Result().Header.Values("Set-Cookie") {
		if strings.HasPrefix(h, auth.SessionCookie+"=") {
			return h
		}
	}
	return ""
}
