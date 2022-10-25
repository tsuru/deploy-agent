GO ?= go
PROTOC ?= protoc

.PHONY: test
test: generate
	$(GO) test -race ./...

.PHONY: generate
generate:
	$(PROTOC) --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		api/v1alpha1/*.proto
