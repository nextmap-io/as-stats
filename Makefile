.PHONY: build build-collector build-api run-collector run-api test lint migrate dev docker-build docker-up docker-down frontend-dev clean

# Go build
build: build-collector build-api

build-collector:
	go build -o bin/collector ./cmd/collector

build-api:
	go build -o bin/api ./cmd/api

# Run locally
run-collector:
	go run ./cmd/collector

run-api:
	go run ./cmd/api

# Test
test:
	go test ./... -v -race

lint:
	golangci-lint run ./...

# Database
migrate:
	@echo "Applying migrations to ClickHouse..."
	@for f in migrations/*.up.sql; do \
		echo "  Applying $$f"; \
		clickhouse-client --host localhost --user asstats --password asstats --database asstats --multiquery < "$$f"; \
	done

# Docker
docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-build:
	docker compose build

# Frontend
frontend-dev:
	cd frontend && npm run dev

frontend-build:
	cd frontend && npm run build

# Dev: start infrastructure + run services
dev: docker-up
	@echo "ClickHouse and Redis are running."
	@echo "Run 'make run-collector' and 'make run-api' in separate terminals."

clean:
	rm -rf bin/ frontend/dist/
