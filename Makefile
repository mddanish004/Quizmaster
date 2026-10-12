.PHONY: bootstrap dev build fmt lint test test-integration test-race e2e openapi-check migrate-up migrate-down compose-up load-smoke security

GO := go
BACKEND_DIR := backend
FRONTEND_DIR := frontend

bootstrap:
	cd $(BACKEND_DIR) && $(GO) mod download
	cd $(FRONTEND_DIR) && npm ci
	$(GO) install github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.3 || true
	$(GO) install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.29.0 || true

dev:
	docker compose up --build

build:
	cd $(BACKEND_DIR) && $(GO) build ./...
	cd $(FRONTEND_DIR) && npm run build

fmt:
	cd $(BACKEND_DIR) && gofmt -w .
	cd $(FRONTEND_DIR) && npx prettier --write src 2>/dev/null || true

lint:
	cd $(BACKEND_DIR) && gofmt -l . && $(GO) vet ./...
	cd $(FRONTEND_DIR) && npm run typecheck

test:
	cd $(BACKEND_DIR) && $(GO) test ./...
	cd $(FRONTEND_DIR) && npm test -- --run 2>/dev/null || npm run test

test-integration:
	cd $(BACKEND_DIR) && $(GO) test -tags=integration ./...

test-race:
	cd $(BACKEND_DIR) && $(GO) test -race ./internal/platform/... ./internal/live/...

e2e:
	./scripts/e2e.sh

openapi-check:
	python3 scripts/openapi_check.py docs/openapi.yaml

migrate-up:
	migrate -path database/migrations -database "$${MIGRATIONS_DATABASE_URL:-$${DATABASE_URL}}" up

migrate-down:
	migrate -path database/migrations -database "$${MIGRATIONS_DATABASE_URL:-$${DATABASE_URL}}" down 1

compose-up:
	docker compose up --build

load-smoke:
	cd $(BACKEND_DIR) && go run ./test/load -mode burst -n 200

security:
	cd $(BACKEND_DIR) && $(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./... || true
	trivy fs --severity HIGH,CRITICAL . || true
	gitleaks detect --source . || true
