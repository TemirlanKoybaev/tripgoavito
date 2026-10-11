-include .env

.PHONY: generate migrate migrate-down run test

generate:
	go tool oapi-codegen \
		-generate types,chi-server \
		-include-operation-ids createTrip,getTrip,finishTrip,health,ready \
		-package api \
		-o api/api.gen.go \
		contracts/openapi/trip-service.openapi.yaml

migrate:
	go tool goose -dir migrations postgres "$(DATABASE_URL)" up

migrate-down:
	go tool goose -dir migrations postgres "$(DATABASE_URL)" down

run:
	go run ./cmd/trip-service

test:
	go test ./...