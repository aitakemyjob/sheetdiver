# SheetDiver build tasks. `make help` lists targets.
#
# Builds go through scripts/build.sh (Linux/macOS) and scripts/build.ps1
# (Windows), the same scripts the release workflow runs.
#
# Linux desktop builds need: sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
WAILS_VERSION := v2.16.0
GOLANGCI_VERSION := v2.5.0
GOLANGCI ?= $(shell go env GOPATH)/bin/golangci-lint
WAILS ?= $(shell go env GOPATH)/bin/wails
ifeq ($(shell uname -s),Linux)
  WAILS_TAGS := -tags webkit2_41
endif

.PHONY: help tools deps frontend test test-go test-frontend test-corpus corpus vet lint vuln build package headless headless-all serve dev icon release clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

tools: ## Install the Wails CLI, golangci-lint and govulncheck
	go install github.com/wailsapp/wails/v2/cmd/wails@$(WAILS_VERSION)
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	go install golang.org/x/vuln/cmd/govulncheck@latest

deps: ## Install frontend dependencies
	cd frontend && npm ci

frontend: ## Build the frontend bundle (frontend/dist)
	cd frontend && npm run build

test: test-go test-frontend ## Run all tests

test-go: ## Go tests with the race detector (headless build tag)
	go test -race -tags nogui ./...

test-frontend: ## Frontend type check and unit tests
	cd frontend && npm run typecheck && npm test

corpus: ## Download the real-datasheet test corpus (testdata/corpus/manifest.json)
	scripts/corpus.sh

test-corpus: ## Check extraction against the downloaded corpus
	go test -tags nogui -run TestCorpus -count=1 -v ./internal/extract/

vet: ## go vet
	go vet -tags nogui ./...

lint: ## golangci-lint (Go) and ESLint (frontend)
	$(GOLANGCI) run ./...
	cd frontend && npm run lint

vuln: ## Known-vulnerability scan: govulncheck and npm audit
	$(shell go env GOPATH)/bin/govulncheck -tags nogui ./...
	cd frontend && npm audit --omit=dev --audit-level=high

build: ## Desktop app for this machine → build/bin
	scripts/build.sh --version $(VERSION)

package: ## Desktop app + release artifacts for this machine → dist/
	scripts/build.sh --version $(VERSION) --package

headless: frontend ## Headless binary for this machine → build/bin/sheetdiver-headless
	go build -tags nogui -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o build/bin/sheetdiver-headless .

headless-all: ## Headless binaries for every OS/arch → dist/
	scripts/build.sh --headless --package --version $(VERSION)

serve: headless ## Run the browser UI against your config
	./build/bin/sheetdiver-headless serve

dev: ## Wails dev mode with hot reload
	$(WAILS) dev $(WAILS_TAGS)

icon: ## Regenerate the app icon and logos (scripts/brand draws the mark)
	go run ./scripts/brand

release: ## Tag and push a release: make release VERSION=v1.2.3
	@echo "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$$' || (echo "VERSION must look like v1.2.3 (got $(VERSION))"; exit 1)
	@test -z "$$(git status --porcelain)" || (echo "working tree is dirty; commit first"; exit 1)
	@test "$$(git rev-parse --abbrev-ref HEAD)" = main || (echo "release from main"; exit 1)
	@! git rev-parse -q --verify "refs/tags/$(VERSION)" >/dev/null || (echo "tag $(VERSION) already exists"; exit 1)
	$(MAKE) test
	git tag -a $(VERSION) -m "SheetDiver $(VERSION)"
	git push origin $(VERSION)
	@echo "Pushed $(VERSION); the Release workflow will publish it."

clean: ## Remove build outputs
	rm -rf build/bin dist frontend/dist/assets frontend/dist/index.html
