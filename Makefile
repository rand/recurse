# Recurse Makefile

.PHONY: build test clean python-env python-dev lint

# Default target
all: build

# Build the main binary
build:
	go build -o recurse ./cmd/recurse

# Run all tests
test:
	go test ./...

# Run tests with verbose output
test-v:
	go test -v ./...

# Clean build artifacts
clean:
	rm -f recurse
	rm -rf pkg/python/.venv
	rm -f pkg/python/uv.lock

# Setup Python environment using uv
python-env:
	@echo "Setting up Python environment..."
	@if command -v uv >/dev/null 2>&1; then \
		cd pkg/python && uv sync; \
	else \
		echo "uv not found. Install with: curl -LsSf https://astral.sh/uv/install.sh | sh"; \
		exit 1; \
	fi

# Setup Python environment with dev tools (ruff)
python-dev:
	@echo "Setting up Python dev environment..."
	@if command -v uv >/dev/null 2>&1; then \
		cd pkg/python && uv sync --dev; \
	else \
		echo "uv not found. Install with: curl -LsSf https://astral.sh/uv/install.sh | sh"; \
		exit 1; \
	fi

# Lint Python code with ruff
lint-python:
	@cd pkg/python && .venv/bin/ruff check bootstrap.py

# Format Python code with ruff
fmt-python:
	@cd pkg/python && .venv/bin/ruff format bootstrap.py

# Lint Go code
lint-go:
	golangci-lint run ./...

# Full lint (Go + Python)
lint: lint-go lint-python

# Install uv if not present
install-uv:
	@if ! command -v uv >/dev/null 2>&1; then \
		curl -LsSf https://astral.sh/uv/install.sh | sh; \
	else \
		echo "uv already installed"; \
	fi

# Full setup (Go deps + Python env)
setup: python-env
	go mod download
