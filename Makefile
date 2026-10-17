.PHONY: check backend-check agent-check fmt lint \
        backend-build backend-test backend-fmt backend-fmt-check \
        agent-build agent-test agent-fmt agent-fmt-check backend-lint agent-lint \
        agent-integration

check: backend-check agent-check

fmt: backend-fmt agent-fmt

lint: backend-lint agent-lint

backend-check: backend-fmt-check backend-lint backend-build backend-test

agent-check: agent-fmt-check agent-lint agent-build agent-test

backend-build:
	cd backend && mkdir -p bin && go build -o bin/m-seismo .

backend-test:
	cd backend && go test ./...

backend-fmt:
	cd backend && gofmt -w .

backend-fmt-check:
	@cd backend && out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

agent-build:
	cd agent && cargo build --locked

agent-test:
	cd agent && cargo test --locked

agent-integration:
	cd agent && cargo test --locked --test docker_integration -- --ignored --nocapture

agent-fmt:
	cd agent && cargo fmt

agent-fmt-check:
	cd agent && cargo fmt --check

backend-lint:
	cd backend && golangci-lint run ./...

agent-lint:
	cd agent && cargo clippy --locked --all-targets -- -D warnings
