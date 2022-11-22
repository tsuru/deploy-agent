ARG go_version=1.19
ARG alpine_version=3.16

FROM golang:${go_version}-alpine${alpine_version} AS build-stage
COPY ./ /go/src/github.com/tsuru/deploy-agent
WORKDIR /go/src/github.com/tsuru/deploy-agent
ARG deploy_agent_version=v2
RUN set -x \
    && go build -o ./bin/deploy-agent ./main.go

FROM alpine:${alpine_version}
COPY --from=build-stage /go/src/github.com/tsuru/deploy-agent/bin/deploy-agent /usr/local/bin/
ARG docker_credential_gcr_version=2.1.6
ARG grpc_health_probe_version=0.4.14
RUN set -ex \
    && apk add --no-cache --update curl tar \
    && curl -fsSL "https://github.com/GoogleCloudPlatform/docker-credential-gcr/releases/download/v${docker_credential_gcr_version}/docker-credential-gcr_linux_amd64-${docker_credential_gcr_version}.tar.gz" \
       | tar -xzf- docker-credential-gcr \
    && mv docker-credential-gcr /usr/local/bin/ \
    && docker-credential-gcr version \
    && docker-credential-gcr configure-docker --include-artifact-registry \
    && curl -fsSL -o /usr/local/bin/grpc_health_probe "https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/v${grpc_health_probe_version}/grpc_health_probe-linux-amd64" \
    && chmod +x /usr/local/bin/grpc_health_probe
EXPOSE 8080
ENTRYPOINT ["deploy-agent"]
