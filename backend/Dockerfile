# syntax=docker/dockerfile:1

# ---- build ---------------------------------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /out/seismo . && \
    go build -trimpath -ldflags="-s -w" -o /out/fakegithub ./cmd/fakegithub

# ---- shared runtime base -------------------------------------------------
FROM alpine:3.22 AS runtime
RUN apk add --no-cache ca-certificates wget && \
    addgroup -S -g 10001 seismo && adduser -S -u 10001 -G seismo seismo && \
    mkdir -p /shared && chown seismo:seismo /shared
USER seismo
WORKDIR /app

# ---- fake GitHub (development and tests only) ----------------------------
FROM runtime AS fakegithub
COPY --from=build /out/fakegithub /app/fakegithub
EXPOSE 9090
HEALTHCHECK --interval=2s --timeout=2s --retries=15 CMD wget -qO- http://127.0.0.1:9090/healthz || exit 1
ENTRYPOINT ["/app/fakegithub"]

# ---- backend (default target) --------------------------------------------
FROM runtime AS backend
COPY --from=build /out/seismo /app/seismo
EXPOSE 8080
HEALTHCHECK --interval=5s --timeout=3s --retries=10 CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/seismo"]
