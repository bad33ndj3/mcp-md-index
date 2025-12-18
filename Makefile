.PHONY: build test lint clean

# Build the MCP server binary
build:
	go build -o mcp-md-index .

# Run all tests with verbose output
test:
	go test -v ./...

# Run tests with coverage
test-coverage:
	go test -cover ./...

# Run go vet for static analysis
lint:
	go vet ./...

# Clean build artifacts and cache
clean:
	rm -f mcp-md-index
	rm -rf .mcp-mdx-cache

# Install dependencies
deps:
	go mod download

# Format code
fmt:
	go fmt ./...
