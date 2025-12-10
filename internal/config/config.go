package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type ListenerConfig struct {
	Name       string    `yaml:"name"`
	Address    string    `yaml:"address"`
	TLS        TLSConfig `yaml:"tls"`
	RedirectTo string    `yaml:"redirectTo,omitempty"`
}

type Config struct {
	Server    ServerConfig     `yaml:"server"`
	Cache     CacheConfig      `yaml:"cache"`
	Clusters  []ClusterConfig  `yaml:"clusters"`
	Routes    []RouteConfig    `yaml:"routes"`
	Listeners []ListenerConfig `yaml:"listeners,omitempty"`
}

type ServerConfig struct {
	Address      string    `yaml:"address"`
	TLS          TLSConfig `yaml:"tls"`
	IPBlockCIDRS []string  `yaml:"ipBlockCIDRS,omitempty"`
}

type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"certFile"`
	KeyFile  string `yaml:"keyFile"`
}

type CacheConfig struct {
	MaxEntries   int           `yaml:"maxEntries"`
	DefaultTTL   time.Duration `yaml:"defaultTTL"`
	MaxBodyBytes int64         `yaml:"maxBodyBytes"`
}

type ClusterConfig struct {
	Name           string                `yaml:"name"`
	Endpoints      []string              `yaml:"endpoints"`
	HealthCheck    *HealthCheckConfig    `yaml:"healthCheck,omitempty"`
	CircuitBreaker *CircuitBreakerConfig `yaml:"circuitBreaker,omitempty"`
}

type HealthCheckConfig struct {
	Path               string        `yaml:"path"`
	Interval           time.Duration `yaml:"interval"`
	Timeout            time.Duration `yaml:"timeout"`
	UnhealthyThreshold int           `yaml:"unhealthyThreshold"`
	HealthyThreshold   int           `yaml:"healthyThreshold"`
}

type CircuitBreakerConfig struct {
	ConsecutiveFailures int           `yaml:"consecutiveFailures"`
	Cooldown            time.Duration `yaml:"cooldown"`
}

type RouteConfig struct {
	Name       string            `yaml:"name"`
	PathPrefix string            `yaml:"pathPrefix"`
	Cluster    string            `yaml:"cluster"`
	Cache      *RouteCacheConfig `yaml:"cache,omitempty"`
}

type RouteCacheConfig struct {
	Enabled *bool          `yaml:"enabled,omitempty"`
	TTL     *time.Duration `yaml:"ttl,omitempty"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal yaml: %w", err)
	}

	if cfg.Server.Address == "" {
		cfg.Server.Address = ":8080"
	}

	if cfg.Cache.MaxEntries <= 0 {
		cfg.Cache.MaxEntries = 1000
	}

	if cfg.Cache.MaxBodyBytes <= 0 {
		cfg.Cache.MaxBodyBytes = 1 << 20 // 1 MiB
	}

	for i := range cfg.Clusters {
		hc := cfg.Clusters[i].HealthCheck
		if hc != nil {
			if hc.Interval <= 0 {
				hc.Interval = 10 * time.Second
			}
			if hc.Timeout <= 0 {
				hc.Timeout = 1 * time.Second
			}
			if hc.UnhealthyThreshold <= 0 {
				hc.UnhealthyThreshold = 3
			}
			if hc.HealthyThreshold <= 0 {
				hc.HealthyThreshold = 1
			}
		}

		cb := cfg.Clusters[i].CircuitBreaker
		if cb != nil {
			if cb.ConsecutiveFailures <= 0 {
				cb.ConsecutiveFailures = 5
			}
			if cb.Cooldown <= 0 {
				cb.Cooldown = 30 * time.Second
			}
		}
	}

	return &cfg, nil
}

func (cfg *Config) RouteCacheEnabled(rc RouteConfig) bool {
	if rc.Cache != nil && rc.Cache.Enabled != nil {
		return *rc.Cache.Enabled
	}
	return true
}

func (cfg *Config) RouteTTL(rc RouteConfig) time.Duration {
	if rc.Cache != nil && rc.Cache.TTL != nil {
		return *rc.Cache.TTL
	}
	return cfg.Cache.DefaultTTL
}
