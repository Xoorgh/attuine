.PHONY: build test lint clean install uninstall man

PREFIX ?= /usr/local
BINARY := attuine

build:
	go build -o bin/$(BINARY) ./cmd/attuine

test:
	go test ./... -v

lint:
	go vet ./...

clean:
	rm -rf bin/ doc/man/

man: build
	mkdir -p doc/man
	./bin/$(BINARY) man doc/man/

install: bin/$(BINARY) doc/man/attuine.1
	mkdir -p $(DESTDIR)$(PREFIX)/bin
	mkdir -p $(DESTDIR)$(PREFIX)/share/man/man1
	install -m755 bin/$(BINARY) $(DESTDIR)$(PREFIX)/bin/$(BINARY)
	install -m644 doc/man/attuine.1 $(DESTDIR)$(PREFIX)/share/man/man1/attuine.1

uninstall:
	rm -f $(DESTDIR)$(PREFIX)/bin/$(BINARY)
	rm -f $(DESTDIR)$(PREFIX)/share/man/man1/attuine.1
