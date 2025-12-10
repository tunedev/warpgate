package cluster

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

type HealthCheckConfig struct {
	Path               string
	Interval           time.Duration
	Timeout            time.Duration
	UnhealthyThreshold int
	HealthyThreshold   int
}

type CircuitBreakerConfig struct {
	ConsecutiveFailures int
	Cooldown            time.Duration
}

type Endpoint struct {
	URL   *url.URL
	Alive bool

	hcSuccesses int
	hcFailures  int

	cbFailures       int
	circuitOpenUntil time.Time
}

type LoadBalancer interface {
	Pick() (*Endpoint, error)
}

type Cluster interface {
	Name() string
	PickEndpoint() (*Endpoint, error)
	ReportSuccess(ep *Endpoint)
	ReportFailure(ep *Endpoint)
	StartHealthChecks(ctx context.Context, client *http.Client)
}
