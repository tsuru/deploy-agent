ARG golang_version=1.16
ARG alpine_version=3.15
ARG docker_version=20.10.12
ARG buildkit_version=v0.9.3

FROM golang:${golang_version}-alpine${alpine_version} AS builder
RUN apk add --no-cache --update gcc libc-dev
COPY . /go/src/github.com/tsuru/deploy-agent/
WORKDIR /go/src/github.com/tsuru/deploy-agent/
RUN go build

FROM docker:${docker_version}-alpine${alpine_version} AS docker

FROM moby/buildkit:${buildkit_version}-rootless
COPY --from=docker /usr/local/bin/docker /usr/local/bin/
COPY --from=builder /go/src/github.com/tsuru/deploy-agent/deploy-agent /bin/
# NOTE(nettoclaudio): This piece of code configures the buildctl to not pull container images
# from insecure registries under TLS protocol. It may be needed while developing anything on
# Tsuru that uses containerd as container runtime.
#
# To use it, you should pass a comma-separated string of hosts during build of container image:
#   docker build --build-arg insecure_registries=169.196.254.1:5000,my.insecure.registry:5000 .
ARG insecure_registries
RUN set -ex && \
    mkdir -p /home/user/.config/buildkit && \
    IFS="," && \
    for registry in ${insecure_registries}; do \
      echo -e "[registry.\"${registry}\"]\n  http = true\n" >>  /home/user/.config/buildkit/buildkitd.toml; \
    done
USER 0:0
WORKDIR /
RUN set -ex && \
    apk --update --no-cache add sudo && \
    ln -s /bin/deploy-agent /bin/tsuru_unit_agent
ENTRYPOINT ["./bin/deploy-agent"]
