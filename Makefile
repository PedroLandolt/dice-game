export DATABASE_URL ?= postgres://dice_app:dice_app@localhost:5432/dice?sslmode=disable

.PHONY: up down run test test-race

up:
	docker compose up -d

down:
	docker compose down

run: export DEV_MODE = true
run:
	go run ./cmd/server

test:
	go test ./...

test-race:
	docker run --rm -v "$(CURDIR):/src" -w /src -e DATABASE_URL="postgres://dice_app:dice_app@host.docker.internal:5432/dice?sslmode=disable" golang:1.27 go test -race ./...
