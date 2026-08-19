.PHONY: build run-server run-worker test test-docker test-all cover cover-html mocks lint tidy up down migrate-up migrate-down

BIN_DIR := bin
MIGRATE_DSN ?= postgres://avatars:avatars@localhost:5432/avatars?sslmode=disable

build:
	go build -o $(BIN_DIR)/server ./cmd/server
	go build -o $(BIN_DIR)/worker ./cmd/worker

run-server:
	go run ./cmd/server

run-worker:
	go run ./cmd/worker

test:
	go test ./...

test-docker:
	go test -tags=docker ./internal/repository/...

test-all:
	go test -tags=docker ./...

cover:
	go test -tags=docker -coverprofile=coverage.out ./...
	grep -v '/mocks/' coverage.out > coverage.filtered.out
	go tool cover -func=coverage.filtered.out | tail -1

cover-html: cover
	go tool cover -html=coverage.filtered.out

# Требует mockgen: go install go.uber.org/mock/mockgen@latest
mocks:
	go generate ./...

lint:
	go vet ./...
	go vet -tags=docker ./internal/repository/...
	golangci-lint run ./...

tidy:
	go mod tidy

up:
	docker compose -f docker/docker-compose.yml up -d --build

down:
	docker compose -f docker/docker-compose.yml down -v

migrate-up:
	migrate -path migrations -database "$(MIGRATE_DSN)" up

migrate-down:
	migrate -path migrations -database "$(MIGRATE_DSN)" down 1
