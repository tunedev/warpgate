package proxy_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"
	"warpgate/internal/proxy"
)

func TestSimpleDirector_PrefixMatch(t *testing.T) {
	u, _ := url.Parse("https://backend.local")
	d := proxy.NewSimpleDirector([]proxy.SimpleRoute{
		{
			Prefix:       "/api",
			Upstream:     u,
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

	if outReq.URL.Scheme != "https" || outReq.URL.Host != "backend.local" {
		t.Errorf("expected Host to be upstream host, fot %s", outReq.Host)
	}

	if outReq.RequestURI != "" {
		t.Errorf("expected RequestURI to be empty, got %q", outReq.RequestURI)
	}

	if meta.UpstreamName != "backend.local" {
		t.Errorf("unexpected UpstreamName: %s", meta.UpstreamName)
	}
	if !meta.CacheEnabled {
		t.Errorf("expeted CacheEnabled to be true")
	}
	if meta.CacheTTL != 10*time.Second {
		t.Errorf("unexpected CacheTTl: %v", meta.CacheTTL)
	}
	if got := outReq.Header.Get("X-Forwarded-For"); got != "10.0.0.1" {
		t.Errorf("expected X-Forwarded-For=10.0.0.1, got %q", got)
	}
}

func TestSimpleDirector_NoRoute(t *testing.T) {
	u, _ := url.Parse("https://backend.local")
	d := proxy.NewSimpleDirector([]proxy.SimpleRoute{
		{Prefix: "/api", Upstream: u},
	})

	req, _ := http.NewRequest(http.MethodGet, "https://example.com/other", nil)
	_, _, err := d.Direct(req)
	if err == nil {
		t.Fatal("expected error for unmatched route, got nil")
	}
}

func TestSimpleDirector_RoutingPriority(t *testing.T) {
	u1, _ := url.Parse("https://backend-specific.local")
	u2, _ := url.Parse("https://backend-general.local")

	d := proxy.NewSimpleDirector([]proxy.SimpleRoute{
		{Prefix: "/api/users", Upstream: u1, CacheEnabled: true},
		{Prefix: "/api", Upstream: u2, CacheEnabled: false},
	})

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/api/users/123", nil)
	outReq, meta, err := d.Direct(req)

	if err != nil {
		t.Fatalf("enexpected error: %v", err)
	}

	if outReq.URL.Host != "backend-specific.local" {
		t.Errorf("expected Upstream Host 'backend-specific.local', got %s", outReq.URL.Host)
	}
	if !meta.CacheEnabled {
		t.Errorf("expected CachedEnabled to be true from specific route")
	}
}

func TestSimpleDirector_RewriteAndPathPreservation(t *testing.T) {
	upstreamURL, _ := url.Parse("https://new-host:8080")
	d := proxy.NewSimpleDirector([]proxy.SimpleRoute{
		{Prefix: "/admin", Upstream: upstreamURL},
	})

	req, _ := http.NewRequest(http.MethodGet, "http://old-host.com:80/admin/dashboard?q=test", nil)
	req.RequestURI = "/admin/dashboard?q=test"

	outReq, _, err := d.Direct(req)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	if outReq.URL.Scheme != "https" || outReq.URL.Host != "new-host:8080" {
		t.Errorf("URL tranformation incorrect. Got %s://%s, expectedhttps://new-host:8080", outReq.URL.Scheme, outReq.URL.Host)
	}

	if outReq.Host != "new-host:8080" {
		t.Errorf("Host header incorrect. Got %s, expected new-host:8080", outReq.Host)
	}

	if outReq.URL.Path != "/admin/dashboard" {
		t.Errorf("Path was not preserved. Got %s, expected /admin/dashboard", outReq.URL.Path)
	}

	if outReq.RequestURI != "" {
		t.Errorf("RequestURI was not cleard. Got %q, expected\"\"", outReq.RequestURI)
	}
}

func TestSimpleDirector_XForwarwardedFor_Appending(t *testing.T) {
	u, _ := url.Parse("https://backend.local")
	d := proxy.NewSimpleDirector([]proxy.SimpleRoute{
		{Prefix: "/", Upstream: u},
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
