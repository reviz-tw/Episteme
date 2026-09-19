.PHONY: build test test-integration generate dev compose
build:
	go build -o bin/server ./cmd/server
	go build -o bin/mcp-stdio ./cmd/mcp-stdio
	go build -o bin/export ./cmd/export
	cd web && npm ci && npm run build
test:
	go test -race ./...
	cd web && npm run typecheck
test-integration:
	test -n "$$TEST_DATABASE_URL"
	go test -race -count=1 ./internal/api ./internal/mcp
generate:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate
dev:
	go run ./cmd/server
compose:
	docker compose --env-file .env -f deploy/docker-compose.yml up --build -d
