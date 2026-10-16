.PHONY: all backend-build backend-test backend-fmt backend-vet backend-run backend-migrate dev-up dev-down agent-build agent-test agent-fmt

all: backend-build agent-build

backend-build:
	cd backend && go build -o bin/ ./...

backend-test:
	cd backend && go test ./...

backend-fmt:
	cd backend && test -z "$$(gofmt -l .)"

backend-vet:
	cd backend && go vet ./...

# Both read the configuration from the environment (see backend/.env.example).
backend-run:
	cd backend && go run ./cmd/server

backend-migrate:
	cd backend && go run ./cmd/server migrate

dev-up:
	docker compose -f deploy/compose.yaml up -d --wait

dev-down:
	docker compose -f deploy/compose.yaml down

agent-build:
	cd agent && cargo build

agent-test:
	cd agent && cargo test

agent-fmt:
	cd agent && cargo fmt --check
