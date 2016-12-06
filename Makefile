.PHONY: test vendor

all: vendor fmt build

update:
		-rm -rf vendor
		govend -u -v -l

vendor:
		go list github.com/govend/govend
		echo 'Verifying dependencies:'
		govend -v

clean:
		rm -rf vendor bin

fmt:
	gofmt -w .

test:
	go test -v ./

build: fmt
	go build -o bin/`basename ${PWD}` cli/*.go

