VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/shortdesk/shortdesk/internal/cli.Version=$(VERSION)
DESK_IMAGE ?= shortdesk/desk:dev

.PHONY: all web build test vet fmt check image clean

all: web build ## Dashboard + binary (what releases are built with)

web: ## Build the dashboard and bundle it into the binary's sources
	cd web && npm ci --no-audit --no-fund && npm run build
	find internal/webui/dist -mindepth 1 -not -name README.md -exec rm -rf {} +
	cp -R web/out/. internal/webui/dist/

build: ## Build the shortdesk binary into bin/ (bundles whatever `make web` produced)
	go build -ldflags "$(LDFLAGS)" -o bin/shortdesk ./cmd/shortdesk

test: ## Run unit tests
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

check: vet test ## Everything CI should run
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)

image: ## Build the desk image
	docker build -t $(DESK_IMAGE) images/desk

clean:
	rm -rf bin
