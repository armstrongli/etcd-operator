
build:
	./hack/build/docker_build
.PHONY: build

code-gen:
	./hack/k8s/codegen/update-generated.sh
	./hack/k8s/codegen/verify-generated.sh
.PHONY: code-gen

fmt:
	find . -name *.go | grep -v '/vendor' | xargs gofmt -l -w 
.PHONY: fmt

unit-test:
	go test ./... -v -skip */test/e2e/*
.PHONY: unit-test

ut: unit-test
.PHONY: ut