# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install build dependencies
# git is often required for fetching dependencies
RUN apk add --no-cache git

# Copy go module files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
# CGO_ENABLED=0 creates a statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -o rewards ./cmd/rewards

# Final stage
FROM alpine:latest

WORKDIR /app

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Copy binary from builder
COPY --from=builder /app/rewards .

# Copy default configuration file
COPY depositor-name.yaml .

# Create data directory for rewards history
RUN mkdir -p data

# Expose the default port
EXPOSE 8080

# Run the application
ENTRYPOINT ["./rewards"]

