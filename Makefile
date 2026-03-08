.PHONY: build test lint clean install man

PREFIX ?= /usr/local

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

install: build man
	install -Dm755 bin/attuine $(DESTDIR)$(PREFIX)/bin/attuine
	install -Dm644 doc/man/attuine.1 $(DESTDIR)$(PREFIX)/share/man/man1/attuine.1
