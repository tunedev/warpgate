package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type nopLogger struct{}

func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

func TestIPFilter_BlocksCIDR(t *testing.T) {
	logger := nopLogger{}
	mw, err := IPFilter(logger, []string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("IPFilter error: %v", err)
	}

	blocked := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		blocked = false
	})

	h := mw(next)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "10.1.2.3:12345"

	rr := httptest.NewRecorder()
	blocked = true
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", rr.Code)
	}

	if !blocked {
		t.Fatal("expected blocked to be true")
	}
}

func TestIPFilter_AllowedNonBlockedIP(t *testing.T) {
	logger := nopLogger{}
	mw, err := IPFilter(logger, []string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("IPFilter error: %v", err)
	}

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	h := mw(next)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "192.168.1.2:12345"

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if !called {
		t.Fatalf("expected next handler to be called for allowed IP")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
}
