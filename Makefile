.PHONY: build run test vet tidy up dc-up dc-down dc-logs

build:
	@go build -o bin/server ./cmd/server

run:
	@go run ./cmd/server

test:
	@go test ./... -v

vet:
	@go vet ./...

tidy:
	@go mod tidy

up:
	@docker compose up -d --build

dc-up:
	@docker compose up -d postgres redis

dc-down:
	@docker compose down

dc-logs:
	@docker compose logs -f
