.PHONY: dev db-up test build compose-up compose-down

dev:
	@echo "Run 'make db-up', then start 'go run ./cmd/server' and 'npm --prefix frontend run dev' in separate terminals."

db-up:
	docker compose up -d postgres

test:
	go test ./cmd/... ./internal/...
	npm --prefix frontend run lint
	npm --prefix frontend run build

build:
	npm --prefix frontend run build
	go build -o bin/artifact-hub ./cmd/server

compose-up:
	docker compose up --build

compose-down:
	docker compose down
