SHELL := /bin/bash
include .env
export

SERVER_DIR := ./cmd/bgex-server
MIGRATIONS_DIR := ./migrations

.PHONY: run build test vet lint lint-fix fmt tidy migrate-up migrate-down migrate-new services-up services-down db-up db-down db-logs redis-up redis-down redis-logs

run:
	go run $(SERVER_DIR)

build:
	go build -o ./bin/bgex $(SERVER_DIR)

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

lint-fix:
	golangci-lint run --fix ./...

fmt:
	golangci-lint fmt ./...

tidy:
	go mod tidy

docker-up:
	docker-compose up -d

docker-down:
	docker-compose down

db-up:
	docker-compose up -d postgres

db-down:
	docker-compose stop postgres

db-logs:
	docker-compose logs -f postgres

redis-up:
	docker-compose up -d redis

redis-down:
	docker-compose stop redis

redis-logs:
	docker-compose logs -f redis

migrate-up:
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" down 1

# Usage: make migrate-new name=add_profiles
migrate-new:
	migrate create -ext sql -dir $(MIGRATIONS_DIR) -seq $(name)
