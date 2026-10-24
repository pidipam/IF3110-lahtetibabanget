package testutil

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
)

var csrfPattern = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

type Client struct {
	t       testing.TB
	h       http.Handler
	Cookies map[string]string
	CSRF    string
}

func NewClient(t testing.TB, h http.Handler) *Client {
	return &Client{t: t, h: h, Cookies: map[string]string{}}
}

// mengirim request
func (c *Client) Do(method, path string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, path, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for name, value := range c.Cookies {
		r.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	for _, ck := range w.Result().Cookies() {
		if ck.MaxAge < 0 || ck.Value == "" {
			delete(c.Cookies, ck.Name)
		} else {
			c.Cookies[ck.Name] = ck.Value
		}
	}
	if m := csrfPattern.FindStringSubmatch(w.Body.String()); m != nil {
		c.CSRF = m[1]
	}
	return w
}

func (c *Client) Post(path string, form url.Values) *httptest.ResponseRecorder {
	if form == nil {
		form = url.Values{}
	}
	form.Set(auth.CSRFField, c.CSRF)
	return c.Do("POST", path, form)
}

func (c *Client) Login(username, password string) *httptest.ResponseRecorder {
	c.Do("GET", "/login", nil)
	w := c.Post("/login", url.Values{"username": {username}, "password": {password}})
	if w.Code == http.StatusSeeOther {
		c.Do("GET", "/", nil)
	}
	return w
}

// mengembalikan nilai cookie session 
func (c *Client) Session() string {
	return c.Cookies[auth.SessionCookie]
}

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
