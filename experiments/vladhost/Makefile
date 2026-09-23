SHELL := /bin/bash
.DEFAULT_GOAL := help

ifneq (,$(wildcard .env))
include .env
export
endif

help: ## Список команд
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'

env: ## Создать .env с новым секретом (если его ещё нет)
	@if [ -f .env ]; then echo ".env уже есть — не трогаю"; else \
	  sed "s/__GENERATED__/$$(openssl rand -hex 32)/" .env.example > .env; echo ".env создан"; fi

db-up: ## Поднять PostgreSQL и дождаться готовности
	docker compose up -d --wait postgres

db-down: ## Остановить PostgreSQL (данные сохраняются)
	docker compose down

migrate: ## Применить миграции к dev-БД
	cd backend && go run ./cmd/vladhost migrate up

dev-back: ## Запустить бэкенд на VLADHOST_ADDR
	cd backend && go run ./cmd/vladhost serve

dev-front: ## Запустить Vite (проксирует /api на бэкенд)
	cd frontend && npm run dev

build: ## Собрать бинарь (linux/amd64, как на VPS) и фронтенд
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o ../bin/vladhost ./cmd/vladhost
	cd frontend && npm run build

test-back: db-up ## Тесты и линтер бэкенда (нужна БД)
	cd backend && go vet ./... && golangci-lint run ./... && go test ./... -count=1

test-front: ## Фронтенд: типы, линтер, тесты
	cd frontend && npm run typecheck && npm run lint && npm test

e2e: db-up ## Сквозные тесты в браузере (Playwright + настоящий бэкенд)
	cd backend && go build -o ../bin/vladhost ./cmd/vladhost
	cd frontend && npm run e2e

test: test-back test-front ## Всё быстрое (без e2e)

.PHONY: help env db-up db-down migrate dev-back dev-front build test-back test-front e2e test
