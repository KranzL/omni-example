.PHONY: build test vet fmt-check run

build:
	go build -o bin/harness ./cmd/harness

test:
	go test -race ./...

vet:
	go vet ./...

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

ARGS ?= db-check

run:
	go run ./cmd/harness $(ARGS)
