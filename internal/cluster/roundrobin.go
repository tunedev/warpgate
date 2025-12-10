package cluster

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
	"warpgate/internal/metrics"
)

type roundRobin struct {
	mu        sync.Mutex
	name      string
	endpoints []*Endpoint
	idx       int

	healthCfg *HealthCheckConfig
	cbCfg     *CircuitBreakerConfig
}

func NewRoundRobinCluster(name string, endpoints []*Endpoint, hc *HealthCheckConfig, cb *CircuitBreakerConfig) Cluster {
	for _, ep := range endpoints {
		ep.Alive = true
	}

	return &roundRobin{
		name:      name,
		endpoints: endpoints,
		healthCfg: hc,
		cbCfg:     cb,
	}
}

func (c *roundRobin) Name() string {
	return c.name
}

func (c *roundRobin) PickEndpoint() (*Endpoint, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	n := len(c.endpoints)
	if n == 0 {
		return nil, errors.New("cluster has no endpoints")
	}

	now := time.Now()

	for i := 0; i < n; i++ {
		ep := c.endpoints[c.idx]
		c.idx = (c.idx + 1) % n

		if !ep.Alive {
			continue
		}

		if !ep.circuitOpenUntil.IsZero() && now.Before(ep.circuitOpenUntil) {
			continue
		}

		if !ep.circuitOpenUntil.IsZero() && now.After(ep.circuitOpenUntil) {
			ep.circuitOpenUntil = time.Time{}
			ep.cbFailures = 0
		}
		return ep, nil
	}

	return nil, errors.New("cluster has no alive endpoints")
}

func (c *roundRobin) ReportSuccess(ep *Endpoint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ep.cbFailures = 0
}

func (c *roundRobin) ReportFailure(ep *Endpoint) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ep.cbFailures++
	if c.cbCfg != nil && ep.cbFailures >= c.cbCfg.ConsecutiveFailures {
		ep.circuitOpenUntil = time.Now().Add(c.cbCfg.Cooldown)
	}
}

func (c *roundRobin) StartHealthChecks(ctx context.Context, client *http.Client) {
	if c.healthCfg == nil {
		return
	}

	hc := *c.healthCfg
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

	ticker := time.NewTicker(hc.Interval)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.runHealthChecks(client, hc)
			}
		}
	}()
}

func (c *roundRobin) runHealthChecks(client *http.Client, hc HealthCheckConfig) {
	c.mu.Lock()
	endpoints := append([]*Endpoint(nil), c.endpoints...)
	c.mu.Unlock()

	unhealthy := 0

	for _, ep := range endpoints {
		urlCopy := *ep.URL
		urlCopy.Path = hc.Path

		hctx, cancel := context.WithTimeout(context.Background(), hc.Timeout)
		req, err := http.NewRequestWithContext(hctx, http.MethodGet, urlCopy.String(), nil)
		if err != nil {
			cancel()
			continue
		}

		resp, err := client.Do(req)
		ok := err == nil && resp.StatusCode >= 200 && resp.StatusCode < 400
		if resp != nil {
			_ = resp.Body.Close()
		}
		cancel()

		c.mu.Lock()
		if ok {
			ep.hcFailures = 0
			ep.hcSuccesses++
			if ep.hcSuccesses >= hc.HealthyThreshold {
				ep.Alive = true
			}
		} else {
			ep.hcSuccesses = 0
			ep.hcFailures++
			if ep.hcFailures >= hc.UnhealthyThreshold {
				ep.Alive = false
			}
		}
		c.mu.Unlock()
	}

	c.mu.Lock()
	for _, ep := range c.endpoints {
		if !ep.Alive {
			unhealthy++
		}
	}
	c.mu.Unlock()

	metrics.SetClusterUnhealthy(c.name, float64(unhealthy))
}
