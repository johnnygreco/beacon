.DEFAULT_GOAL := help
.PHONY: help build install-local run generate generate-check clean clean-local clean-deps simulator publish test test-race test-cover perf-bench perf-explain fmt fmt-check lint

GO_PACKAGE_DIRS = $(shell go list -f '{{.Dir}}' ./... | grep -v '/node_modules/')
GO_PACKAGES = $(patsubst $(CURDIR)%,.%,$(GO_PACKAGE_DIRS))
INSTALL_DIR ?= $(HOME)/.local/bin

help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: generate ## Build the beacon binary
	go build -o bin/beacon ./cmd/beacon

install-local: generate ## Install local dev beacon globally (INSTALL_DIR=~/.local/bin)
	mkdir -p "$(INSTALL_DIR)"
	go build -o "$(INSTALL_DIR)/beacon" ./cmd/beacon

run: generate ## Run the beacon server
	go run ./cmd/beacon up

generate: ## Generate templ templates
	go tool templ generate

generate-check: ## Verify templ generated files are current
	go tool templ generate
	@drift=$$(git status --porcelain -- ':(glob)**/*_templ.go'); \
	if [ -n "$$drift" ]; then \
		echo "go tool templ generate left uncommitted changes:"; \
		echo "$$drift"; \
		git diff --stat -- ':(glob)**/*_templ.go'; \
		git diff --exit-code -- ':(glob)**/*_templ.go' || true; \
		exit 1; \
	fi

clean: ## Remove build and test artifacts
	rm -rf bin/ dist/ beacon simulator coverage.txt cover.html playwright-report/ test-results/

clean-local: clean ## Remove repo-local scratch environments and local databases
	rm -rf .scratch/ .venv/ .ralphkit/ .claude/ .kraang/ plans/
	rm -f .mcp.json plan.md task*.md
	rm -f ./*.duckdb ./*.duckdb.wal

clean-deps: ## Remove dependency installs
	rm -rf node_modules/

simulator: ## Build the simulator binary
	go build -o bin/simulator ./cmd/simulator

test: generate ## Run all tests
	go test $(GO_PACKAGES)

test-race: generate ## Run tests with race detector
	go test -race $(GO_PACKAGES)

test-cover: generate ## Run tests with coverage report and threshold checks
	go test -race -coverprofile=coverage.txt $(GO_PACKAGES)
	go tool cover -func=coverage.txt | tail -1
	./scripts/check-coverage.sh coverage.txt

perf-bench: ## Run perf benchmarks (PERF_SIZE=small|medium|large)
	@PERF_SIZE=$${PERF_SIZE:-medium} && \
	PERF_BENCH=$${PERF_BENCH:-.} && \
	PERF_BENCHTIME=$${PERF_BENCHTIME:-1s} && \
	PERF_COUNT=$${PERF_COUNT:-1} && \
	echo "=== Beacon Perf Bench (size=$$PERF_SIZE, bench=$$PERF_BENCH, benchtime=$$PERF_BENCHTIME, count=$$PERF_COUNT, rev=$$(git rev-parse --short HEAD)) ===" && \
	PERF_SIZE=$$PERF_SIZE go test -bench=$$PERF_BENCH -benchtime=$$PERF_BENCHTIME -benchmem -count=$$PERF_COUNT -timeout=10m ./internal/perf/

perf-explain: ## Print ClickHouse plans for representative perf queries
	@PERF_SIZE=$${PERF_SIZE:-medium} && \
	echo "=== Beacon Perf Explain (size=$$PERF_SIZE, rev=$$(git rev-parse --short HEAD)) ===" && \
	BEACON_PERF_EXPLAIN=1 PERF_SIZE=$$PERF_SIZE go test -run TestExplainQueryPlans -count=1 -timeout=10m -v ./internal/perf/

fmt: ## Format tracked Go files
	git ls-files '*.go' | xargs gofmt -w

fmt-check: ## Check tracked Go files are gofmt formatted
	@drift=$$(git ls-files '*.go' | xargs gofmt -l); \
	if [ -n "$$drift" ]; then \
		echo "gofmt drift detected:"; \
		echo "$$drift"; \
		echo "Run 'make fmt'."; \
		exit 1; \
	fi

lint: ## Run linter
	golangci-lint run $(GO_PACKAGES)

publish: ## Publish a release (usage: make publish VERSION=x.y.z)
	./scripts/publish.sh $(VERSION)
