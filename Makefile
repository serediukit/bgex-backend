SHELL := /bin/bash
include .env
export

MIGRATIONS_DIR := ./migrations

.PHONY: run build test vet lint tidy migrate-up migrate-down migrate-new services-up services-down db-up db-down db-logs redis-up redis-down redis-logs

run:
	go run ./cmd/app

build:
	go build -o ./bin/bgex ./cmd/app

test:
	go test ./...

vet:
	go vet ./...

tidy:
	go mod tidy

services-up:
	docker compose up -d

services-down:
	docker compose down

db-up:
	docker compose up -d postgres

db-down:
	docker compose stop postgres

db-logs:
	docker compose logs -f postgres

redis-up:
	docker compose up -d redis

redis-down:
	docker compose stop redis

redis-logs:
	docker compose logs -f redis

migrate-up:
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" down 1

# Usage: make migrate-new name=add_profiles
migrate-new:
	migrate create -ext sql -dir $(MIGRATIONS_DIR) -seq $(name)
