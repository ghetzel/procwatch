.PHONY: test deps

all: fmt deps build

deps:
	@go list github.com/mjibson/esc || go get github.com/mjibson/esc/...
	@go list golang.org/x/tools/cmd/goimports || go get golang.org/x/tools/cmd/goimports
	go generate -x
	go get .

clean:
	-rm -rf bin
	-rm *.rpm *.tar.gz *.deb

fmt:
	goimports -w .

test:
	go build -o bin/procwatch-tester tests/tester.go
	go test .

build: fmt
	go build -o bin/`basename ${PWD}` procwatch/main.go procwatch/dashboard.go

packages: fmt deps build test
	-rm -rf pkg *.deb *.rpm *.tar.gz
	mkdir -p pkg/usr/bin
	cp bin/procwatch pkg/usr/bin/
	-fpm -s dir -t deb -n procwatch -v "`./bin/procwatch -v | cut -d' ' -f3`" -C pkg usr
	-fpm -s dir -t rpm -n procwatch -v "`./bin/procwatch -v | cut -d' ' -f3`" -C pkg usr
	cd bin && tar czvf "../procwatch-`../bin/procwatch -v | cut -d' ' -f3`.tar.gz" procwatch
