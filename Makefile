VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/shortdesk/shortdesk/internal/cli.Version=$(VERSION)
DESK_IMAGE ?= shortdesk/desk:dev

.PHONY: all web build test vet fmt check image desk-binary clean

all: web build ## Dashboard + binary (what releases are built with)

web: ## Build the dashboard and the phone, and bundle them into the binary's sources
	cd web && npm ci --no-audit --no-fund && npm run build
	find internal/webui/dist -mindepth 1 -not -name README.md -exec rm -rf {} +
	cp -R web/out/. internal/webui/dist/
	cd web && SHORTDESK_BUILD=phone npm run build
	find internal/phoneweb/dist -mindepth 1 -not -name README.md -exec rm -rf {} +
	cp -R web/out/. internal/phoneweb/dist/

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

DESK_ARCH ?= $(shell docker version -f '{{.Server.Arch}}' 2>/dev/null || go env GOARCH)

desk-binary: ## Linux shortdesk for the desk image (the main desk runs the Phone Desk with it)
	CGO_ENABLED=0 GOOS=linux GOARCH=$(DESK_ARCH) go build -trimpath -ldflags "-s -w $(LDFLAGS)" \
		-o images/desk/rootfs/usr/local/bin/shortdesk ./cmd/shortdesk

image: desk-binary ## Build the desk image
	docker build -t $(DESK_IMAGE) images/desk

clean:
	rm -rf bin
