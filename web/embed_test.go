package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func serve(h http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}

func TestSPAFallback(t *testing.T) {
	h := NewHandler(fstest.MapFS{"index.html": {Data: []byte("<html>spa</html>")}, "assets/app.js": {Data: []byte("console.log(1)")}, "favicon.svg": {Data: []byte("<svg/>")}})
	for _, path := range []string{"/app/", "/app/login", "/app/admin/users/3", "/app/index.html"} {
		w := serve(h, path)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "spa") {
			t.Fatalf("%s: status %d body %q", path, w.Code, w.Body.String())
		}
	}
	w := serve(h, "/app/assets/app.js")
	if w.Code != http.StatusOK || w.Body.String() != "console.log(1)" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("aset: status %d cache %q", w.Code, w.Header().Get("Cache-Control"))
	}
	if w := serve(h, "/app/favicon.svg"); w.Code != http.StatusOK || w.Body.String() != "<svg/>" {
		t.Fatalf("favicon: %d", w.Code)
	}
	if w := serve(h, "/app/assets/hilang.js"); w.Code != http.StatusNotFound {
		t.Fatalf("aset hilang harus 404, dapat %d", w.Code)
	}
	if w := serve(h, "/app/../../etc/passwd"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "spa") {
		t.Fatalf("path traversal: %d", w.Code)
	}
}

func TestSPANotBuilt(t *testing.T) {
	w := serve(NewHandler(fstest.MapFS{".gitkeep": {Data: nil}}), "/app/")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", w.Code)
	}
}
