FROM golang:1.22.5 AS build

ENV GOOS=linux
ENV GOARCH=amd64
ENV CGO_ENABLED=0

WORKDIR /app/gossip

COPY cmd ./cmd
COPY log ./log
COPY proxy ./proxy
COPY ./go.mod ./go.sum ./

RUN go build -trimpath -ldflags="-s -w \
    -X github.com/rchnmy/gossip/log.ver=1.2.0" \
    -o /app/gossip/bin ./cmd

FROM gcr.io/distroless/static-debian11:nonroot-amd64

ENV TZ=UTC

COPY --from=build /app/gossip/bin /usr/local/bin/gossip

EXPOSE 9055
ENTRYPOINT ["/usr/local/bin/gossip"]
