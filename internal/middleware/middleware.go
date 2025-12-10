package middleware

import "net/http"

type Middleware func(http.Handler) http.Handler

// Chain applies middlewares in order: m1(m2(...(h))).
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
