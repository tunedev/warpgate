package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChain(t *testing.T) {
	var calls []string

	m1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, "m1-before")
			next.ServeHTTP(w, r)
			calls = append(calls, "m1-after")
		})
	}
	m2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, "m2-before")
			next.ServeHTTP(w, r)
			calls = append(calls, "m2-after")
		})
	}
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "handler")
	})

	h := Chain(final, m1, m2)

	req := httptest.NewRequest(http.MethodGet, "http://examople.com/", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	expected := []string{
		"m1-before",
		"m2-before",
		"handler",
		"m2-after",
		"m1-after",
	}
	if len(calls) != len(expected) {
		t.Fatalf("expected %d calls, got %d", len(expected), len(calls))
	}
	for i := range expected {
		if calls[i] != expected[i] {
			t.Errorf("at %d: expected %q, got %q", i, expected[1], calls[i])
		}
	}
}
