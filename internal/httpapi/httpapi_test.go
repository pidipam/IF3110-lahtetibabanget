package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteErrorFormat(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusConflict, "conflict", "Bentrok.", map[string]int{"id": 3})
	if w.Code != http.StatusConflict || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status %d, content-type %q", w.Code, w.Header().Get("Content-Type"))
	}
	var got map[string]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	e := got["error"]
	if e["code"] != "conflict" || e["message"] != "Bentrok." || e["details"].(map[string]any)["id"] != float64(3) {
		t.Fatalf("isi error salah: %v", got)
	}
}

func TestWriteErrorOmitsEmptyDetails(t *testing.T) {
	w := httptest.NewRecorder()
	NotFound(w)
	if strings.Contains(w.Body.String(), "details") {
		t.Fatalf("details kosong harus dihilangkan: %s", w.Body.String())
	}
}

func TestDecodeJSON(t *testing.T) {
	type in struct {
		Name string `json:"name"`
	}
	cases := []struct {
		body string
		ok bool
	}{{`{"name":"a"}`, true}, {`{"name":"a","x":1}`, false}, {`{"name":"a"}{}`, false}, {`nope`, false}, {``, false}, {`{"name":"` + strings.Repeat("a", MaxBodyBytes) + `"}`, false}}
	for _, tc := range cases {
		r := httptest.NewRequest("POST", "/api/x", strings.NewReader(tc.body))
		var v in
		err := DecodeJSON(httptest.NewRecorder(), r, &v)
		if (err == nil) != tc.ok {
			t.Fatalf("body %.30q: err %v", tc.body, err)
		}
	}
}

func TestIsAPI(t *testing.T) {
	for path, want := range map[string]bool{"/api": true, "/api/me": true, "/apix": false, "/app/api": false, "/": false} {
		if got := IsAPI(httptest.NewRequest("GET", path, nil)); got != want {
			t.Fatalf("%s: %v", path, got)
		}
	}
}
