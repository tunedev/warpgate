package middleware

import (
	"net"
	"net/http"
	"strings"
	"warpgate/internal/logging"
)

type ipFilter struct {
	logger logging.Logger
	nets   []*net.IPNet
}

// IPFilter constructs a middleware that blocks requests from client IPs
// within any of the given CIDR ranges.
func IPFilter(logger logging.Logger, cidrs []string) (Middleware, error) {
	if len(cidrs) == 0 {
		return func(next http.Handler) http.Handler {
			return next
		}, nil
	}

	var nets []*net.IPNet
	for _, c := range cidrs {
		_, ipnet, err := net.ParseCIDR(c)
		if err != nil {
			return nil, err
		}
		nets = append(nets, ipnet)
	}

	f := &ipFilter{
		logger: logger,
		nets:   nets,
	}

	return f.middleware, nil
}

func (f *ipFilter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := f.extractClientIP(r)
		if clientIP == nil {
			next.ServeHTTP(w, r)
			return
		}

		for _, n := range f.nets {
			if n.Contains(clientIP) {
				if f.logger != nil {
					f.logger.Info("ip blocked",
						"ip", clientIP.String(),
						"path", r.URL.Path,
					)
				}
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func (f *ipFilter) extractClientIP(r *http.Request) net.IP {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ipStr := strings.TrimSpace(parts[0])
			if ip := net.ParseIP(ipStr); ip != nil {
				return ip
			}
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return net.ParseIP(r.RemoteAddr)
	}
	return net.ParseIP(host)
}
