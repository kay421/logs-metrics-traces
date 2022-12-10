FROM golang:1.19-alpine as builder

ENV deps "git"

RUN apk update && apk upgrade
RUN apk add --no-cache $deps

ENV CGO_ENABLED=0

WORKDIR /build/

COPY go.mod go.sum /build/
RUN go mod download
RUN apk del --purge $deps

COPY main.go /build/main.go
COPY pkg /build/pkg
RUN go build -trimpath -o /usr/local/bin/main -ldflags="-s -w" /build/main.go

FROM gcr.io/distroless/base
COPY --from=builder /usr/local/bin/main /usr/local/bin/main

ENTRYPOINT ["/usr/local/bin/main"]
