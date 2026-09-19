package auth

import (
	"net/http"
	"net/http/httptest"
)

func newRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }

func newRequest() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/auth/callback", nil)
}
