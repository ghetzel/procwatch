.PHONY: test deps

all: fmt deps build

deps:
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

