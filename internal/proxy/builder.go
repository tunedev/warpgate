package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"warpgate/internal/cache"
	"warpgate/internal/cluster"
	"warpgate/internal/config"
	"warpgate/internal/logging"
	"warpgate/internal/metrics"
	"warpgate/internal/middleware"
	"warpgate/internal/upstream"
)

type ListenerServer struct {
	Name   string
	Server *http.Server
	TLS    config.TLSConfig
}

type Builder struct {
	cfg    *config.Config
	logger logging.Logger
}

func NewBuilder(cfg *config.Config, logger logging.Logger) *Builder {
	return &Builder{
		cfg:    cfg,
		logger: logger,
	}
}

func (b *Builder) Build(ctx context.Context) ([]*ListenerServer, error) {
	clusters, err := b.buildClusters(ctx)
	if err != nil {
		return nil, err
	}

	routes := b.buildRoutes()
	director := NewSimpleDirector(routes)

	transport := upstream.NewTransport()
	memcache := cache.NewInMemoryCache(b.cfg.Cache.MaxEntries)

	engine := NewEngine(director, memcache, transport, clusters, b.logger)
	engine.MaxCacheBodySize = b.cfg.Cache.MaxBodyBytes

	var mws []middleware.Middleware

	if len(b.cfg.Server.IPBlockCIDRS) > 0 {
		ipMw, err := middleware.IPFilter(b.logger, b.cfg.Server.IPBlockCIDRS)
		if err != nil {
			return nil, fmt.Errorf("invalid ipBlockCIDRs: %w", err)
		}
		mws = append(mws, ipMw)
	}

	var appHandler http.Handler = engine
	appHandler = middleware.Chain(appHandler, mws...)

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.Handle("/", appHandler)

	if len(b.cfg.Listeners) == 0 {
		return []*ListenerServer{
			{
				Name: "default",
				Server: &http.Server{
					Addr:    b.cfg.Server.Address,
					Handler: mux,
				},
				TLS: b.cfg.Server.TLS,
			},
		}, nil
	}

	return b.buildListeners(mux)
}

func (b *Builder) buildClusters(ctx context.Context) (map[string]cluster.Cluster, error) {
	clusters := make(map[string]cluster.Cluster)

	for _, c := range b.cfg.Clusters {
		var endpoints []*cluster.Endpoint
		for _, raw := range c.Endpoints {
			u, err := url.Parse(raw)
			if err != nil {
				return nil, fmt.Errorf("parse endpoint %q for cluster %s: %w", raw, c.Name, err)
			}
			endpoints = append(endpoints, &cluster.Endpoint{URL: u})
		}

		var hc *cluster.HealthCheckConfig
		if c.HealthCheck != nil {
			hc = &cluster.HealthCheckConfig{
				Path:               c.HealthCheck.Path,
				Interval:           c.HealthCheck.Interval,
				Timeout:            c.HealthCheck.Timeout,
				UnhealthyThreshold: c.HealthCheck.UnhealthyThreshold,
				HealthyThreshold:   c.HealthCheck.HealthyThreshold,
			}
		}

		var cb *cluster.CircuitBreakerConfig
		if c.CircuitBreaker != nil {
			cb = &cluster.CircuitBreakerConfig{
				ConsecutiveFailures: c.CircuitBreaker.ConsecutiveFailures,
				Cooldown:            c.CircuitBreaker.Cooldown,
			}
		}

		cl := cluster.NewRoundRobinCluster(c.Name, endpoints, hc, cb)
		clusters[c.Name] = cl

		if hc != nil {
			client := &http.Client{}
			cl.StartHealthChecks(ctx, client)
		}
	}
	return clusters, nil
}

func (b *Builder) buildRoutes() []SimpleRoute {
	var routes []SimpleRoute
	for _, r := range b.cfg.Routes {
		routes = append(routes, SimpleRoute{
			Prefix:       r.PathPrefix,
			ClusterName:  r.Cluster,
			CacheEnabled: b.cfg.RouteCacheEnabled(r),
			CacheTTL:     b.cfg.RouteTTL(r),
		})
	}
	return routes
}

func (b *Builder) buildListeners(mux http.Handler) ([]*ListenerServer, error) {
	ListenerByName := make(map[string]config.ListenerConfig, len(b.cfg.Listeners))
	for _, l := range b.cfg.Listeners {
		ListenerByName[l.Name] = l
	}

	var listeners []*ListenerServer

	for _, lst := range b.cfg.Listeners {
		var handler http.Handler

		if lst.RedirectTo != "" && !lst.TLS.Enabled {
			target, ok := ListenerByName[lst.RedirectTo]
			if !ok {
				return nil, fmt.Errorf("listener %q has redirectTo=%q but target not found", lst.Name, lst.RedirectTo)
			}
			handler = httpsRedirecthandler(target.Address)
		} else {
			handler = mux
		}

		srv := &http.Server{
			Addr:    lst.Address,
			Handler: handler,
		}

		listeners = append(listeners, &ListenerServer{
			Name:   lst.Name,
			Server: srv,
			TLS:    lst.TLS,
		})
	}

	return listeners, nil
}

func httpsRedirecthandler(targetAddr string) http.Handler {
	port := extractPort(targetAddr)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetURL := *r.URL
		targetURL.Scheme = "https"

		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}

		if port == "" || port == "443" {
			targetURL.Host = host
		} else {
			targetURL.Host = fmt.Sprintf("%s:%s", host, port)
		}
		http.Redirect(w, r, targetURL.String(), http.StatusMovedPermanently)
	})
}

func extractPort(addr string) string {
	idx := strings.LastIndex(addr, ";")
	if idx == -1 {
		return ""
	}
	return addr[idx+1:]
}
