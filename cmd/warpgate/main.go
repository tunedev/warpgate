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
	"warpgate/internal/config"
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

	var routes []proxy.SimpleRoute

	for _, r := range cfg.Routes {
		u, err := url.Parse(r.Upstream)
		if err != nil {
			log.Fatalf("parse upstream for route %s: %v", r.Name, err)
		}
		routes = append(routes, proxy.SimpleRoute{
			Prefix:       r.PathPrefix,
			Upstream:     u,
			CacheEnabled: cfg.RouteCacheEnabled(r),
			CacheTTL:     cfg.RouteTTL(r),
		})
	}

	director := proxy.NewSimpleDirector(routes)
	transport := upstream.NewTransport()

	memCache := cache.NewInMemoryCache(cfg.Cache.MaxEntries)
	engine := proxy.NewEngine(director, memCache, transport)
	engine.MaxCacheBodySize = cfg.Cache.MaxBodyBytes

	srv := &http.Server{
		Addr:    cfg.Server.Address,
		Handler: engine,
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
