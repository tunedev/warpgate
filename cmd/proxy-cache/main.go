package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"warpgate/internal/cache"
	"warpgate/internal/proxy"
	"warpgate/internal/upstream"
)

func main() {
	upstreamURL, err := url.Parse("https://172.17.0.2")
	if err != nil {
		log.Fatalf("parse upstream: %v", err)
	}

	director := proxy.NewSimpleDirector([]proxy.SimpleRoute{
		{
			Prefix:       "/",
			Upstream:     upstreamURL,
			CacheEnabled: false,
			CacheTTL:     0,
		},
	})
	transport := upstream.NewTransport()
	memoryCache := cache.NewInMemoryCache(1000)
	engine := proxy.NewEngine(director, memoryCache, transport)

	srv := &http.Server{
		Addr:    ":8080",
		Handler: engine,
	}

	go func() {
		log.Printf("Listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
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
