.PHONY: test deps

all: fmt deps build

deps:
	@go list github.com/mjibson/esc || go get github.com/mjibson/esc/...
	@go list golang.org/x/tools/cmd/goimports || go get golang.org/x/tools/cmd/goimports
	go generate -x
	go get .

clean:
	rm -rf bin

fmt:
	goimports -w .

test:
	go test -race -v ./

build: fmt
	go build -o bin/`basename ${PWD}` cli/main.go
	go build -o bin/procwatch-tester cli/tester.go


packages:
	-rm -rf pkg
	mkdir -p pkg/usr/bin
	cp bin/procwatch pkg/usr/bin/
	-fpm -s dir -t deb -n procwatch -v "`./bin/procwatch -v | cut -d' ' -f3`" -C pkg usr
	-fpm -s dir -t rpm -n procwatch -v "`./bin/procwatch -v | cut -d' ' -f3`" -C pkg usr
	-cd pkg && tar czvf "../procwatch-`../bin/procwatch -v | cut -d' ' -f3`.tar.gz" usr
