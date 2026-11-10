package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const Prefix = "/api/"

const MaxBodyBytes = 1 << 20

const DefaultPageSize = 20

const MaxPageSize = 100

var ErrInvalidBody = errors.New("body JSON tidak valid")

type ErrorBody struct {
	Code string `json:"code"`
	Message string `json:"message"`
	Details any `json:"details,omitempty"`
}

type PageParams struct {
	Page int
	PageSize int
}

type Page[T any] struct {
	Items []T `json:"items"`
	Page int `json:"page"`
	PageSize int `json:"page_size"`
	Total int `json:"total"`
}

type errorEnvelope struct {
	Error ErrorBody `json:"error"`
}

func IsAPI(r *http.Request) bool {
	return r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, Prefix)
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, status int, code, message string, details any) {
	WriteJSON(w, status, errorEnvelope{Error: ErrorBody{Code: code, Message: message, Details: details}})
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return ErrInvalidBody
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalidBody
	}
	return nil
}

func Unauthenticated(w http.ResponseWriter, expired bool) {
	if expired {
		WriteError(w, http.StatusUnauthorized, "session_expired", "Sesi Anda sudah berakhir. Silakan login kembali.", nil)
		return
	}
	WriteError(w, http.StatusUnauthorized, "unauthenticated", "Anda harus login.", nil)
}

func Forbidden(w http.ResponseWriter) {
	WriteError(w, http.StatusForbidden, "forbidden", "Anda tidak memiliki izin untuk aksi ini.", nil)
}

func CSRFInvalid(w http.ResponseWriter) {
	WriteError(w, http.StatusForbidden, "csrf_invalid", "Token CSRF tidak valid. Muat ulang halaman.", nil)
}

func NotFound(w http.ResponseWriter) {
	WriteError(w, http.StatusNotFound, "not_found", "Sumber tidak ditemukan.", nil)
}

func Internal(w http.ResponseWriter) {
	WriteError(w, http.StatusInternalServerError, "internal", "Terjadi kesalahan pada server.", nil)
}

func Invalid(w http.ResponseWriter, message string, details any) {
	WriteError(w, http.StatusUnprocessableEntity, "validation_error", message, details)
}

func (p PageParams) Offset() int {
	return (p.Page - 1) * p.PageSize
}

func ParsePage(w http.ResponseWriter, r *http.Request) (PageParams, bool) {
	p := PageParams{Page: 1, PageSize: DefaultPageSize}
	details := map[string]string{}
	q := r.URL.Query()
	if v := q.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			details["page"] = "Page harus bilangan bulat minimal 1."
		}
		p.Page = n
	}
	if v := q.Get("page_size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > MaxPageSize {
			details["page_size"] = "Page size harus bilangan bulat 1 sampai " + strconv.Itoa(MaxPageSize) + "."
		}
		p.PageSize = n
	}
	if len(details) > 0 {
		Invalid(w, "Parameter paginasi tidak valid.", details)
		return p, false
	}
	return p, true
}

func NewPage[T any](items []T, p PageParams, total int) Page[T] {
	if items == nil {
		items = []T{}
	}
	return Page[T]{Items: items, Page: p.Page, PageSize: p.PageSize, Total: total}
}

func PathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}
