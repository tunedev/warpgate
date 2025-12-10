package cluster

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"
	"warpgate/internal/metrics"
)

func TestMain(m *testing.M) {
	metrics.Init()
	os.Exit(m.Run())
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return u
}

func TestRobinRobin_PickEndpoint_BasicAndAlive(t *testing.T) {
	ep1 := &Endpoint{URL: mustParseURL(t, "http://backend1")}
	ep2 := &Endpoint{URL: mustParseURL(t, "http://backend2")}

	cl := NewRoundRobinCluster("test", []*Endpoint{ep1, ep2}, nil, nil).(*roundRobin)

	if !ep1.Alive || !ep2.Alive {
		t.Fatalf("expected endpoints to be marked alive at startup")
	}

	got1, err := cl.PickEndpoint()
	if err != nil {
		t.Fatalf("PickEndpoint error: %v", err)
	}

	got2, err := cl.PickEndpoint()
	if err != nil {
		t.Fatalf("PickEndpoint error: %v", err)
	}

	got3, err := cl.PickEndpoint()
	if err != nil {
		t.Fatalf("PickEndpoint error: %v", err)
	}

	got4, err := cl.PickEndpoint()
	if err != nil {
		t.Fatalf("PickEndpoint error: %v", err)
	}

	if got1 != ep1 || got2 != ep2 || got3 != ep1 || got4 != ep2 {
		t.Errorf("round-robin sequence incorrect: got [%p %p %p %p], want [ep1 ep2 ep1 ep2]", got1, got2, got3, got4)
	}

	ep2.Alive = false

	for i := 0; i < 4; i++ {
		got, err := cl.PickEndpoint()
		if err != nil {
			t.Fatalf("PickEndpoint error after ep2 down: %v", err)
		}
		if got != ep1 {
			t.Errorf("expected only ep1 when ep2 is not alive, got %p", got)
		}
	}
}

func TestRoundRobin_CircuitBreaker_OpenAndCloses(t *testing.T) {
	cbCfg := &CircuitBreakerConfig{
		ConsecutiveFailures: 2,
		Cooldown:            20 * time.Millisecond,
	}

	ep := &Endpoint{URL: mustParseURL(t, "http://backend")}
	cl := NewRoundRobinCluster("cb", []*Endpoint{ep}, nil, cbCfg).(*roundRobin)

	got, err := cl.PickEndpoint()
	if err != nil {
		t.Errorf("PickEndpoint error: %v", err)
	}
	if got != ep {
		t.Fatalf("expected to pick ep, got %p", got)
	}

	cl.ReportFailure(ep)
	if ep.cbFailures != 1 {
		t.Errorf("expected cbFailures=1 after first failure, got %d", ep.cbFailures)
	}
	if !ep.circuitOpenUntil.IsZero() {
		t.Errorf("expected circuit to remain closed after first failure")
	}

	cl.ReportFailure(ep)
	if ep.cbFailures != 2 {
		t.Errorf("expected cbFailures=2 after first failure, got %d", ep.cbFailures)
	}
	if ep.circuitOpenUntil.IsZero() {
		t.Errorf("expected circuitOpenUntil to be set after first reaching failure treshold")
	}

	if _, err := cl.PickEndpoint(); err == nil {
		t.Fatalf("expected PickEndpoint to fail while circuit is open")
	}

	time.Sleep(cbCfg.Cooldown + 5*time.Millisecond)

	got2, err := cl.PickEndpoint()
	if err != nil {
		t.Fatalf("PickEndpoint error after cooldown: %v", err)
	}
	if got2 != ep {
		t.Fatalf("expected to pick ep after cooldown, got %p", got2)
	}
	if !ep.circuitOpenUntil.IsZero() {
		t.Errorf("expected circuitOpenUntil to be reset after cooldown")
	}
	if ep.cbFailures != 0 {
		t.Errorf("expected cbFailures to be reset after cooldown, got %d", ep.cbFailures)
	}
}

func TestRoundRobin_HealthChecks_MarkUnhealthyAndRecover(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("expected health check path /health, got %q", r.URL.Path)
		}
		if healthy.Load() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	ep := &Endpoint{URL: mustParseURL(t, srv.URL)}
	hcCfg := &HealthCheckConfig{
		Path:               "/health",
		Interval:           50 * time.Millisecond,
		Timeout:            200 * time.Millisecond,
		UnhealthyThreshold: 2,
		HealthyThreshold:   1,
	}

	cl := NewRoundRobinCluster("hc", []*Endpoint{ep}, hcCfg, nil).(*roundRobin)
	client := &http.Client{}

	cl.runHealthChecks(client, *hcCfg)
	if !ep.Alive {
		t.Fatalf("expected endpoint to remain alive after first failed health check")
	}
	if ep.hcFailures != 1 {
		t.Errorf("expected hcFailures=1 after first failure, got %d", ep.hcFailures)
	}

	cl.runHealthChecks(client, *hcCfg)
	if ep.Alive {
		t.Fatalf("expected endpoint to be marked unhealthy after reaching UnhealthyTreshold")
	}
	if ep.hcFailures < hcCfg.UnhealthyThreshold {
		t.Errorf("expected hcFailures >= %d after failures, got %d", hcCfg.UnhealthyThreshold, ep.hcFailures)
	}

	healthy.Store(true)
	cl.runHealthChecks(client, *hcCfg)
	if !ep.Alive {
		t.Fatalf("expected endpoint to recover and be marked healthy after successful checks")
	}
	if ep.hcSuccesses < hcCfg.HealthyThreshold {
		t.Errorf("expected hcSuccesses >= %d after success, got %d", hcCfg.HealthyThreshold, ep.hcSuccesses)
	}
}
