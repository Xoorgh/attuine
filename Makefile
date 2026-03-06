.PHONY: build test lint clean

build:
	go build -o bin/attuine ./cmd/attuine

test:
	go test ./... -v

lint:
	go vet ./...

clean:
	rm -rf bin/
