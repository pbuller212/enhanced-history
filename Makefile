.PHONY: build test vet install

build:
	go build -o hist ./cmd/hist

test:
	go test ./...

vet:
	go vet ./...

install:
	go build -o $(HOME)/bin/hist ./cmd/hist
