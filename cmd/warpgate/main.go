package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"warpgate/internal/config"
	"warpgate/internal/logging"
	"warpgate/internal/metrics"
	"warpgate/internal/proxy"
)

func main() {
	configPath := flag.String("config", "./configs/warpgate.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	metrics.Init()
	logger := logging.New()

	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()

	builder := proxy.NewBuilder(cfg, logger)
	listeners, err := builder.Build(bgCtx)
	if err != nil {
		log.Fatalf("build proxy: %v", err)
	}

	var wg sync.WaitGroup

	for _, ls := range listeners {
		wg.Add(1)

		go func(ls *proxy.ListenerServer) {
			defer wg.Done()

			scheme := "http"
			if ls.TLS.Enabled {
				scheme = "https"
			}

			log.Printf("Listener %q starting on %s (%s)", ls.Name, ls.Server.Addr, scheme)

			var err error
			if ls.TLS.Enabled {
				err = ls.Server.ListenAndServeTLS(ls.TLS.CertFile, ls.TLS.KeyFile)
			} else {
				err = ls.Server.ListenAndServe()
			}

			if err != nil && err != http.ErrServerClosed {
				log.Printf("Listener %q error: %v", ls.Name, err)
			}
		}(ls)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	<-stop
	log.Println("Shutting down listeners gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, ls := range listeners {
		go func(s *http.Server) {
			if err := s.Shutdown(ctx); err != nil {
				log.Printf("server shutdown error: %v", err)
			}
		}(ls.Server)
	}

	wg.Wait()
	log.Println("All listeners stopped")
}
