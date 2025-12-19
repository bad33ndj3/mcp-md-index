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
	rm -rf .mcp-md-index-cache

# Install dependencies
deps:
	go mod download

# Format code
fmt:
	go fmt ./...

install:
	go install .

# Follow today's debug log
logs:
	tail -f .mcp-md-index-cache/debug-$$(date +%Y-%m-%d).txt

# Run benchmarks with memory stats
bench:
	go test -bench=. -benchmem ./...

# Run benchmarks and save to file for comparison
bench-save:
	go test -bench=. -benchmem ./... | tee bench.txt

# Compare benchmarks (requires benchstat: go install golang.org/x/perf/cmd/benchstat@latest)
bench-compare:
	@if [ -f bench_before.txt ] && [ -f bench_after.txt ]; then \
		benchstat bench_before.txt bench_after.txt; \
	else \
		echo "Run 'make bench-save' before and after changes, saving to bench_before.txt and bench_after.txt"; \
	fi