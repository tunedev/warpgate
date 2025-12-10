package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"warpgate/internal/cache"
	"warpgate/internal/cluster"
	"warpgate/internal/config"
	"warpgate/internal/logging"
	"warpgate/internal/metrics"
	"warpgate/internal/proxy"
	"warpgate/internal/upstream"
)

func main() {
	configPath := flag.String("config", "./configs/warpgate.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()

	clusters := make(map[string]cluster.Cluster)

	for _, c := range cfg.Clusters {
		var endpoints []*cluster.Endpoint
		for _, raw := range c.Endpoints {
			u, err := url.Parse(raw)
			if err != nil {
				log.Fatalf("parse endpoint %q for cluster %s: %v", raw, c.Name, err)
			}
			endpoints = append(endpoints, &cluster.Endpoint{
				URL: u,
			})
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

		clusters[c.Name] = cluster.NewRoundRobinCluster(c.Name, endpoints, hc, cb)
	}

	var routes []proxy.SimpleRoute
	for _, r := range cfg.Routes {
		routes = append(routes, proxy.SimpleRoute{
			Prefix:       r.PathPrefix,
			ClusterName:  r.Cluster,
			CacheEnabled: cfg.RouteCacheEnabled(r),
			CacheTTL:     cfg.RouteTTL(r),
		})
	}

	healthClient := &http.Client{}
	for _, cl := range clusters {
		cl.StartHealthChecks(bgCtx, healthClient)
	}

	metrics.Init()

	logger := logging.New()
	director := proxy.NewSimpleDirector(routes)
	transport := upstream.NewTransport()

	memCache := cache.NewInMemoryCache(cfg.Cache.MaxEntries)
	engine := proxy.NewEngine(director, memCache, transport, clusters, logger)
	engine.MaxCacheBodySize = cfg.Cache.MaxBodyBytes

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.Handle("/", engine)

	srv := &http.Server{
		Addr:    cfg.Server.Address,
		Handler: mux,
	}

	go func() {
		log.Printf("Listening on %s", srv.Addr)
		if cfg.Server.TLS.Enabled {
			if err := srv.ListenAndServeTLS(cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile); err != nil && err != http.ErrServerClosed {
				log.Fatalf("server TLS error: %v", err)
			}
		} else {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("server error: %v", err)
			}
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	<-stop
	log.Println("Shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
}
