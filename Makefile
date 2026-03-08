.DEFAULT_GOAL := help
.PHONY: help build run generate clean simulator publish

help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: generate ## Build the beacon binary
	go build -o bin/beacon ./cmd/beacon

run: generate ## Run the beacon server
	go run ./cmd/beacon serve

generate: ## Generate templ templates
	go tool templ generate

clean: ## Remove build artifacts
	rm -rf bin/ dist/

simulator: ## Build the simulator binary
	go build -o bin/simulator ./cmd/simulator

publish: ## Publish a release (usage: make publish VERSION=x.y.z)
	./scripts/publish.sh $(VERSION)
