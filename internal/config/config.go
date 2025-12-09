package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server ServerConfig  `yaml:"server"`
	Cache  CacheConfig   `yaml:"cache"`
	Routes []RouteConfig `yaml:"routes"`
}

type ServerConfig struct {
	Address string    `yaml:"address"`
	TLS     TLSConfig `yaml:"tls"`
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

type RouteConfig struct {
	Name       string            `yaml:"name"`
	PathPrefix string            `yaml:"pathPrefix"`
	Upstream   string            `yaml:"upstream"`
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
