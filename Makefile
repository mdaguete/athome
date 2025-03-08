.PHONY: all clean build run test frontend-build frontend-dev backend-build backend-run dev container container-run container-stop

# Default target
all: build

# Clean build artifacts
clean:
	rm -rf public/*
	rm -f athome

# Build frontend and backend
build: frontend-build backend-build

# Run the complete application
run: build
	./athome

# Build frontend
frontend-build:
	cd frontend && npm run build
	mkdir -p public
	cp -r frontend/dist/* public/

# Run frontend in development mode
frontend-dev:
	cd frontend && npm run dev

# Build backend
backend-build:
	go build -o athome .

# Run backend only
backend-run: backend-build
	./athome

# Development mode - run frontend and backend concurrently
dev:
	@echo "Starting development servers..."
	@(cd frontend && npm run dev) & \
	(go run . ) & \
	wait

# Test the application
test:
	go test ./...
	cd frontend && npm test

# Install dependencies
deps:
	cd frontend && npm install
	go mod download

# Build container image
container:
	podman build -t athome:latest .

# Run container
container-run: container
	podman run -d \
		-p 8200:8200 \
		-e ATHOME_VALID_HANDLES="" \
		--name athome \
		athome:latest

# Stop and remove container
container-stop:
	-podman stop athome
	-podman rm athome

# Show help
help:
	@echo "Available targets:"
	@echo "  all           - Build everything (default)"
	@echo "  clean         - Remove build artifacts"
	@echo "  build         - Build frontend and backend"
	@echo "  run           - Build and run the complete application"
	@echo "  frontend-build- Build frontend only"
	@echo "  frontend-dev  - Run frontend in development mode"
	@echo "  backend-build - Build backend only"
	@echo "  backend-run   - Build and run backend only"
	@echo "  dev           - Run both frontend and backend in development mode"
	@echo "  test          - Run all tests"
	@echo "  deps          - Install all dependencies"
	@echo "  container     - Build container image using podman"
	@echo "  container-run - Run container with podman"
	@echo "  container-stop- Stop and remove running container"
	@echo "  help          - Show this help message"
