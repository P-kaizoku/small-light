# Build stage: compile a static binary
FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /server ./cmd/server

# Runtime stage: minimal, non-root
FROM alpine:3.20

RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 appuser

USER appuser

COPY --from=builder /server /usr/local/bin/server

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=5s --retries=5 \
    CMD wget -qO- http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["server"]
