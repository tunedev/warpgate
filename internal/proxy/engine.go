package proxy

import (
	"bytes"
	"context"
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
	Director         Director
	Cache            cache.Cache
	Transport        Transport
	MaxCacheBodySize int64
}

func NewEngine(d Director, c cache.Cache, t Transport) *Engine {
	return &Engine{
		Director:         d,
		Cache:            c,
		Transport:        t,
		MaxCacheBodySize: 1 << 20,
	}
}

func (e *Engine) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	outReq, meta, err := e.Director.Direct(req)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadGateway)
		return
	}

	cacheableMethod := outReq.Method == http.MethodGet || outReq.Method == http.MethodHead

	if meta.CacheEnabled && cacheableMethod {
		if ok := e.serveFromCache(ctx, rw, outReq); ok {
			return
		}
	}

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

	var buf *bytes.Buffer
	var key string
	shouldCache := meta.CacheEnabled && cacheableMethod && isCacheableResponse(resp)
	if shouldCache {
		expiry := computeExpiry(resp, meta.CacheTTL)
		if expiry.IsZero() {
			shouldCache = false
		} else {
			buf = &bytes.Buffer{}
			key = cacheKeyFromRequest(outReq)
		}
	}

	var reader io.Reader = resp.Body
	if shouldCache {
		reader = io.TeeReader(resp.Body, buf)
	}

	_, copyErr := io.Copy(rw, reader)

	for k, values := range resp.Trailer {
		for _, v := range values {
			rw.Header().Set(k, v)
		}
	}
	close(done)

	if shouldCache && copyErr == nil && e.Cache != nil {
		if int64(buf.Len()) <= e.MaxCacheBodySize {
			expiry := computeExpiry(resp, meta.CacheTTL)
			if !expiry.IsZero() {
				e.Cache.Set(ctx, key, &cache.CachedResponse{
					StatusCode: resp.StatusCode,
					Header:     cloneHeader(resp.Header),
					Body:       buf.Bytes(),
					ExpiresAt:  expiry,
				})
			}
		}
	}

	if copyErr != nil {
		fmt.Println("copy error", copyErr)
	}
}

func (e *Engine) serveFromCache(ctx context.Context, rw http.ResponseWriter, req *http.Request) bool {
	if e.Cache == nil {
		return false
	}

	key := cacheKeyFromRequest(req)
	cached, ok := e.Cache.Get(ctx, key)
	if !ok {
		return false
	}

	copyHeader(rw.Header(), cached.Header)
	rw.WriteHeader(cached.StatusCode)

	_, _ = rw.Write(cached.Body)
	return true
}

func copyHeader(dst, src http.Header) {
	for k, values := range src {
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}

func cloneHeader(src http.Header) http.Header {
	dst := make(http.Header, len(src))
	for k, values := range src {
		for _, v := range values {
			dst.Add(k, v)
		}
	}
	return dst
}

func cacheKeyFromRequest(req *http.Request) string {
	u := *req.URL
	return req.Method + " " + u.Scheme + "://" + req.Host + u.RequestURI()
}

func isCacheableResponse(resp *http.Response) bool {
	if resp.StatusCode != http.StatusOK {
		return false
	}

	cc := resp.Header.Get("Cache-Control")
	ccLower := strings.ToLower(cc)
	if strings.Contains(ccLower, "no-store") {
		return false
	}
	if strings.Contains(ccLower, "private") {
		return false
	}
	return true
}

func computeExpiry(resp *http.Response, routeTTLSeconds int64) time.Time {
	now := time.Now()

	cc := resp.Header.Get("Cache-Control")
	for _, part := range strings.Split(cc, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "max-age=") {
			val := strings.TrimPrefix(part, "max-age=")
			if secs, err := time.ParseDuration(val + "s"); err == nil {
				return now.Add(secs)
			}
		}
	}

	if routeTTLSeconds > 0 {
		return now.Add(time.Duration(routeTTLSeconds) * time.Second)
	}

	return time.Time{}
}
