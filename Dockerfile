FROM golang:alpine as builder
RUN apk add gcc libc-dev --update
COPY . /go/src/github.com/tsuru/deploy-agent/
WORKDIR /go/src/github.com/tsuru/deploy-agent/
RUN go build

FROM docker:1.11.2 as docker

FROM moby/buildkit:rootless

WORKDIR /
USER 0:0
RUN apk --no-cache add sudo
COPY --from=docker /usr/local/bin/docker /usr/local/bin/docker
COPY --from=builder /go/src/github.com/tsuru/deploy-agent/deploy-agent /bin/
RUN ln -s /bin/deploy-agent /bin/tsuru_unit_agent
ENTRYPOINT ["./bin/deploy-agent"]
