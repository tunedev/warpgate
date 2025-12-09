package cache

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

func makeResponse(status int, body string, ttl time.Duration) *CachedResponse {
	var expires time.Time
	if ttl > 0 {
		expires = time.Now().Add(ttl)
	}
	return &CachedResponse{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       []byte(body),
		ExpiresAt:  expires,
	}
}

func TestNewInMemoryCache(t *testing.T) {
	tests := []struct {
		name       string
		maxEntries int
		wantSize   int
	}{
		{"ValidSize", 10, 10},
		{"ZeroSize", 0, 1024},
		{"NegativeSize", -5, 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewInMemoryCache(tt.maxEntries)
			if c == nil {
				t.Fatal("NewInMemoryCache returned nil")
			}

			if c.maxEntries != tt.wantSize {
				t.Fatalf("maxEntries = %d, want %d", c.maxEntries, tt.wantSize)
			}
		})
	}
}

func TestSetAndGet(t *testing.T) {
	c := NewInMemoryCache(10)
	ctx := context.Background()

	resp1 := makeResponse(200, "data1", 0)
	resp1.Header.Set("X-Test", "1")

	c.Set(ctx, "key1", resp1)

	gotResp, ok := c.Get(ctx, "key1")
	if !ok {
		t.Fatal("Get failed for existing key")
	}
	if gotResp.StatusCode != resp1.StatusCode {
		t.Errorf("StatusCode = %d, want %d", gotResp.StatusCode, resp1.StatusCode)
	}
	if string(gotResp.Body) != "data1" {
		t.Errorf("Body = %q, want %q", gotResp.Body, "data1")
	}
	if gotResp.Header.Get("X-Test") != "1" {
		t.Errorf("Header[X-Test] = %q, want %q", gotResp.Header.Get("X-Test"), "1")
	}

	_, ok = c.Get(ctx, "nonexistent")
	if ok {
		t.Error("Get succeeded for non existent key")
	}

	resp2 := makeResponse(201, "data2", 0)
	c.Set(ctx, "key1", resp2)
	gotResp, ok = c.Get(ctx, "key1")
	if !ok || string(gotResp.Body) != "data2" {
		t.Errorf("Update failed, want %q, got %q", "data2", string(gotResp.Body))
	}
}

func TestDelete(t *testing.T) {
	c := NewInMemoryCache(10)
	ctx := context.Background()

	c.Set(ctx, "key1", makeResponse(200, "data1", 0))

	c.Delete(ctx, "key1")
	_, ok := c.Get(ctx, "key1")
	if ok {
		t.Error("Delete failed, key1 still exists")
	}

	c.Delete(ctx, "does-not-exist")
}

func TestLRUEviction(t *testing.T) {
	c := NewInMemoryCache(3)
	ctx := context.Background()

	c.Set(ctx, "key1", makeResponse(200, "body1", 0))
	c.Set(ctx, "key2", makeResponse(200, "body1", 0))
	c.Set(ctx, "key3", makeResponse(200, "body3", 0))

	_, ok := c.Get(ctx, "key1")
	if !ok {
		t.Fatal("key1 evicted prematurely")
	}

	c.Get(ctx, "key1")
	c.Set(ctx, "key4", makeResponse(200, "body4", 0))

	_, ok = c.Get(ctx, "key2")
	if ok {
		t.Error("LRU Eviction failed: key2 was not evicted")
	}

	if _, ok := c.Get(ctx, "key1"); !ok {
		t.Error("key1 was evicted incorrectly")
	}
	if _, ok := c.Get(ctx, "key3"); !ok {
		t.Error("key3 was evicted incorrectly")
	}
	if _, ok := c.Get(ctx, "key4"); !ok {
		t.Error("key4 was evicted incorrectly")
	}
}

func TestLRUUpdate(t *testing.T) {
	c := NewInMemoryCache(3)
	ctx := context.Background()

	c.Set(ctx, "key1", makeResponse(200, "A", 0))
	c.Set(ctx, "key2", makeResponse(200, "B", 0))
	c.Set(ctx, "key3", makeResponse(200, "C", 0))

	c.Set(ctx, "key1", makeResponse(201, "A_updated", 0))
	c.Set(ctx, "key4", makeResponse(200, "D", 0))

	if _, ok := c.Get(ctx, "key2"); ok {
		t.Error("LRU position update failed: key2 was not evicted after key1 update")
	}

	if _, ok := c.Get(ctx, "key1"); !ok {
		t.Error("key1 was incorrectly evicted")
	}
}

func TestTTLEvictionOnGet(t *testing.T) {
	c := NewInMemoryCache(10)
	ctx := context.Background()

	c.Set(ctx, "key_expired", makeResponse(200, "expired", time.Millisecond))
	c.Set(ctx, "key_fresh", makeResponse(200, "fresh", 0))

	time.Sleep(2 * time.Millisecond)

	_, ok := c.Get(ctx, "key_expired")
	if ok {
		t.Error("Expired key was not deleted by Get call")
	}

	_, ok = c.Get(ctx, "key_fresh")
	if !ok {
		t.Error("Fresh key was incorrectly expired")
	}
}

func TestConcurrency(t *testing.T) {
	c := NewInMemoryCache(100)
	ctx := context.Background()
	numGoRoutines := 50
	numOperations := 1000

	var wg sync.WaitGroup

	for i := 0; i < numGoRoutines; i++ {
		wg.Add(1)
		go func(gID int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key_%d", (j%10)+1)

				switch j % 5 {
				case 0, 1:
					c.Get(ctx, key)
				case 2:
					c.Set(ctx, key, makeResponse(200, "data_"+key, 0))
				case 3:
					c.Set(ctx, key, makeResponse(200, "data_ttl_"+key, time.Hour))
				case 4:
					c.Delete(ctx, key)
				}
			}
		}(i)
	}

	wg.Wait()

	for i := 1; i <= 10; i++ {
		key := fmt.Sprintf("key_%d", i)
		if resp, ok := c.Get(ctx, key); ok && resp == nil {
			t.Errorf("Got ok=true but nil response for key %q", key)
		}
	}
}
