# Semua variabel boleh ditimpa: `make run APP_ENCRYPTION_KEY=...`
# Key di bawah HANYA untuk development.

TARGET_MARIADB_ADDR ?= 127.0.0.1:3307
APP_ENCRYPTION_KEY ?= ViQIBs1QPisgpu5jmZ6xOAvG25UXZhzma/gdUag/afA=
TARGET_DB_URL ?= postgres://app:app@127.0.0.1:5435/sample?sslmode=disable
TEST_DATABASE_URL ?= postgres://postgres:postgres@127.0.0.1:5433/dbmp_test?sslmode=disable
PKG ?= ./...

export APP_ENCRYPTION_KEY
export TARGET_DB_URL
export TEST_DATABASE_URL
export TARGET_MARIADB_ADDR

.PHONY: help up down reset-target run test test-unit test-integration
.PHONY: ... reset-mariadb

help: 
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | sed -E 's/:.*## /\t/'

up: 
	docker compose up -d --wait

down: 
	docker compose down

reset-target:
	docker compose rm -sfv target-db
	docker compose up -d --wait target-db

reset-mariadb:
	docker compose rm -sfv target-mariadb
	docker compose up -d --wait target-mariadb
	
# Membaca .env (jika ada) dengan sintaks shell, jadi nilai berkutip dan karakter khusus aman.
# Isi .env menimpa default di atas. Lihat .env.example.
run: ## jalankan server (baca .env bila ada; ADMIN_PASSWORD wajib pada run pertama)
	@set -a; [ ! -f .env ] || . ./.env; set +a; go run ./cmd/server

test-unit: ## unit test saja (-short), tanpa database; boleh PKG=./internal/dbconn
	go test -short $(PKG) -count=1

test-integration: ## integration test saja; butuh `make up`. Gagal (bukan skip) jika DB tidak ada
	REQUIRE_DB=1 go test $(PKG) -count=1

test: test-unit test-integration ## unit lalu integration; butuh `make up`
