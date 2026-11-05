package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetOrLoad_ConcurrentCallsShareOneLoad(t *testing.T) {
	c := New[int](10, time.Minute, time.Second)
	var loads atomic.Int32
	release := make(chan struct{})

	load := func(context.Context) (int, error) {
		loads.Add(1)
		<-release // hold the load open so every goroutine piles up behind it
		return 42, nil
	}

	const callers = 50
	var wg sync.WaitGroup
	results := make(chan int, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := c.GetOrLoad(context.Background(), "k", load)
			if err != nil {
				t.Error(err)
			}
			results <- v
		}()
	}
	time.Sleep(50 * time.Millisecond) // let callers reach singleflight
	close(release)
	wg.Wait()
	close(results)

	if n := loads.Load(); n != 1 {
		t.Fatalf("burst of %d identical requests caused %d loads, want 1", callers, n)
	}
	for v := range results {
		if v != 42 {
			t.Fatalf("got %d", v)
		}
	}
}

func TestGetOrLoad_ServesFromCacheUntilTTL(t *testing.T) {
	c := New[int](10, 50*time.Millisecond, time.Second)
	var loads atomic.Int32
	load := func(context.Context) (int, error) { return int(loads.Add(1)), nil }

	first, _ := c.GetOrLoad(context.Background(), "k", load)
	second, _ := c.GetOrLoad(context.Background(), "k", load)
	if first != 1 || second != 1 {
		t.Fatalf("second call should hit cache: %d, %d", first, second)
	}

	time.Sleep(80 * time.Millisecond)
	third, _ := c.GetOrLoad(context.Background(), "k", load)
	if third != 2 {
		t.Fatalf("entry should expire after TTL, got %d", third)
	}
}

func TestGetOrLoad_DoesNotCacheErrors(t *testing.T) {
	c := New[int](10, time.Minute, time.Second)
	boom := errors.New("boom")
	calls := 0
	load := func(context.Context) (int, error) {
		calls++
		if calls == 1 {
			return 0, boom
		}
		return 7, nil
	}

	if _, err := c.GetOrLoad(context.Background(), "k", load); !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
	if v, err := c.GetOrLoad(context.Background(), "k", load); err != nil || v != 7 {
		t.Fatalf("a failed load must not be cached: %v %v", v, err)
	}
}

func TestGetOrLoad_CancelledCallerDoesNotCancelSharedLoad(t *testing.T) {
	c := New[int](10, time.Minute, time.Second)
	started := make(chan struct{})
	release := make(chan struct{})
	load := func(ctx context.Context) (int, error) {
		close(started)
		select {
		case <-release:
			return 1, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	firstErr := make(chan error, 1)
	go func() { _, err := c.GetOrLoad(ctx, "k", load); firstErr <- err }()
	<-started

	secondVal := make(chan int, 1)
	go func() { v, _ := c.GetOrLoad(context.Background(), "k", load); secondVal <- v }()
	time.Sleep(20 * time.Millisecond)

	cancel() // first client disconnects
	if err := <-firstErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("first caller should see its own cancellation, got %v", err)
	}
	close(release)
	if v := <-secondVal; v != 1 {
		t.Fatalf("second caller should still get the shared result, got %d", v)
	}
}
