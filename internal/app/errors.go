package app

import (
	"net/http"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/httpapi"
)

func (a *App) RenderError(w http.ResponseWriter, r *http.Request, status int, title, message string, showLogin bool) {
	a.Render.Render(w, status, "error", a.View(r, title, map[string]any{"Code": status, "Message": message, "ShowLogin": showLogin}))
}

func (a *App) unauthorized(w http.ResponseWriter, r *http.Request, expired bool) {
	if httpapi.IsAPI(r) {
		httpapi.Unauthenticated(w, expired)
		return
	}
	if expired {
		a.RenderError(w, r, http.StatusUnauthorized, "Sesi Berakhir", "Sesi Anda sudah berakhir atau dicabut. Silakan login kembali.", true)
		return
	}
	if r.Method == http.MethodGet {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	a.RenderError(w, r, http.StatusUnauthorized, "Perlu Login", "Anda harus login untuk mengakses halaman ini.", true)
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
	a.RenderError(w, r, http.StatusForbidden, "Akses Ditolak", "Anda tidak memiliki izin untuk melakukan aksi ini.", false)
}

func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	a.Logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	if httpapi.IsAPI(r) {
		httpapi.Internal(w)
		return
	}
	a.RenderError(w, r, http.StatusInternalServerError, "Kesalahan Server", "Terjadi kesalahan pada server. Silakan coba lagi.", false)
}

func (a *App) notFound(w http.ResponseWriter, r *http.Request) {
	if httpapi.IsAPI(r) {
		httpapi.NotFound(w)
		return
	}
	a.RenderError(w, r, http.StatusNotFound, "Tidak Ditemukan", "Halaman yang Anda cari tidak ada.", false)
}
