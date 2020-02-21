FROM golang:1.13-alpine as builder
RUN apk add gcc libc-dev --update
COPY . /go/src/github.com/tsuru/deploy-agent/
WORKDIR /go/src/github.com/tsuru/deploy-agent/
RUN go build

FROM docker:1.11.2

WORKDIR /
COPY --from=builder /go/src/github.com/tsuru/deploy-agent/deploy-agent /bin/
RUN ln -s /bin/deploy-agent /bin/tsuru_unit_agent
CMD ["./bin/deploy-agent"]
