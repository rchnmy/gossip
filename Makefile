REP=github.com/rchnmy/gossip
APP=gossip
VER=1.2.0

.PHONY: *

all: build

init:
	@go mod init $(REP)
	@go mod tidy

update:
	@go get -u ./...

build: export GOOS=linux
build: export GOARCH=amd64
build: export CGO_ENABLED=0
build:
	@go build -trimpath -ldflags="-s -w \
		-X $(REP)/log.ver=$(VER)" \
		-o $(APP) ./cmd

clean:
	@-go clean
	@-rm -rf go.mod go.sum $(APP)
