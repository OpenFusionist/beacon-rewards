.PHONY: build run clean test lint deps

include .env

# Build the application
build:
	swag init -g cmd/rewards/main.go -o docs
	go build -o bin/rewards ./cmd/rewards

# Run the application
run: build
	./bin/rewards

# Clean build artifacts
clean:
	rm -rf bin/

# Run tests
test:
	go test -v ./...

# Run linter
lint:
	golangci-lint run

# Download dependencies
deps:
	go mod download
	go mod tidy

