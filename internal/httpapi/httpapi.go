package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const Prefix = "/api/"

const MaxBodyBytes = 1 << 20

var ErrInvalidBody = errors.New("body JSON tidak valid")

type ErrorBody struct {
	Code string `json:"code"`
	Message string `json:"message"`
	Details any `json:"details,omitempty"`
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
