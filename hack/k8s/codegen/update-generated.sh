#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")
CODEGEN_PKG=${CODEGEN_PKG:-$(cd "${SCRIPT_ROOT}"; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../../../code-generator)}

source "vendor/k8s.io/code-generator/kube_codegen.sh"

THIS_PKG="github.com/coreos/etcd-operator/pkg/generated"

kube::codegen::gen_helpers \
    --boilerplate "${SCRIPT_ROOT}/boilerplate.go.txt" \
    "${SCRIPT_ROOT}/../../../pkg/apis"

rm -r "${SCRIPT_ROOT}/../../../pkg/generated" || true

kube::codegen::gen_client \
    --with-watch \
    --with-applyconfig \
    --output-dir "${SCRIPT_ROOT}/../../../pkg/generated" \
    --output-pkg "${THIS_PKG}" \
    --boilerplate "${SCRIPT_ROOT}/boilerplate.go.txt" \
    "${SCRIPT_ROOT}/../../../pkg/apis"