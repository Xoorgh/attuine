.PHONY: build test lint clean install man

build:
	go build -o bin/attuine ./cmd/attuine

test:
	go test ./... -v

lint:
	go vet ./...

clean:
	rm -rf bin/ doc/man/

man: build
	mkdir -p doc/man
	./bin/attuine man doc/man/

install: build
	go install ./cmd/attuine
