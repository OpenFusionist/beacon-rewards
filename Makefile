.PHONY: build run clean test lint deps docker-build docker-run

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

# Build Docker image
docker-build:
	docker build -t beacon-rewards:latest .

# Build and run Docker container
docker-run: docker-build
	docker run -p $(SERVER_PORT):$(SERVER_PORT) --env-file $(CURDIR)/.env -v $(CURDIR)/data:/app/data --restart=unless-stopped --name beacon-rewards -d beacon-rewards

docker-stop:
	docker stop beacon-rewards

docker-remove: docker-stop
	docker rm beacon-rewards

docker-logs:
	docker logs -f beacon-rewards --tail 100

docker-exec:
	docker exec -it beacon-rewards /bin/sh
