.PHONY: build run clean test lint deps

# Build the application
build:
	go build -o bin/rewards ./cmd/rewards

# Run the application
run:
	go run ./cmd/rewards/main.go

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

# Run with custom configuration
run-custom:
	@echo "Running with custom configuration..."
	SERVER_PORT=9090 go run ./cmd/rewards/main.go

