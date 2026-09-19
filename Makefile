SHELL := /bin/bash
.DEFAULT_GOAL := help

# Подхватываем .env, если он есть.
ifneq (,$(wildcard .env))
include .env
export
endif

help: ## Список команд
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

env: ## Создать .env с новым секретом прокси (если .env ещё нет)
	@if [ -f .env ]; then echo ".env уже есть — не трогаю"; else \
	  sed "s/__GENERATED__/$$(openssl rand -hex 32)/" .env.example > .env; echo ".env создан"; fi

db-up: ## Поднять PostgreSQL и дождаться готовности
	docker compose up -d --wait postgres

db-down: ## Остановить PostgreSQL (данные сохраняются)
	docker compose down

db-reset: ## Снести данные PostgreSQL и создать заново
	docker compose down -v
	docker compose up -d --wait postgres

migrate: ## Применить миграции к dev-БД
	cd backend && go run ./cmd/kupol migrate up

test-back: db-up ## Тесты бэкенда (нужна БД)
	cd backend && KUPOL_TEST_DATABASE_URL="$(KUPOL_TEST_DATABASE_URL)" go test ./... -count=1

test-proxy: ## Тесты PHP-прокси
	XDEBUG_MODE=off php frontend/tests/php/run.php

test-front: ## Тесты фронтенда (vitest) и линтер
	cd frontend && npm run lint && npm test

test: test-back test-proxy test-front ## Все быстрые тесты

e2e-api: db-up ## Сквозная проверка curl -> PHP-прокси -> Go -> PostgreSQL
	./scripts/e2e.sh

e2e-browser: db-up ## Тесты в настоящем Chromium на полном стеке (сам поднимет scripts/dev.sh)
	cd frontend && npx playwright test

e2e: e2e-api e2e-browser ## Все сквозные тесты

e2e-apache: db-up ## Боевая сборка на настоящем Apache + PHP 8.3 (docker): check-site.sh и браузерные тесты
	./scripts/e2e-apache.sh

test-deploy: db-up ## Скрипты деплоя на настоящих sshd и FTP (локально): откат, --wipe, защита config.php
	./scripts/test-deploy.sh

test-nginx: db-up ## Боевой конфиг nginx для VPS настоящим nginx (docker): подпись, X-Forwarded-For, лимиты
	./scripts/test-nginx.sh

test-infra: e2e-apache test-deploy test-nginx ## Всё, что проверяет инфраструктуру (нужен docker, lftp, sshd)

build-front: ## Сборка фронтенда в frontend/dist
	cd frontend && npm ci && npm run build

dev: ## Всё для разработки: БД, Go API, PHP-прокси, Vite
	./scripts/dev.sh

build-back: ## Собрать бинарник Go для linux/amd64 (VPS)
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
	  -ldflags "-s -w -X kupol/internal/version.Version=$$(git describe --tags --always --dirty 2>/dev/null || echo dev) -X kupol/internal/version.Commit=$$(git rev-parse --short HEAD 2>/dev/null || echo unknown)" \
	  -o bin/kupol ./cmd/kupol

.PHONY: help env db-up db-down db-reset migrate test-back test-proxy test-front test e2e-api e2e-browser e2e e2e-apache test-deploy test-nginx test-infra dev build-back build-front
