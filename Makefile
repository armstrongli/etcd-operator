
build:
	./hack/build/docker_build
.PHONY: build

code-gen:
	./hack/k8s/codegen/update-generated.sh
	./hack/k8s/codegen/verify-generated.sh
.PHONY: code-gen

unit-test:
	go test ./... -v -skip */test/e2e/*
.PHONY: unit-test

ut: unit-test
.PHONY: ut