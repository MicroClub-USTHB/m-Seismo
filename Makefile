.PHONY: all backend-build backend-test backend-fmt agent-build agent-test agent-fmt db-up db-down db-status sqlc-generate

all: backend-build agent-build

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

backend-fmt:
	cd backend && test -z "$$(gofmt -l .)"

agent-build:
	cd agent && cargo build

agent-test:
	cd agent && cargo test

agent-fmt:
	cd agent && cargo fmt --check
