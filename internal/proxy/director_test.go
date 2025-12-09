package proxy_test

import (
	"net/http"
	"testing"
	"time"
	"warpgate/internal/proxy"
)

func TestSimpleDirector_PrefixMatch(t *testing.T) {
	d := proxy.NewSimpleDirector([]proxy.SimpleRoute{
		{
			Prefix:       "/api",
			ClusterName:  "demo_cluster",
			CacheEnabled: true,
			CacheTTL:     10 * time.Second,
		},
	})

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/api/users", nil)
	req.RemoteAddr = "10.0.0.1:1234"

	outReq, meta, err := d.Direct(req)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	if meta.ClusterName != "demo_cluster" {
		t.Errorf("unexpected ClusterName=demo_cluster: %s", meta.ClusterName)
	}
	if !meta.CacheEnabled {
		t.Errorf("expeted CacheEnabled to be true")
	}
	if meta.CacheTTL != 10*time.Second {
		t.Errorf("unexpected CacheTTl: %v", meta.CacheTTL)
	}
	if meta.RouteName != "/api" {
		t.Errorf("expected RouteName=/api, got %q", meta.RouteName)
	}

	if got := outReq.Header.Get("X-Forwarded-For"); got != "10.0.0.1" {
		t.Errorf("expected X-Forwarded-For=10.0.0.1, got %q", got)
	}
}

func TestSimpleDirector_NoRoute(t *testing.T) {
	d := proxy.NewSimpleDirector([]proxy.SimpleRoute{
		{Prefix: "/api", ClusterName: "api_cluster"},
	})

	req, _ := http.NewRequest(http.MethodGet, "https://example.com/other", nil)
	_, _, err := d.Direct(req)
	if err == nil {
		t.Fatal("expected error for unmatched route, got nil")
	}
}

func TestSimpleDirector_RoutingPriority(t *testing.T) {
	d := proxy.NewSimpleDirector([]proxy.SimpleRoute{
		{Prefix: "/api/users", ClusterName: "users_cluster", CacheEnabled: true},
		{Prefix: "/api", ClusterName: "api_cluster", CacheEnabled: false},
	})

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/api/users/123", nil)
	_, meta, err := d.Direct(req)
	if err != nil {
		t.Fatalf("enexpected error: %v", err)
	}

	if meta.RouteName != "/api/users" {
		t.Errorf("expected RouteName=/api/users, got %q", meta.RouteName)
	}
	if meta.ClusterName != "users_cluster" {
		t.Errorf("expected ClusterName=users_cluster, got %q", meta.ClusterName)
	}

	if !meta.CacheEnabled {
		t.Errorf("expected CachedEnabled to be true from specific route")
	}
}

func TestSimpleDirector_XForwarwardedFor_Appending(t *testing.T) {
	d := proxy.NewSimpleDirector([]proxy.SimpleRoute{
		{Prefix: "/", ClusterName: "default"},
	})

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.Header.Set("X-Forwarded-For", "192.168.1.1, 10.0.0.5")
	req.RemoteAddr = "172.16.0.10:54321"

	outReq, _, _ := d.Direct(req)

	expected := "192.168.1.1, 10.0.0.5, 172.16.0.10"
	if got := outReq.Header.Get("X-forwarded-For"); got != expected {
		t.Errorf("X-Forwarded-For Appending failed.\nExpected: %q\nGot: \t%q", expected, got)
	}
	req2, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	req2.RemoteAddr = "10.0.0.25"
	outReq2, _, _ := d.Direct(req2)

	expected2 := "10.0.0.25"
	if got := outReq2.Header.Get("X-Forwarded-For"); got != expected2 {
		t.Errorf("X-Forwarded-For for bare IP failed. Expected: %q, Got: %q", expected2, got)
	}

	req3, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	req3.RemoteAddr = "tcp://10.0.0.50:8080"
	outReq3, _, err := d.Direct(req3)

	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	expected3 := "10.0.0.50"
	if got := outReq3.Header.Get("X-Forwarded-For"); got != expected3 {
		t.Errorf("X-Forwarded-For scheme sanitization failed.\nExpected: %q\nGot:\t%q", expected3, got)
	}
}
