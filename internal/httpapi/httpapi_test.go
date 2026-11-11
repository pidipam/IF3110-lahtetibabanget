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

func TestParsePage(t *testing.T) {
	cases := []struct {
		query string
		page int
		size int
		ok bool
	}{{"", 1, DefaultPageSize, true}, {"page=3&page_size=50", 3, 50, true}, {"page_size=100", 1, 100, true}, {"page=0", 0, 0, false}, {"page=-1", 0, 0, false}, {"page=abc", 0, 0, false}, {"page_size=0", 0, 0, false}, {"page_size=101", 0, 0, false}, {"page=1.5", 0, 0, false}}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		p, ok := ParsePage(w, httptest.NewRequest("GET", "/api/x?"+tc.query, nil))
		if ok != tc.ok {
			t.Fatalf("%q: ok %v, want %v", tc.query, ok, tc.ok)
		}
		if !ok {
			if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "validation_error") {
				t.Fatalf("%q: status %d body %s", tc.query, w.Code, w.Body.String())
			}
			continue
		}
		if p.Page != tc.page || p.PageSize != tc.size {
			t.Fatalf("%q: got %+v", tc.query, p)
		}
	}
	if (PageParams{Page: 3, PageSize: 20}).Offset() != 40 {
		t.Fatal("offset salah")
	}
}

func TestNewPageNeverNull(t *testing.T) {
	b, _ := json.Marshal(NewPage[int](nil, PageParams{Page: 2, PageSize: 10}, 15))
	if string(b) != `{"items":[],"page":2,"page_size":10,"total":15}` {
		t.Fatalf("got %s", b)
	}
}

func TestPathID(t *testing.T) {
	mux := http.NewServeMux()
	var got int64
	var ok bool
	mux.HandleFunc("/x/{id}", func(w http.ResponseWriter, r *http.Request) { got, ok = PathID(r, "id") })
	for path, want := range map[string]int64{"/x/12": 12, "/x/0": 0, "/x/-3": 0, "/x/abc": 0, "/x/99999999999999999999": 0} {
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
		if ok != (want > 0) || (ok && got != want) {
			t.Fatalf("%s: got %d %v", path, got, ok)
		}
	}
}
