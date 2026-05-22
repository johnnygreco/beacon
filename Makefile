.DEFAULT_GOAL := help
.PHONY: help build run generate clean clean-local clean-deps simulator publish test test-race test-cover perf-bench fmt fmt-check lint

help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: generate ## Build the beacon binary
	go build -o bin/beacon ./cmd/beacon

run: generate ## Run the beacon server
	go run ./cmd/beacon up

generate: ## Generate templ templates
	go tool templ generate

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
	go test ./...

test-race: generate ## Run tests with race detector
	go test -race ./...

test-cover: generate ## Run tests with coverage report and threshold checks
	go test -race -coverprofile=coverage.txt ./...
	go tool cover -func=coverage.txt | tail -1
	./scripts/check-coverage.sh coverage.txt

perf-bench: ## Run perf benchmarks (PERF_SIZE=small|medium|large)
	@PERF_SIZE=$${PERF_SIZE:-medium} && \
	echo "=== Beacon Perf Bench (size=$$PERF_SIZE, rev=$$(git rev-parse --short HEAD)) ===" && \
	PERF_SIZE=$$PERF_SIZE go test -bench=. -benchmem -count=1 -timeout=10m ./internal/perf/

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
	golangci-lint run ./...

publish: ## Publish a release (usage: make publish VERSION=x.y.z)
	./scripts/publish.sh $(VERSION)
