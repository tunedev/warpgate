package proxy

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type SimpleRoute struct {
	Prefix       string
	Upstream     *url.URL
	CacheEnabled bool
	CacheTTL     time.Duration
}

type SimpleDirector struct {
	Routes []SimpleRoute
}

func NewSimpleDirector(routes []SimpleRoute) *SimpleDirector {
	return &SimpleDirector{Routes: routes}
}

func (d *SimpleDirector) Direct(req *http.Request) (*http.Request, RouteMetadata, error) {
	var route *SimpleRoute
	for i := range d.Routes {
		if strings.HasPrefix(req.URL.Path, d.Routes[i].Prefix) {
			route = &d.Routes[i]
			break
		}
	}
	if route == nil {
		return nil, RouteMetadata{}, fmt.Errorf("no route for path %s", req.URL.Path)
	}

	outReq := req.Clone(req.Context())

	outReq.URL.Scheme = route.Upstream.Scheme
	outReq.URL.Host = route.Upstream.Host
	outReq.Host = route.Upstream.Host
	outReq.RequestURI = ""

	rawAddr := req.RemoteAddr

	if strings.Contains(rawAddr, "://") {
		if parts := strings.SplitN(rawAddr, "://", 2); len(parts) == 2 {
			rawAddr = parts[1]
		}
	}

	clientIp := ""
	if host, _, err := net.SplitHostPort(rawAddr); err == nil {
		clientIp = host
	} else if strings.Contains(err.Error(), "missing port in address") {
		clientIp = req.RemoteAddr
	}

	if clientIp != "" {
		prior := req.Header.Get("X-Forwarded-For")
		if prior != "" {
			outReq.Header.Set("X-Forwarded-For", prior+", "+clientIp)
		} else {
			outReq.Header.Set("X-Forwarded-For", clientIp)
		}
	}

	meta := RouteMetadata{
		UpstreamName: route.Upstream.Host,
		CacheEnabled: route.CacheEnabled,
		CacheTTL:     route.CacheTTL,
	}
	return outReq, meta, nil
}
