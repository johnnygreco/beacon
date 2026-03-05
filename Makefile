.PHONY: build run generate clean simulator

build: generate
	go build -o bin/technodrome ./cmd/technodrome

run: generate
	go run ./cmd/technodrome serve

generate:
	templ generate

clean:
	rm -rf bin/

simulator:
	go run ./cmd/simulator
