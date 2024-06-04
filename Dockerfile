FROM golang:1.22.3 as build

ENV GOOS linux
ENV GOARCH amd64
ENV CGO_ENABLED 0

WORKDIR /app/gossip

COPY cmd ./cmd
COPY go.mod go.sum .

RUN --mount=type=cache,target=./pkg go mod download
RUN go build -ldflags="-s -w" -trimpath -o /app/gossip/bin

FROM gcr.io/distroless/static-debian11:nonroot-amd64

ENV TZ UTC

COPY --from=build /app/gossip/bin /usr/local/bin/gossip

EXPOSE 9055
ENTRYPOINT ["/usr/local/bin/gossip"]
