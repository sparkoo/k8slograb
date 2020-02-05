### build image
FROM golang:1.13 AS builder
WORKDIR /go/src

# first copy and download go modules to cache this image layer
COPY go.mod /go/src
COPY go.sum /go/src
RUN go mod download

# copy and build actual sources
COPY cmd /go/src/cmd
COPY pkg /go/src/pkg
RUN OOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -x -o /go/bin/k8slograb cmd/k8slograb.go


### runtime image
FROM alpine:3
COPY --from=builder /go/bin/k8slograb .
CMD ["/k8slograb"]
