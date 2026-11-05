package indexer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"article-service/internal/article"
)

type fakeSource struct {
	batches [][]article.Article
	handled int
	err     error
}

func (f *fakeSource) ProcessBatch(ctx context.Context, limit int, handle func(context.Context, []article.Article) error) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	if len(f.batches) == 0 {
		return 0, nil
	}
	b := f.batches[0]
	if err := handle(ctx, b); err != nil {
		return len(b), err // source keeps the events for retry
	}
	f.batches = f.batches[1:]
	f.handled += len(b)
	return len(b), nil
}

type fakeSink struct {
	got []article.Article
	err error
}

func (f *fakeSink) Upsert(_ context.Context, a []article.Article) error {
	if f.err != nil {
		return f.err
	}
	f.got = append(f.got, a...)
	return nil
}

func newTestWorker(src Source, sink Sink, batch int) *Worker {
	return NewWorker(src, sink, Config{BatchSize: batch, IdleInterval: time.Millisecond, MaxBackoff: 5 * time.Millisecond},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestRunOnce_PassesBatchToSink(t *testing.T) {
	src := &fakeSource{batches: [][]article.Article{{{ID: "a"}, {ID: "b"}}}}
	sink := &fakeSink{}

	n, err := newTestWorker(src, sink, 10).RunOnce(context.Background())
	if err != nil || n != 2 || len(sink.got) != 2 {
		t.Fatalf("n=%d err=%v got=%v", n, err, sink.got)
	}
}

func TestRunOnce_SinkFailureKeepsEvents(t *testing.T) {
	src := &fakeSource{batches: [][]article.Article{{{ID: "a"}}}}
	sink := &fakeSink{err: errors.New("es down")}

	if _, err := newTestWorker(src, sink, 10).RunOnce(context.Background()); err == nil {
		t.Fatal("want error")
	}
	if len(src.batches) != 1 {
		t.Fatal("events must stay in the outbox when indexing fails")
	}
}

func TestRun_DrainsThenStopsOnCancel(t *testing.T) {
	src := &fakeSource{batches: [][]article.Article{{{ID: "a"}, {ID: "b"}}, {{ID: "c"}, {ID: "d"}}, {{ID: "e"}}}}
	sink := &fakeSink{}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := newTestWorker(src, sink, 2).Run(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run should stop with the context error, got %v", err)
	}
	if len(sink.got) != 5 {
		t.Fatalf("all 5 articles should be indexed, got %d", len(sink.got))
	}
}

func TestRun_KeepsRetryingAfterErrors(t *testing.T) {
	src := &fakeSource{err: errors.New("db down")}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	if err := newTestWorker(src, &fakeSink{}, 2).Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors must not stop the worker; got %v", err)
	}
}
