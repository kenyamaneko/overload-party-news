.PHONY: build test vet fmt run tidy db-up db-down db-reset help

APP := overload-party-news

build: ## Build Docker image
	docker build -t $(APP) .

test: ## Run tests (Testcontainers; requires Docker running)
	go test ./... -count=1 -race

vet: ## Run go vet
	go vet ./...

tidy: ## Tidy dependencies
	go mod tidy

fmt: ## Format code
	gofmt -s -w .

db-up: ## Start local Postgres (docker compose)
	docker compose up -d postgres

db-down: ## Stop local Postgres
	docker compose down

db-reset: ## Drop volume and recreate DB
	docker compose down -v
	docker compose up -d postgres

run: db-up ## Run news server locally against compose Postgres (local env 込み)
	ENV=local \
	INTERNAL_PORT=9008 \
	ADMIN_PORT=9108 \
	DATABASE_CONN="host=localhost port=5432 dbname=news user=news password=news sslmode=disable" \
	GOOGLE_CLOUD_PROJECT=news-local \
	NEWS_ARTICLE_COLLECTED_SUBSCRIPTION=news-article-collected-news-sub \
	PUBSUB_EMULATOR_HOST=localhost:8085 \
	go run ./cmd/server

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
