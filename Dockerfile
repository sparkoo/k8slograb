FROM golang:1.13 AS builder
WORKDIR /go/src
COPY cmd /go/src/cmd
COPY pkg /go/src/pkg
COPY go.mod /go/src
COPY go.sum /go/src
RUN OOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /go/bin/k8slograb cmd/k8slograb.go

FROM alpine:3
COPY --from=builder /go/bin/k8slograb .
CMD ["/k8slograb"]
