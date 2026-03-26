# Root Makefile — orchestrates cli build + plugin packaging

.PHONY: build install test lint clean dev-plugin

# Build the CLI binary for the current platform into plugin/bin/
build:
	cd cli && go build -o ../plugin/bin/restruct .

# Install the CLI to $GOPATH/bin
install:
	cd cli && go install .

# Run all tests
test:
	cd cli && go test ./...

# Lint
lint:
	cd cli && golangci-lint run

# Build and symlink for local plugin development
dev-plugin: build
	@echo "Plugin ready at ./plugin"
	@echo "Test with: claude --plugin-dir ./plugin"

clean:
	rm -f plugin/bin/restruct plugin/bin/restruct-*
	cd cli && go clean
