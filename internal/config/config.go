// Package config reads settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"article-service/internal/elasticsearch"
	"article-service/internal/indexer"
	"article-service/internal/postgres"
)

type Config struct {
	HTTPAddr string
	DB       postgres.PoolConfig
	ESURL    string
	ESIndex  elasticsearch.IndexConfig
	Cache    CacheConfig
	Indexer  indexer.Config
}

type CacheConfig struct {
	Size        int
	TTL         time.Duration
	LoadTimeout time.Duration
}

func Load() (Config, error) {
	var e envReader
	cfg := Config{
		HTTPAddr: e.str("HTTP_ADDR", ":8080"),
		DB: postgres.PoolConfig{
			URL:              e.str("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/articles?sslmode=disable"),
			MaxConns:         int32(e.int("DB_MAX_CONNS", 20)),
			StatementTimeout: e.duration("DB_STATEMENT_TIMEOUT", 3*time.Second),
		},
		ESURL: e.str("ELASTICSEARCH_URL", "http://localhost:9200"),
		ESIndex: elasticsearch.IndexConfig{
			Alias:    e.str("ES_INDEX_ALIAS", "articles"),
			Name:     e.str("ES_INDEX_NAME", "articles_v1"),
			Shards:   e.int("ES_SHARDS", 1),
			Replicas: e.int("ES_REPLICAS", 0),
		},
		Cache: CacheConfig{
			Size:        e.int("CACHE_SIZE", 10_000),
			TTL:         e.duration("CACHE_TTL", 2*time.Second),
			LoadTimeout: e.duration("CACHE_LOAD_TIMEOUT", 5*time.Second),
		},
		Indexer: indexer.Config{
			BatchSize:    e.int("INDEXER_BATCH_SIZE", 500),
			IdleInterval: e.duration("INDEXER_IDLE_INTERVAL", 500*time.Millisecond),
			MaxBackoff:   e.duration("INDEXER_MAX_BACKOFF", 30*time.Second),
		},
	}
	return cfg, e.err()
}

// envReader collects all parse errors so they are reported together.
type envReader struct{ errs []error }

func (e *envReader) str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func (e *envReader) int(key string, def int) int {
	v := e.str(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: %w", key, err))
	}
	return n
}

func (e *envReader) duration(key string, def time.Duration) time.Duration {
	v := e.str(key, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: %w", key, err))
	}
	return d
}

func (e *envReader) err() error { return errors.Join(e.errs...) }
