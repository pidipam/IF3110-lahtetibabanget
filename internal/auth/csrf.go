package auth

import "net/http"

const CSRFField = "csrf_token"

const CSRFHeader = "X-CSRF-Token"

func IsSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func ValidCSRF(expected string, r *http.Request) bool {
	got := r.Header.Get(CSRFHeader)
	if got == "" {
		got = r.PostFormValue(CSRFField)
	}
	return TokensEqual(expected, got)
}
