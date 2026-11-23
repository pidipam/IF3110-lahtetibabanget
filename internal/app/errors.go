package app

import (
	"net/http"
	"strconv"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/httpapi"
	webui "github.com/pidipam/IF3110-lahtetibabanget/web"
)

func (a *App) page(w http.ResponseWriter, r *http.Request, status int, target string) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
	http.Error(w, http.StatusText(status), status)
}

func errorPage(status int) string {
	return webui.Base + "error/" + strconv.Itoa(status)
}

func (a *App) unauthorized(w http.ResponseWriter, r *http.Request, expired bool) {
	if httpapi.IsAPI(r) {
		httpapi.Unauthenticated(w, expired)
		return
	}
	if expired {
		a.page(w, r, http.StatusUnauthorized, webui.Base+"sesi-berakhir")
		return
	}
	a.page(w, r, http.StatusUnauthorized, webui.Base+"login")
}

func (a *App) forbidden(w http.ResponseWriter, r *http.Request) {
	if httpapi.IsAPI(r) {
		if !auth.IsSafeMethod(r.Method) && !auth.ValidCSRF(auth.CSRFToken(r.Context()), r) {
			httpapi.CSRFInvalid(w)
			return
		}
		httpapi.Forbidden(w)
		return
	}
	a.page(w, r, http.StatusForbidden, errorPage(http.StatusForbidden))
}

func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	a.Logger.Error("request failed", "method", r.Method, "path", httpapi.LogPath(r), "err", err)
	if httpapi.IsAPI(r) {
		httpapi.Internal(w)
		return
	}
	a.page(w, r, http.StatusInternalServerError, errorPage(http.StatusInternalServerError))
}

func (a *App) notFound(w http.ResponseWriter, r *http.Request) {
	if httpapi.IsAPI(r) {
		httpapi.NotFound(w)
		return
	}
	a.page(w, r, http.StatusNotFound, errorPage(http.StatusNotFound))
}
