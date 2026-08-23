.PHONY: build run-server run-worker test test-docker test-all cover cover-html mocks lint tidy up up-infra up-monitoring down migrate-up migrate-down helm-sync helm-lint helm-template

BIN_DIR := bin
MIGRATE_DSN ?= postgres://avatars:avatars@localhost:5432/avatars?sslmode=disable
CHART_DIR := deploy/helm/avatars-service
HELM_VALUES ?= $(CHART_DIR)/values.yaml

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

up-infra:
	docker compose -f docker/infrastructure/docker-compose.yml up -d

up-monitoring:
	docker compose -f docker/monitoring/docker-compose.yml up -d

down:
	docker compose -f docker/docker-compose.yml down -v

migrate-up:
	migrate -path migrations -database "$(MIGRATE_DSN)" up

migrate-down:
	migrate -path migrations -database "$(MIGRATE_DSN)" down 1

# Миграции в чарте - копия migrations/: Helm читает файлы только внутри чарта.
helm-sync:
	rm -f $(CHART_DIR)/files/migrations/*.sql
	cp migrations/*.sql $(CHART_DIR)/files/migrations/

helm-lint: helm-sync
	helm lint $(CHART_DIR)
	helm lint $(CHART_DIR) -f $(CHART_DIR)/values-staging.yaml
	helm lint $(CHART_DIR) -f $(CHART_DIR)/values-production.yaml

helm-template: helm-sync
	helm template avatars-service $(CHART_DIR) -f $(HELM_VALUES)
