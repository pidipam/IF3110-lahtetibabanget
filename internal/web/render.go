package web

import (
	"bytes"
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/users"
)

//go:embed templates/*.html
var templateFiles embed.FS

type View struct {
	Title string
	Nav string
	User *users.User
	CSRF string
	Notice string
	Error string
	Data map[string]any
}

type Renderer struct {
	pages map[string]*template.Template
}

func NewRenderer() (*Renderer, error) {
	names, err := fs.Glob(templateFiles, "templates/*.html")
	if err != nil {
		return nil, err
	}
	r := &Renderer{pages: map[string]*template.Template{}}
	for _, name := range names {
		base := path.Base(name)
		if base == "layout.html" {
			continue
		}
		t, err := template.New(base).ParseFS(templateFiles, "templates/layout.html", name)
		if err != nil {
			return nil, err
		}
		r.pages[strings.TrimSuffix(base, ".html")] = t
	}
	return r, nil
}

func (rd *Renderer) Render(w http.ResponseWriter, status int, page string, v View) {
	t, ok := rd.pages[page]
	if !ok {
		http.Error(w, "Terjadi kesalahan pada server.", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", v); err != nil {
		http.Error(w, "Terjadi kesalahan pada server.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}