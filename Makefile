VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT := $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
GOLANGCI_VERSION := v2.15.0
GOLANGCI_LINT := .tools/golangci-lint-$(GOLANGCI_VERSION)

.PHONY: build fmt fmt-check vet lint test smoke ci tools clean

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/close ./cmd/close

fmt:
	gofmt -w cmd internal

fmt-check:
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)

vet:
	go vet ./...

$(GOLANGCI_LINT):
	@mkdir -p .tools
	GOBIN=$(CURDIR)/.tools go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	@mv .tools/golangci-lint $(GOLANGCI_LINT)

tools: $(GOLANGCI_LINT)

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run

test:
	go test -race -coverpkg=./internal/... -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | awk '/^total:/ { found=1; p=$$3+0; print "Internal coverage: " $$3; if (p<80) exit 1 } END { if (!found) exit 1 }'

smoke: build
	python3 scripts/smoke-test.py ./bin/close

ci: fmt-check vet lint test smoke

clean:
	go clean
	rm -f bin/close coverage.out
