.PHONY: help run build test clean dev deps

help:
	@echo "Available commands:"
	@echo "  make run    - Run the server"
	@echo "  make build  - Build the server binary"
	@echo "  make test   - Run tests"
	@echo "  make clean  - Clean build artifacts"
	@echo "  make dev    - Run server in development mode with hot reload"
	@echo "  make deps   - Download dependencies"

run:
	go run cmd/server/main.go

build:
	go build -o bin/server cmd/server/main.go

test:
	go test ./...

clean:
	rm -rf bin/

dev:
	@which air > /dev/null 2>&1 || (echo "Installing air..." && go install github.com/air-verse/air@latest)
	@PATH="$${HOME}/go/bin:$${PATH}" air

deps:
	go mod download
	go mod tidy

docker-build:
	docker build -t orkestra-backend:latest .

docker-run:
	docker run -p 3000:3000 --env-file .env orkestra-backend:latest