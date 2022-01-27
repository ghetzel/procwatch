.PHONY: test deps
.EXPORT_ALL_VARIABLES:

GO111MODULE     ?= on
CGO_ENABLED      = 0
LOCALS          := $(shell find . -type f -name '*.go' 2> /dev/null)

all: deps fmt build

deps:
	go get ./...

clean:
	-rm -rf bin
	-rm *.rpm *.tar.gz *.deb

fmt:
	go mod tidy
	go generate ./...
	gofmt -w $(LOCALS)

test:
	go build -tags nocgo -o bin/procwatch-tester tests/tester.go
	go test ./...

build:
	go build -tags nocgo -o bin/procwatch cmd/procwatch/*.go

packages: fmt deps build test
	-rm -rf pkg *.deb *.rpm *.tar.gz
	mkdir -p pkg/usr/bin
	cp bin/procwatch pkg/usr/bin/
	-fpm -s dir -t deb -n procwatch -v "`./bin/procwatch -v | cut -d' ' -f3`" -C pkg usr
	-fpm -s dir -t rpm -n procwatch -v "`./bin/procwatch -v | cut -d' ' -f3`" -C pkg usr
	cd bin && tar czvf "../procwatch-`../bin/procwatch -v | cut -d' ' -f3`.tar.gz" procwatch
