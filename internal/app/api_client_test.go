package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/sessionapi"
)

type apiClient struct {
	t *testing.T
	app *App
	cookies map[string]string
	csrf string
}

type apiErr struct {
	Error struct {
		Code string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (e *testEnv) api() *apiClient {
	return &apiClient{t: e.t, app: e.app, cookies: map[string]string{}}
}

func (c *apiClient) send(method, path, body, csrf string) *httptest.ResponseRecorder {
	c.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		r.Header.Set(auth.CSRFHeader, csrf)
	}
	for name, value := range c.cookies {
		r.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	w := httptest.NewRecorder()
	c.app.ServeHTTP(w, r)
	for _, ck := range w.Result().Cookies() {
		if ck.MaxAge < 0 || ck.Value == "" {
			delete(c.cookies, ck.Name)
		} else {
			c.cookies[ck.Name] = ck.Value
		}
	}
	return w
}

func (c *apiClient) fetchCSRF() {
	c.t.Helper()
	w := c.send("GET", "/api/csrf", "", "")
	expectStatus(c.t, w, http.StatusOK)
	var out sessionapi.CSRFResponse
	decodeBody(c.t, w, &out)
	if out.CSRFToken == "" {
		c.t.Fatal("token CSRF kosong")
	}
	c.csrf = out.CSRFToken
}

func (c *apiClient) login(username, password string) *httptest.ResponseRecorder {
	c.t.Helper()
	c.fetchCSRF()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	w := c.send("POST", "/api/login", string(body), c.csrf)
	if w.Code == http.StatusOK {
		var me sessionapi.MeResponse
		decodeBody(c.t, w, &me)
		c.csrf = me.CSRFToken
	}
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type %q, body: %s", ct, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("body bukan JSON: %v, body: %s", err, w.Body.String())
	}
}

func expectAPIError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	expectStatus(t, w, status)
	var e apiErr
	decodeBody(t, w, &e)
	if e.Error.Code != code || e.Error.Message == "" {
		t.Fatalf("error %+v, want code %q", e.Error, code)
	}
}

func (c *apiClient) json(method, path string, v any) *httptest.ResponseRecorder {
	c.t.Helper()
	body := ""
	if v != nil {
		b, err := json.Marshal(v)
		if err != nil {
			c.t.Fatal(err)
		}
		body = string(b)
	}
	return c.send(method, path, body, c.csrf)
}

func (c *apiClient) session() string {
	return c.cookies[auth.SessionCookie]
}

func (c *apiClient) withSession(token string) *apiClient {
	return &apiClient{t: c.t, app: c.app, cookies: map[string]string{auth.SessionCookie: token}, csrf: c.csrf}
}

func (e *testEnv) apiLogin(username, password string) *apiClient {
	e.t.Helper()
	c := e.api()
	expectStatus(e.t, c.login(username, password), http.StatusOK)
	return c
}

func expectInvalidField(t *testing.T, w *httptest.ResponseRecorder, field string) {
	t.Helper()
	expectStatus(t, w, http.StatusUnprocessableEntity)
	var e struct {
		Error struct {
			Code string `json:"code"`
			Details map[string]string `json:"details"`
		} `json:"error"`
	}
	decodeBody(t, w, &e)
	if e.Error.Code != "validation_error" || e.Error.Details[field] == "" {
		t.Fatalf("butuh validation_error untuk %q, dapat %s", field, w.Body.String())
	}
}
