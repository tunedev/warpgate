package proxy

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"warpgate/internal/cache"
)

type Director interface {
	Direct(req *http.Request) (*http.Request, RouteMetadata, error)
}

type RouteMetadata struct {
	UpstreamName string
	CacheEnabled bool
	CacheTTL     int64
}

type Transport interface {
	RoundTrip(*http.Request) (*http.Response, error)
}

type Engine struct {
	Director  Director
	Cache     cache.Cache
	Transport Transport
}

func NewEngine(d Director, c cache.Cache, t Transport) *Engine {
	return &Engine{
		Director:  d,
		Cache:     c,
		Transport: t,
	}
}

func (e *Engine) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	outReq, meta, err := e.Director.Direct(req)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadGateway)
		return
	}

	_ = meta

	resp, err := e.Transport.RoundTrip(outReq)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyHeader(rw.Header(), resp.Header)

	trailerKeys := make([]string, 0, len(resp.Trailer))
	for k := range resp.Trailer {
		trailerKeys = append(trailerKeys, k)
	}
	if len(trailerKeys) > 0 {
		rw.Header().Set("Trailer", strings.Join(trailerKeys, ","))
	}

	rw.WriteHeader(resp.StatusCode)

	flusher, _ := rw.(http.Flusher)

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if flusher != nil {
					flusher.Flush()
				}
			case <-done:
				return
			}
		}
	}()

	_, copyErr := io.Copy(rw, resp.Body)

	for k, values := range resp.Trailer {
		for _, v := range values {
			rw.Header().Set(k, v)
		}
	}
	close(done)

	if copyErr != nil {
		fmt.Println("copy error", copyErr)
	}
}

func copyHeader(dst, src http.Header) {
	for k, values := range src {
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}
