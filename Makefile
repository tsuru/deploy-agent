TAG=latest
BINARY=deploy-agent
IMAGE=tsuru/$(BINARY)

.PHONY: build-docker push test integration

build-docker:
	docker build --rm -t $(IMAGE):$(TAG) .

push: build-docker
	docker push $(IMAGE):$(TAG)

test: 
	go test ./... -coverprofile=

integration:
	DEPLOYAGENT_INTEGRATION="true" make test