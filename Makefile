.PHONY: build run generate clean simulator

build: generate
	go build -o bin/technodrome ./cmd/technodrome

run: generate
	go run ./cmd/technodrome serve

generate:
	templ generate

clean:
	rm -rf bin/
	rm -f technodrome.duckdb technodrome.duckdb.wal

simulator:
	go run ./cmd/simulator
