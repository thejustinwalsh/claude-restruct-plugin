BINARY := restruct
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X github.com/tjw/restruct/cmd.Version=$(VERSION)"

.PHONY: build install test lint clean

build:
	go build $(LDFLAGS) -o $(BINARY) .

install:
	go install $(LDFLAGS) .

test:
	go test ./...

lint:
	golangci-lint run

clean:
	rm -f $(BINARY)
