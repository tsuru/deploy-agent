GO ?= go
GOLANGCI_LINT ?= golangci-lint
PROTOC ?= protoc
DOCKER ?= docker

# check if user uses docker-compose standalone or compose plugin
DOCKER_COMPOSE := $(shell if $(DOCKER) compose version > /dev/null 2>&1; then echo "$(DOCKER) compose"; else echo "docker-compose"; fi)

INTERNAL_IP ?= 169.196.255.254

LOCAL_DEV ?= ./misc/local-dev.sh

.PHONY: setup
setup:
	@$(LOCAL_DEV) setup-loopback $(TSURU_HOST_IP)
	@$(DOCKER_COMPOSE) up -d

.PHONY: cleanup
cleanup:
	@$(DOCKER_COMPOSE) down
	@$(LOCAL_DEV) cleanup-loopback $(TSURU_HOST_IP)

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
