package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattventura/respond/internal/middleware"
)

func TestFSAuth_ValidSecret(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := middleware.FSAuth("mysecret", next)

	req := httptest.NewRequest("POST", "/fs/voice", nil)
	req.Header.Set("X-FS-Secret", "mysecret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("next handler was not called with valid secret")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestFSAuth_InvalidSecret(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := middleware.FSAuth("mysecret", next)

	req := httptest.NewRequest("POST", "/fs/voice", nil)
	req.Header.Set("X-FS-Secret", "wrongsecret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("next handler should not be called with invalid secret")
	}
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestFSAuth_MissingSecret(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := middleware.FSAuth("mysecret", next)

	req := httptest.NewRequest("POST", "/fs/voice", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("next handler should not be called with missing secret")
	}
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}
