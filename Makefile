.PHONY: build test test-unit test-e2e test-coverage lint run setup db-up db-down migrate-up migrate-down ci

build:
	npm --prefix web install && npm --prefix web run build
	go build -o bin/slack   ./cmd/slack
	go build -o bin/setup   ./cmd/setup
	go build -o bin/cli     ./cmd/cli
	go build -o bin/runtime ./cmd/runtime

test:
	go test ./...

test-unit:
	go test -short ./...

test-e2e:
	go test -tags e2e ./internal/e2e/...

COVERAGE_THRESHOLD ?= 80

test-coverage:
	go test -short -coverprofile=coverage.out ./...
	@echo "--- Coverage Report ---"
	@go tool cover -func=coverage.out | tail -1
	@COVERAGE=$$(go tool cover -func=coverage.out | tail -1 | awk '{print $$NF}' | tr -d '%'); \
	if [ $$(echo "$$COVERAGE < $(COVERAGE_THRESHOLD)" | bc) -eq 1 ]; then \
		echo "⚠ WARNING: Total coverage $${COVERAGE}% is below $(COVERAGE_THRESHOLD)% threshold"; \
	fi

lint:
	golangci-lint run ./...

run:
	go run ./cmd/slack

run-cli:
	go run ./cmd/cli

setup:
	go run ./cmd/setup

db-up:
	docker compose up -d postgres
	@echo "Waiting for Postgres to be ready..."
	@until docker compose exec postgres pg_isready -U agent -d agent_dev > /dev/null 2>&1; do sleep 1; done
	@echo "Postgres is ready."

db-down:
	docker compose down

DATABASE_URL ?= postgres://agent:agent@localhost:5433/agent_dev?sslmode=disable

migrate-up:
	goose -dir db/migrations postgres "$(DATABASE_URL)" up

migrate-down:
	goose -dir db/migrations postgres "$(DATABASE_URL)" down

ci:
	@$(MAKE) lint
	@$(MAKE) build
	@$(MAKE) test-unit
	@if pg_isready -h localhost -p 5433 > /dev/null 2>&1; then \
		echo "==> Postgres available — running e2e tests..."; \
		$(MAKE) test-e2e; \
	else \
		echo "==> Postgres not available — skipping e2e tests (run 'make db-up')"; \
	fi
	@echo "==> CI passed"
