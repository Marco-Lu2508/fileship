BINARY=fileship
GO=go
NPM=npm

.PHONY: all dev build build-frontend build-backend clean docker docker-up

all: build

## Entwicklung
dev-backend:
	ROOT_PATH=./data DB_PATH=./fileship.db $(GO) run ./cmd/fileship

dev-frontend:
	cd frontend && $(NPM) run dev

## Build
build: build-frontend build-backend

build-frontend:
	cd frontend && $(NPM) ci && $(NPM) run build

build-backend:
	CGO_ENABLED=1 $(GO) build -ldflags="-s -w" -o $(BINARY) ./cmd/fileship

## Docker
docker:
	docker build -t fileship:latest .

docker-up:
	docker compose up -d

docker-down:
	docker compose down

## Cleanup
clean:
	rm -f $(BINARY)
	rm -rf frontend/dist
	rm -f fileship.db
