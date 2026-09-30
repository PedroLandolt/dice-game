.PHONY: up down run test

up:
	docker compose up -d

down:
	docker compose down

run:
	go run ./cmd/server

test:
	go test ./...
