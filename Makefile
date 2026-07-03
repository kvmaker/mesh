.PHONY: build test clean

build:
	go build -o bin/meshd ./cmd/meshd
	go build -o bin/mesh ./cmd/mesh

test:
	go test ./... -v

clean:
	rm -rf bin/

build-linux:
	GOOS=linux GOARCH=amd64 go build -o bin/meshd-linux-amd64 ./cmd/meshd
	GOOS=linux GOARCH=amd64 go build -o bin/mesh-linux-amd64 ./cmd/mesh

build-darwin:
	GOOS=darwin GOARCH=arm64 go build -o bin/mesh-darwin-arm64 ./cmd/mesh
