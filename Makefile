BINARY ?= yolobox
CMD_DIR := ./cmd/yolobox
IMAGE ?= yolobox/base:latest
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

.PHONY: build test lint image install uninstall clean

build:
	go build $(LDFLAGS) -o $(BINARY) $(CMD_DIR)

test:
	go test -v ./...

lint:
	go vet ./...
	@which golangci-lint > /dev/null && golangci-lint run || echo "golangci-lint not installed, skipping"

image:
	docker build -t $(IMAGE) .

install: build
	mkdir -p $(BINDIR)
	install -m 0755 $(BINARY) $(BINDIR)/$(BINARY)
	@echo "Installed $(BINARY) to $(BINDIR)/$(BINARY)"

uninstall:
	rm -f $(BINDIR)/$(BINARY)
	@echo "Removed $(BINDIR)/$(BINARY)"

clean:
	rm -f $(BINARY)
