TAG=latest
BINARY=deploy-agent
IMAGE=tsuru/$(BINARY)

.PHONY: build-docker push test

build-docker:
	docker build --rm -t $(IMAGE):$(TAG) .

push: build-docker
	docker push $(IMAGE):$(TAG)

test: 
	go test ./... -coverprofile=
