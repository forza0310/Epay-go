export GOTOOLCHAIN := local

.PHONY: build run check

build:
	go build -trimpath -o bin/epay ./cmd/epay

run:
	go run ./cmd/epay

check:
	go vet ./...
	go test ./...
