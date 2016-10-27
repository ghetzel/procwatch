.PHONY: test

all: vendor fmt build

update:
	test -d vendor && rm -rf vendor || exit 0
	glide up --strip-vcs --update-vendored

vendor:
	go list github.com/Masterminds/glide
	glide install --strip-vcs --update-vendored

clean:
	rm -rf vendor bin

fmt:
	gofmt -w .

test:
	# go test -v .
	go test -v ./scanner

build: fmt
	go build -o bin/`basename ${PWD}` cli/*.go

