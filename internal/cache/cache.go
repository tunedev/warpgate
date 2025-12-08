package cache

import (
	"context"
	"net/http"
	"time"
)

type CachedResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	ExpiresAt  time.Time
}

type Cache interface {
	Get(ctx context.Context, key string) (*CachedResponse, bool)
	Set(ctx context.Context, key string, resp *CachedResponse)
	Delete(ctx context.Context, key string)
}
