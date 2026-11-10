export GOTOOLCHAIN := local

.PHONY: generate check-generated build run check

generate:
	go run ./cmd/plugin-gen

check-generated:
	go run ./cmd/plugin-gen -check

build: generate
	go build -trimpath -o bin/epay ./cmd/epay

run: generate
	go run ./cmd/epay

check: generate
	go vet ./...
	go test ./...
