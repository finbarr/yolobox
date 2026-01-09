# Contributing to yolobox

Thanks for your interest in contributing!

## Getting Started

```bash
git clone https://github.com/finbarr/yolobox.git
cd yolobox
make build
make test
```

## Requirements

- Go 1.22+
- Docker or Podman (for testing the container)

## Development

```bash
make build    # Build binary
make test     # Run tests
make lint     # Run linters
make image    # Build Docker image
```

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Keep the single-file design—don't split into packages unless absolutely necessary
- Add tests for new functionality

## Pull Requests

1. Fork the repo and create a branch
2. Make your changes
3. Run `make test` and `make lint`
4. Submit a PR with a clear description

## Reporting Issues

Include:
- OS and version
- Container runtime (Docker/Podman) and version
- Steps to reproduce
- Expected vs actual behavior
