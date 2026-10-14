.PHONY: check backend-check agent-check fmt \
        backend-build backend-test backend-fmt backend-fmt-check \
        agent-build agent-test agent-fmt agent-fmt-check

check: backend-check agent-check

fmt: backend-fmt agent-fmt

backend-check: backend-fmt-check backend-build backend-test

agent-check: agent-fmt-check agent-build agent-test

backend-build:
	cd backend && go build -o bin/ ./...

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

agent-fmt:
	cd agent && cargo fmt

agent-fmt-check:
	cd agent && cargo fmt --check
