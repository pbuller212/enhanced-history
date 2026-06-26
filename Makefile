.PHONY: build test vet install clean

build:
	CGO_ENABLED=0 go build -o hist ./cmd/hist

test:
	go test ./...

vet:
	go vet ./... && staticcheck ./...

install: build
	cp hist $(HOME)/bin/hist

clean:
	rm -f hist
