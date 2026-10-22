package app

import "net/http"

func (a *App) RenderError(w http.ResponseWriter, r *http.Request, status int, title, message string, showLogin bool) {
	a.Render.Render(w, status, "error", a.View(r, title, map[string]any{"Code": status, "Message": message, "ShowLogin": showLogin}))
}

func (a *App) unauthorized(w http.ResponseWriter, r *http.Request, expired bool) {
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
	a.RenderError(w, r, http.StatusForbidden, "Akses Ditolak", "Anda tidak memiliki izin untuk melakukan aksi ini.", false)
}

func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	a.Logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	a.RenderError(w, r, http.StatusInternalServerError, "Kesalahan Server", "Terjadi kesalahan pada server. Silakan coba lagi.", false)
}

func (a *App) notFound(w http.ResponseWriter, r *http.Request) {
	a.RenderError(w, r, http.StatusNotFound, "Tidak Ditemukan", "Halaman yang Anda cari tidak ada.", false)
}