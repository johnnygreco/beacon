.PHONY: build run generate clean simulator

build: generate
	go build -o bin/beacon ./cmd/beacon

run: generate
	go run ./cmd/beacon serve

generate:
	go tool templ generate

clean:
	rm -rf bin/

simulator:
	go build -o bin/simulator ./cmd/simulator
