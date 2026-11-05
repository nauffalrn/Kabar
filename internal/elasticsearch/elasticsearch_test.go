package elasticsearch

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"article-service/internal/article"
)

// recordingServer captures the last request and replies with a fixed response.
type recordingServer struct {
	method, path, body string
	status             int
	reply              string
}

func (s *recordingServer) start(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.method, s.path, s.body = r.Method, r.URL.Path, string(b)
		if s.status == 0 {
			s.status = http.StatusOK
		}
		w.WriteHeader(s.status)
		_, _ = io.WriteString(w, s.reply)
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, srv.Client())
}

func TestBuildSearchRequest(t *testing.T) {
	after := &article.Cursor{CreatedAt: time.UnixMilli(1_700_000_000_123).UTC(), ID: "x"}
	req := buildSearchRequest(article.ListParams{Query: "banjir", AuthorIDs: []string{"a1", "a2"}, Limit: 21, After: after})

	raw, _ := json.Marshal(req)
	var got struct {
		Size           int              `json:"size"`
		TrackTotalHits bool             `json:"track_total_hits"`
		SearchAfter    []any            `json:"search_after"`
		Sort           []map[string]any `json:"sort"`
		Query          map[string]any   `json:"query"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Size != 21 || got.TrackTotalHits {
		t.Fatalf("size/track_total_hits wrong: %s", raw)
	}
	if len(got.SearchAfter) != 2 || got.SearchAfter[0].(float64) != 1_700_000_000_123 || got.SearchAfter[1] != "x" {
		t.Fatalf("search_after must mirror the keyset (millis, id): %v", got.SearchAfter)
	}
	if got.Sort[0]["created_at"] != "desc" || got.Sort[1]["id"] != "desc" {
		t.Fatalf("sort must be created_at desc, id desc: %v", got.Sort)
	}
	s := string(raw)
	for _, want := range []string{`"terms":{"author_id":["a1","a2"]}`, `"title.id"`, `"body.en"`, `"operator":"and"`, `"filter"`} {
		if !strings.Contains(s, want) {
			t.Errorf("request missing %s: %s", want, s)
		}
	}
	if strings.Contains(s, `"must"`) {
		t.Error("keyword clause must be in filter context (no scoring needed for date sort)")
	}
}

func TestBuildSearchRequest_NoAuthorNoCursor(t *testing.T) {
	raw, _ := json.Marshal(buildSearchRequest(article.ListParams{Query: "x", Limit: 5}))
	if strings.Contains(string(raw), "author_id") || strings.Contains(string(raw), "search_after") {
		t.Fatalf("unexpected clauses: %s", raw)
	}
}

func TestSearcher_List(t *testing.T) {
	srv := &recordingServer{reply: `{"hits":{"hits":[{"_source":{
		"id":"ar1","author_id":"au1","author_name":"Budi","title":"T","body":"B",
		"created_at":"2026-11-03T10:00:00.123456Z","updated_at":"2026-11-03T11:00:00Z"}}]}}`}
	s := NewSearcher(srv.start(t), "articles")

	got, err := s.List(context.Background(), article.ListParams{Query: "t", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if srv.method != http.MethodPost || srv.path != "/articles/_search" {
		t.Fatalf("wrong endpoint %s %s", srv.method, srv.path)
	}
	if len(got) != 1 || got[0].AuthorName != "Budi" || got[0].CreatedAt.Nanosecond() != 123456000 {
		t.Fatalf("unexpected result %+v", got)
	}
}

func TestSearcher_PropagatesAPIError(t *testing.T) {
	srv := &recordingServer{status: 503, reply: `{"error":"unavailable"}`}
	if _, err := NewSearcher(srv.start(t), "articles").List(context.Background(), article.ListParams{Query: "x", Limit: 1}); err == nil {
		t.Fatal("want error")
	}
}

func TestSearcher_ClassifiesTransientErrors(t *testing.T) {
	params := article.ListParams{Query: "x", Limit: 1}

	for _, tt := range []struct {
		status        int
		wantTransient bool
	}{
		{http.StatusTooManyRequests, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
		{http.StatusBadRequest, false},
		{http.StatusInternalServerError, false},
	} {
		srv := &recordingServer{status: tt.status, reply: `{}`}
		_, err := NewSearcher(srv.start(t), "articles").List(context.Background(), params)
		if got := errors.Is(err, article.ErrUnavailable); got != tt.wantTransient {
			t.Errorf("status %d: transient = %v, want %v (%v)", tt.status, got, tt.wantTransient, err)
		}
	}

	t.Run("timeout", func(t *testing.T) {
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			<-release // never answer before the client gives up
		}))
		t.Cleanup(srv.Close)
		t.Cleanup(func() { close(release) }) // runs first, so Close is not blocked by the handler
		c := NewClient(srv.URL, &http.Client{Timeout: 50 * time.Millisecond})

		_, err := NewSearcher(c, "articles").List(context.Background(), params)
		if !errors.Is(err, article.ErrUnavailable) {
			t.Fatalf("timeout should be transient, got %v", err)
		}
	})

	t.Run("connection refused", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		srv.Close() // nothing listens on this address any more

		_, err := NewSearcher(NewClient(srv.URL, nil), "articles").List(context.Background(), params)
		if !errors.Is(err, article.ErrUnavailable) {
			t.Fatalf("connection refused should be transient, got %v", err)
		}
	})
}

func TestWriter_UpsertUsesExternalVersioning(t *testing.T) {
	srv := &recordingServer{reply: `{"errors":false,"items":[]}`}
	w := NewWriter(srv.start(t), "articles")

	err := w.Upsert(context.Background(), []article.Article{{ID: "ar1", Version: 3, Title: "t"}, {ID: "ar2", Version: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if srv.path != "/_bulk" {
		t.Fatalf("path %s", srv.path)
	}

	lines := []string{}
	sc := bufio.NewScanner(strings.NewReader(srv.body))
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) != 4 {
		t.Fatalf("want 4 NDJSON lines (action+doc per article), got %d", len(lines))
	}
	for _, want := range []string{`"_id":"ar1"`, `"version":3`, `"version_type":"external"`} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("action line missing %s: %s", want, lines[0])
		}
	}
}

func TestWriter_ConflictMeansAlreadyUpToDate(t *testing.T) {
	srv := &recordingServer{reply: `{"errors":true,"items":[
		{"index":{"_id":"ar1","status":409,"error":{"type":"version_conflict_engine_exception"}}},
		{"index":{"_id":"ar2","status":201}}]}`}
	if err := NewWriter(srv.start(t), "articles").Upsert(context.Background(), []article.Article{{ID: "ar1"}, {ID: "ar2"}}); err != nil {
		t.Fatalf("409 must be treated as success, got %v", err)
	}
}

func TestWriter_RealFailureIsReported(t *testing.T) {
	srv := &recordingServer{reply: `{"errors":true,"items":[
		{"index":{"_id":"ar1","status":400,"error":{"type":"mapper_parsing_exception"}}}]}`}
	err := NewWriter(srv.start(t), "articles").Upsert(context.Background(), []article.Article{{ID: "ar1"}})
	if err == nil || !strings.Contains(err.Error(), "ar1") {
		t.Fatalf("want failure naming ar1, got %v", err)
	}
}

func TestEnsureIndex(t *testing.T) {
	cfg := IndexConfig{Alias: "articles", Name: "articles_v1", Shards: 1}

	t.Run("alias exists: nothing to do", func(t *testing.T) {
		srv := &recordingServer{status: 200}
		if err := EnsureIndex(context.Background(), srv.start(t), cfg); err != nil {
			t.Fatal(err)
		}
		if srv.method != http.MethodHead {
			t.Fatalf("should only check the alias, last call was %s", srv.method)
		}
	})

	t.Run("alias missing: creates index with mapping", func(t *testing.T) {
		var calls []string
		var created string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, r.Method+" "+r.URL.Path)
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			b, _ := io.ReadAll(r.Body)
			created = string(b)
		}))
		defer srv.Close()

		if err := EnsureIndex(context.Background(), NewClient(srv.URL, nil), cfg); err != nil {
			t.Fatal(err)
		}
		if len(calls) != 2 || calls[1] != "PUT /articles_v1" {
			t.Fatalf("calls %v", calls)
		}
		for _, want := range []string{`"indonesian"`, `"english"`, `"index.sort.field"`, `"articles":{"is_write_index":true}`, `"dynamic":"strict"`} {
			if !strings.Contains(created, want) {
				t.Errorf("index definition missing %s", want)
			}
		}
	})

	t.Run("lost creation race is fine", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"type":"resource_already_exists_exception"}}`)
		}))
		defer srv.Close()
		if err := EnsureIndex(context.Background(), NewClient(srv.URL, nil), cfg); err != nil {
			t.Fatal(err)
		}
	})
}
