.PHONY: up down test test-integration seed lint

up:            ## start postgres, elasticsearch, api and indexer
	docker compose up -d --build

down:
	docker compose down

test:          ## unit tests (no external services needed)
	go test -race ./...

# Integration tests need the compose stack running (make up).
test-integration:
	TEST_DATABASE_URL=postgres://postgres:postgres@localhost:$(or $(POSTGRES_PORT),5432)/articles_test?sslmode=disable \
	TEST_ELASTICSEARCH_URL=http://localhost:9200 \
	go test -race -tags=integration -count=1 -p 1 ./...

seed:          ## load 1M skewed articles (override: make seed N=5000000)
	docker compose run --rm seed -articles $(or $(N),1000000)

lint:
	go vet ./...
