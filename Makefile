TAG=latest
BINARY=deploy-agent
IMAGE=tsuru/$(BINARY)

DOCKER ?= docker

.PHONY: build-docker push build-docker-gcp push-gcp test integration

build-docker-gcp: build-docker
	$(DOCKER) build --platform linux/amd64 -f Dockerfile.gcp --build-arg BASE_IMAGE=$(IMAGE):$(TAG) -t $(IMAGE):$(TAG)-gcp .

push-gcp: build-docker-gcp
	$(DOCKER) push $(IMAGE):$(TAG)-gcp

build-docker:
	$(DOCKER) build --platform linux/amd64 -t $(IMAGE):$(TAG) .

push: build-docker push-gcp
	$(DOCKER) push $(IMAGE):$(TAG)

test: 
	go test ./... -coverprofile=

integration:
	DEPLOYAGENT_INTEGRATION="true" make test
