# Build stage: compile a static binary
FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/server

# Runtime stage: minimal, non-root
FROM alpine:3.20

RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 appuser

USER appuser

COPY --from=builder /server /usr/local/bin/server

EXPOSE 8080

ENTRYPOINT ["server"]
