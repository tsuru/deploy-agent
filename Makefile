TAG=latest
BINARY=deploy-agent
IMAGE=tsuru/$(BINARY)

.PHONY: build-docker push build-docker-gcp push-gcp test integration

build-docker-gcp: build-docker
	docker build --platform linux/amd64 -f Dockerfile.gcp --build-arg BASE_IMAGE=$(IMAGE):$(TAG) -t $(IMAGE):$(TAG)-gcp .

push-gcp: build-docker-gcp
	docker push $(IMAGE):$(TAG)-gcp

build-docker:
	docker build --platform linux/amd64 -t $(IMAGE):$(TAG) .

push: build-docker push-gcp
	docker push $(IMAGE):$(TAG)

test: 
	go test ./... -coverprofile=

integration:
	DEPLOYAGENT_INTEGRATION="true" make test