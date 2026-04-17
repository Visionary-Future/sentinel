.PHONY: run build test lint migrate-up migrate-down docker-up docker-down ui-dev ui-build

BINARY=sentinel
CONFIG=configs/sentinel.yaml
MIGRATIONS_DIR=migrations
DB_URL=postgres://sentinel:sentinel@localhost:5432/sentinel?sslmode=disable

run:
	go run ./cmd/sentinel -config $(CONFIG)

build:
	CGO_ENABLED=0 go build -o bin/$(BINARY) ./cmd/sentinel

test:
	go test ./... -race -count=1

lint:
	golangci-lint run ./...

migrate-up:
	migrate -path $(MIGRATIONS_DIR) -database "$(DB_URL)" up

migrate-down:
	migrate -path $(MIGRATIONS_DIR) -database "$(DB_URL)" down 1

migrate-create:
	@read -p "Migration name: " name; \
	migrate create -ext sql -dir $(MIGRATIONS_DIR) -seq $$name

docker-up:
	docker-compose -f deploy/docker-compose.yml up -d

docker-down:
	docker-compose -f deploy/docker-compose.yml down

docker-logs:
	docker-compose -f deploy/docker-compose.yml logs -f

tidy:
	go mod tidy

ui-dev:
	cd web && npm run dev

ui-build:
	cd web && npm run build
