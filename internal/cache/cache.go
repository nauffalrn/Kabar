// Package cache absorbs bursty reads: singleflight merges concurrent identical
// loads and a short-TTL LRU serves hot pages from memory.
package cache

import (
	"context"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"golang.org/x/sync/singleflight"
)

type TTL[V any] struct {
	entries     *expirable.LRU[string, V]
	group       singleflight.Group
	loadTimeout time.Duration
}

// loadTimeout bounds a shared load, since it no longer belongs to one request.
func New[V any](size int, ttl, loadTimeout time.Duration) *TTL[V] {
	return &TTL[V]{
		entries:     expirable.NewLRU[string, V](size, nil, ttl),
		loadTimeout: loadTimeout,
	}
}

func (c *TTL[V]) GetOrLoad(ctx context.Context, key string, load func(context.Context) (V, error)) (V, error) {
	if v, ok := c.entries.Get(key); ok {
		return v, nil
	}

	ch := c.group.DoChan(key, func() (any, error) {
		// Detached so one client disconnecting does not cancel the other waiters.
		lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.loadTimeout)
		defer cancel()

		v, err := load(lctx)
		if err == nil { // errors are never cached
			c.entries.Add(key, v)
		}
		return v, err
	})

	var zero V
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return zero, res.Err
		}
		return res.Val.(V), nil
	}
}
