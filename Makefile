.PHONY: dev infra-up infra-down test build web-install web-dev web-build

dev:
	@set -a; test ! -f .env || . ./.env; set +a; go run ./cmd/server

infra-up:
	docker compose up -d postgres redis

infra-down:
	docker compose down

test:
	go test ./...

build:
	go build ./cmd/server

web-install:
	cd web && pnpm install

web-dev:
	cd web && pnpm dev

web-build:
	cd web && pnpm build
