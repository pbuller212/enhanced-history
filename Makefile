.PHONY: build test vet install clean

VERSION ?= dev

build:
	CGO_ENABLED=0 go build -ldflags="-X main.Version=$(VERSION)" -o hist ./cmd/hist

test:
	go test ./...

vet:
	go vet ./... && staticcheck ./...

install: build
	cp hist $(HOME)/bin/hist

clean:
	rm -f hist
