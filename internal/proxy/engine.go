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
	"warpgate/internal/cluster"
	"warpgate/internal/logging"
	"warpgate/internal/metrics"
)

type Director interface {
	Direct(req *http.Request) (*http.Request, RouteMetadata, error)
}

type RouteMetadata struct {
	RouteName    string
	ClusterName  string
	CacheEnabled bool
	CacheTTL     time.Duration
}

type Transport interface {
	RoundTrip(*http.Request) (*http.Response, error)
}

type Engine struct {
	Director         Director
	Cache            cache.Cache
	Transport        Transport
	MaxCacheBodySize int64
	Logger           logging.Logger
	Clusters         map[string]cluster.Cluster
}

func NewEngine(d Director, c cache.Cache, t Transport, clusters map[string]cluster.Cluster, l logging.Logger) *Engine {
	return &Engine{
		Director:         d,
		Cache:            c,
		Transport:        t,
		MaxCacheBodySize: 1 << 20,
		Logger:           l,
		Clusters:         clusters,
	}
}

func (e *Engine) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	start := time.Now()

	outReq, meta, err := e.Director.Direct(req)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadGateway)
		if e.Logger != nil {
			e.Logger.Error("director error",
				"method", req.Method,
				"path", req.URL.Path,
				"err", err,
			)
		}
		metrics.ObserveRequest(meta.ClusterName, req.Method, fmt.Sprint(http.StatusBadGateway), time.Since(start))
		return
	}

	cl, ok := e.Clusters[meta.ClusterName]
	if !ok {
		http.Error(rw, fmt.Sprintf("no such cluster: %s", meta.ClusterName), http.StatusBadGateway)
		metrics.ObserveRequest(meta.ClusterName, req.Method, fmt.Sprint(http.StatusBadGateway), time.Since(start))
		return
	}

	endpoint, err := cl.PickEndpoint()
	if err != nil {
		http.Error(rw, fmt.Sprintf("no available endpoint in cluster: %s", meta.ClusterName), http.StatusBadGateway)
		metrics.ObserveRequest(meta.ClusterName, req.Method, fmt.Sprint(http.StatusBadGateway), time.Since(start))
		return
	}

	targetUrl := endpoint.URL
	outReq.URL.Scheme = targetUrl.Scheme
	outReq.URL.Host = targetUrl.Host
	outReq.Host = targetUrl.Host
	outReq.RequestURI = ""

	cacheableMethod := outReq.Method == http.MethodGet || outReq.Method == http.MethodHead
	routeLabel := meta.ClusterName

	if meta.CacheEnabled && cacheableMethod {
		if ok := e.serveFromCache(ctx, rw, outReq, routeLabel, start); ok {
			return
		}
	}

	resp, err := e.Transport.RoundTrip(outReq)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadGateway)
		if e.Logger != nil {
			e.Logger.Error("upstream error",
				"method", outReq.Method,
				"url", outReq.URL.String(),
				"err", err,
			)
		}
		metrics.ObserveRequest(routeLabel, req.Method, fmt.Sprint(http.StatusBadGateway), time.Since(start))
		return
	}
	defer resp.Body.Close()

	statusCode := resp.StatusCode

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

	duration := time.Since(start)
	metrics.ObserveRequest(routeLabel, req.Method, fmt.Sprint(statusCode), duration)
	if e.Logger != nil {
		e.Logger.Info("proxy request",
			"method", req.Method,
			"path", req.URL.Path,
			"status", statusCode,
			"upstream", routeLabel,
			"cacheEnabled", meta.CacheEnabled,
			"duration_ms", duration.Milliseconds(),
		)
	}

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
}

func (e *Engine) serveFromCache(ctx context.Context, rw http.ResponseWriter, req *http.Request, routeLabel string, start time.Time) bool {
	if e.Cache == nil {
		return false
	}

	key := cacheKeyFromRequest(req)
	cached, ok := e.Cache.Get(ctx, key)
	if !ok {
		metrics.IncCacheMiss(routeLabel)
		return false
	}

	copyHeader(rw.Header(), cached.Header)
	rw.WriteHeader(cached.StatusCode)
	_, _ = rw.Write(cached.Body)

	duration := time.Since(start)
	metrics.ObserveRequest(routeLabel, req.Method, fmt.Sprint(cached.StatusCode), duration)
	metrics.IncCacheHit(routeLabel)

	if e.Logger != nil {
		e.Logger.Info("cache hit",
			"method", req.Method,
			"path", req.URL.Path,
			"status", cached.StatusCode,
			"upstream", routeLabel,
			"duration_ms", duration.Milliseconds(),
		)
	}
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

func computeExpiry(resp *http.Response, routeTTL time.Duration) time.Time {
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

	if routeTTL > 0 {
		return now.Add(routeTTL)
	}

	return time.Time{}
}
