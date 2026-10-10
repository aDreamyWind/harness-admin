.PHONY: help dev bootstrap-admin seed-test-organization infra-up infra-down test build web-install web-dev web-build

help:
	@echo "可用命令："
	@echo "  make dev             启动 Go HTTP 服务"
	@echo "  make bootstrap-admin 创建初始管理员"
	@echo "  make seed-test-organization 写入测试企业组织树"
	@echo "  make infra-up        启动 PostgreSQL 和 Redis"
	@echo "  make infra-down      停止本地基础设施"
	@echo "  make test            运行 Go 测试"
	@echo "  make build           构建 Go HTTP 服务"
	@echo "  make web-install     安装前端依赖"
	@echo "  make web-dev         启动前端开发服务"
	@echo "  make web-build       构建前端"

dev:
	@set -a; test ! -f .env || . ./.env; set +a; go run ./cmd/server

bootstrap-admin:
	@set -a; test ! -f .env || . ./.env; set +a; go run ./cmd/bootstrap-admin

seed-test-organization:
	@set -a; test ! -f .env || . ./.env; set +a; go run ./cmd/seed-test-organization

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
