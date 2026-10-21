.PHONY: all backend-build backend-test backend-fmt agent-build agent-test agent-fmt db-up db-down db-status sqlc-generate test-integration \
	dev-up dev-down dev-reset dev-logs api-test api-test-fresh

COMPOSE_DEV := docker compose -f deploy/compose.dev.yml

all: backend-build agent-build

# Local stack: Postgres, Redis, NATS, migrations, fake GitHub, backend on :8080.
dev-up:
	$(COMPOSE_DEV) up --build -d --wait

dev-down:
	$(COMPOSE_DEV) down

# Also deletes the database and NATS volumes.
dev-reset:
	$(COMPOSE_DEV) down -v

dev-logs:
	$(COMPOSE_DEV) logs -f backend fakegithub

# End-to-end API tests against the running stack (see backend/scripts/api).
api-test:
	bash backend/scripts/api/all.sh

api-test-fresh:
	bash backend/scripts/api/all.sh --fresh

db-up:
	cd backend && $(MAKE) db-up

db-down:
	cd backend && $(MAKE) db-down

db-status:
	cd backend && $(MAKE) db-status

sqlc-generate:
	cd backend && $(MAKE) sqlc-generate

backend-build:
	cd backend && go build -o bin/ ./...

backend-test:
	cd backend && go test ./...

# Disposable Postgres/Redis/NATS, Sentry HTTP integration, web and API regressions.
test-integration:
	python3 backend/scripts/test-integration.py

backend-fmt:
	cd backend && test -z "$$(gofmt -l .)"

agent-build:
	cd agent && cargo build

agent-test:
	cd agent && cargo test

agent-fmt:
	cd agent && cargo fmt --check
