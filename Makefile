# Semua variabel boleh ditimpa: `make run APP_ENCRYPTION_KEY=...`
# Key di bawah HANYA untuk development.

APP_ENCRYPTION_KEY ?= ViQIBs1QPisgpu5jmZ6xOAvG25UXZhzma/gdUag/afA=
TARGET_DB_URL ?= postgres://app:app@127.0.0.1:5435/sample?sslmode=disable

export APP_ENCRYPTION_KEY
export TARGET_DB_URL

.PHONY: help up down reset-target run test test-integration

help: 
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | sed -E 's/:.*## /\t/'

up: 
	docker compose up -d --wait

down: 
	docker compose down

reset-target:
	docker compose rm -sfv target-db
	docker compose up -d --wait target-db

run: ## jalankan server (butuh ADMIN_PASSWORD di environment pada run pertama)
	go run ./cmd/server

test: 
	go test ./... -count=1

test-integration:
	go test ./internal/explorer/ -count=1 -v
