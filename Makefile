BINARY ?= yolobox
CMD_DIR := ./cmd/yolobox
IMAGE ?= ghcr.io/finbarr/yolobox:latest
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

.PHONY: build test lint image smoke-test install uninstall clean

build:
	go build $(LDFLAGS) -o $(BINARY) $(CMD_DIR)

test:
	go test -v ./...

lint:
	go vet ./...
	@which golangci-lint > /dev/null && golangci-lint run || echo "golangci-lint not installed, skipping"

image:
	@docker buildx version >/dev/null 2>&1 && \
		docker buildx build -t $(IMAGE) . || \
		docker build -t $(IMAGE) .

SMOKE_TOOLS := node bun python3 uv gh fd bat rg eza

smoke-test: build
	@echo "Running smoke tests..."
	@failed=0; \
	for tool in $(SMOKE_TOOLS); do \
		if ./$(BINARY) run --scratch $$tool --version >/dev/null 2>&1; then \
			echo "  ✓ $$tool"; \
		else \
			echo "  ✗ $$tool"; \
			failed=1; \
		fi; \
	done; \
	if ./$(BINARY) run --scratch go version >/dev/null 2>&1; then \
		echo "  ✓ go"; \
	else \
		echo "  ✗ go"; \
		failed=1; \
	fi; \
	if ./$(BINARY) run --scratch claude --version >/dev/null 2>&1; then \
		echo "  ✓ claude"; \
	else \
		echo "  ✗ claude"; \
		failed=1; \
	fi; \
	if ./$(BINARY) run --scratch codex --version >/dev/null 2>&1; then \
		echo "  ✓ codex"; \
	else \
		echo "  ✗ codex"; \
		failed=1; \
	fi; \
	if ./$(BINARY) run --scratch pi --version >/dev/null 2>&1; then \
		echo "  ✓ pi"; \
	else \
		echo "  ✗ pi"; \
		failed=1; \
	fi; \
	IMG_VER=$$(./$(BINARY) run --scratch /usr/local/bin/claude --version 2>/dev/null | head -1); \
	RUN_VER=$$(./$(BINARY) run claude --version 2>/dev/null | head -1); \
	if [ "$$IMG_VER" = "$$RUN_VER" ]; then \
		echo "  ✓ claude version pinned ($$RUN_VER)"; \
	else \
		echo "  ✗ claude version mismatch: image=$$IMG_VER, got=$$RUN_VER"; \
		failed=1; \
	fi; \
	IMG_VER=$$(./$(BINARY) run --scratch env NO_YOLO=1 codex --version 2>/dev/null | head -1); \
	RUN_VER=$$(./$(BINARY) run --scratch codex --version 2>/dev/null | head -1); \
	if [ "$$IMG_VER" = "$$RUN_VER" ]; then \
		echo "  ✓ codex wrapper matches real binary ($$RUN_VER)"; \
	else \
		echo "  ✗ codex version mismatch: real=$$IMG_VER, wrapper=$$RUN_VER"; \
		failed=1; \
	fi; \
	[ $$failed -eq 0 ]
	@echo "Smoke tests passed!"

install: build
	mkdir -p $(BINDIR)
	install -m 0755 $(BINARY) $(BINDIR)/$(BINARY)
	@echo "Installed $(BINARY) to $(BINDIR)/$(BINARY)"

uninstall:
	rm -f $(BINDIR)/$(BINARY)
	@echo "Removed $(BINDIR)/$(BINARY)"

clean:
	rm -f $(BINARY)
