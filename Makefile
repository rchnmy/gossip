BIN := ./gossip

.PHONY: *

all: build

build: export GOOS = linux
build: export GOARCH = amd64
build: export CGO_ENABLED = 0
build:
	go build -ldflags="-s -w" -trimpath -o $(BIN) ./cmd/main.go

clean:
	rm -f $(BIN)
