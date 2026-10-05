BINARY ?= yolobox
CMD_DIR := ./cmd/yolobox
IMAGE ?= ghcr.io/finbarr/yolobox:latest
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

.PHONY: build test lint image smoke-test smoke-acp install uninstall clean

build:
	go build $(LDFLAGS) -o $(BINARY) $(CMD_DIR)

test:
	go test -v ./...

lint:
	go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run; else echo "golangci-lint not installed, skipping"; fi

image:
	@docker buildx version >/dev/null 2>&1 && \
		docker buildx build -t $(IMAGE) . || \
		docker build -t $(IMAGE) .

SMOKE_ARGS ?= --image $(IMAGE)
SMOKE_TOOLS := node bun python3 uv gh fd bat rg eza

smoke-test: build
	@echo "Running smoke tests..."
	@failed=0; \
	for tool in $(SMOKE_TOOLS); do \
		if ./$(BINARY) run $(SMOKE_ARGS) --scratch $$tool --version >/dev/null 2>&1; then \
			echo "  ✓ $$tool"; \
		else \
			echo "  ✗ $$tool"; \
			failed=1; \
		fi; \
	done; \
	if ./$(BINARY) run $(SMOKE_ARGS) --scratch go version >/dev/null 2>&1; then \
		echo "  ✓ go"; \
	else \
		echo "  ✗ go"; \
		failed=1; \
	fi; \
	if ./$(BINARY) run $(SMOKE_ARGS) --scratch claude --version >/dev/null 2>&1; then \
		echo "  ✓ claude"; \
	else \
		echo "  ✗ claude"; \
		failed=1; \
	fi; \
	if ./$(BINARY) run $(SMOKE_ARGS) --scratch codex --version >/dev/null 2>&1; then \
		echo "  ✓ codex"; \
	else \
		echo "  ✗ codex"; \
		failed=1; \
	fi; \
	if ./$(BINARY) run $(SMOKE_ARGS) --scratch kimi --version >/dev/null 2>&1; then \
		echo "  ✓ kimi"; \
	else \
		echo "  ✗ kimi"; \
		failed=1; \
	fi; \
	if ./$(BINARY) run $(SMOKE_ARGS) --scratch pi --version >/dev/null 2>&1; then \
		echo "  ✓ pi"; \
	else \
		echo "  ✗ pi"; \
		failed=1; \
	fi; \
	if ./$(BINARY) run $(SMOKE_ARGS) --scratch agy --version >/dev/null 2>&1; then \
		echo "  ✓ agy"; \
	else \
		echo "  ✗ agy"; \
		failed=1; \
	fi; \
	REAL_VER=$$(./$(BINARY) run $(SMOKE_ARGS) --scratch env NO_YOLO=1 claude --version 2>/dev/null | head -1); \
	WRAP_VER=$$(./$(BINARY) run $(SMOKE_ARGS) --scratch claude --version 2>/dev/null | head -1); \
	if [ "$$REAL_VER" = "$$WRAP_VER" ]; then \
		echo "  ✓ claude wrapper matches real binary ($$WRAP_VER)"; \
	else \
		echo "  ✗ claude version mismatch: real=$$REAL_VER, wrapper=$$WRAP_VER"; \
		failed=1; \
	fi; \
	IMG_VER=$$(./$(BINARY) run $(SMOKE_ARGS) --scratch env NO_YOLO=1 codex --version 2>/dev/null | head -1); \
	RUN_VER=$$(./$(BINARY) run $(SMOKE_ARGS) --scratch codex --version 2>/dev/null | head -1); \
	if [ "$$IMG_VER" = "$$RUN_VER" ]; then \
		echo "  ✓ codex wrapper matches real binary ($$RUN_VER)"; \
	else \
		echo "  ✗ codex version mismatch: real=$$IMG_VER, wrapper=$$RUN_VER"; \
		failed=1; \
	fi; \
	IMG_VER=$$(./$(BINARY) run $(SMOKE_ARGS) --scratch env NO_YOLO=1 kimi --version 2>/dev/null | head -1); \
	RUN_VER=$$(./$(BINARY) run $(SMOKE_ARGS) --scratch kimi --version 2>/dev/null | head -1); \
	if [ "$$IMG_VER" = "$$RUN_VER" ]; then \
		echo "  ✓ kimi wrapper matches real binary ($$RUN_VER)"; \
	else \
		echo "  ✗ kimi version mismatch: real=$$IMG_VER, wrapper=$$RUN_VER"; \
		failed=1; \
	fi; \
	[ $$failed -eq 0 ]
	python3 scripts/smoke-acp.py --yolobox ./$(BINARY) -- $(SMOKE_ARGS)
	@echo "Smoke tests passed!"

smoke-acp: build
	python3 scripts/smoke-acp.py --yolobox ./$(BINARY) -- $(SMOKE_ARGS)

install: build
	mkdir -p $(BINDIR)
	install -m 0755 $(BINARY) $(BINDIR)/$(BINARY)
	@echo "Installed $(BINARY) to $(BINDIR)/$(BINARY)"

uninstall:
	rm -f $(BINDIR)/$(BINARY)
	@echo "Removed $(BINDIR)/$(BINARY)"

clean:
	rm -f $(BINARY)
