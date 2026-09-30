export DATABASE_URL ?= postgres://dice_app:dice_app@localhost:5432/dice?sslmode=disable

.PHONY: up down run test

up:
	docker compose up -d

down:
	docker compose down

run:
	go run ./cmd/server

test:
	go test ./...
