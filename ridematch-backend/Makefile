.PHONY: run migrate build fmt vet test tidy dev

# Loads .env-style vars is handled by the app itself (godotenv); these
# targets just wrap the go commands you'll use day to day.

## run: start the API server
run:
	go run ./cmd/api

## migrate: create/update all database tables to match current models
migrate:
	go run ./cmd/migrate

## build: compile the API server binary to ./bin/api
build:
	mkdir -p bin
	go build -o bin/api ./cmd/api

## build-migrate: compile the migration binary to ./bin/migrate
build-migrate:
	mkdir -p bin
	go build -o bin/migrate ./cmd/migrate

## fmt: format all Go source files
fmt:
	gofmt -w .

## vet: run go vet across the module
vet:
	go vet ./...

## test: run the Go unit test suite
test:
	go test ./...

## tidy: sync go.mod/go.sum with actual imports
tidy:
	go mod tidy

## dev: format, vet, then run — the everyday inner-loop command
dev: fmt vet run
