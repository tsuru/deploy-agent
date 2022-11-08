GO ?= go
GOLANGCI_LINT ?= golangci-lint
PROTOC ?= protoc
DOCKER ?= docker

INTERNAL_IP ?= 169.196.255.254

.PHONY: all
all: lint test

.PHONY: test
test: generate
	$(GO) test -race ./...

.PHONY: test/integration
test/integration:
	DEPLOY_AGENT_INTEGRATION=true \
		DEPLOY_AGENT_INTEGRATION_REGISTRY_HOST=$(INTERNAL_IP):5000 \
		DEPLOY_AGENT_INTEGRATION_REGISTRY_HTTP=true \
		BUILDKIT_HOST=tcp://0.0.0.0:7777 \
		DOCKER_HOST=tcp://0.0.0.0:2375 \
		$(GO) test -v github.com/tsuru/deploy-agent/pkg/build/buildkit

.PHONY: lint
lint: generate
	$(GOLANGCI_LINT) run ./...

.PHONY: generate
generate:
	$(PROTOC) --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		pkg/build/grpc_build_v1/*.proto

.PHONY: build/container-image
build/container-image:
	$(DOCKER) build -t tsuru/deploy-agent:latest ./
