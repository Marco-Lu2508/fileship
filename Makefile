CARGO=cargo
BINARY=fileship
NPM=npm

.PHONY: all dev build build-frontend build-backend test clean docker docker-up

all: build

## Entwicklung
dev-backend:
	ROOT_PATH=./data DB_PATH=./fileship.db $(CARGO) run

dev-frontend:
	cd frontend && $(NPM) run dev

## Build
build: build-frontend build-backend

build-frontend:
	cd frontend && $(NPM) ci && $(NPM) run build

build-backend:
	$(CARGO) build --release

test:
	$(CARGO) test --release --all-targets

## Docker
docker:
	docker build -t fileship:latest .

docker-up:
	docker compose up -d

docker-down:
	docker compose down

## Cleanup
clean:
	rm -f $(BINARY) fileship.db
	rm -rf target frontend/dist
