#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

GO_CMD=${1:-go}
PKG_ROOT=$(realpath "$(dirname ${BASH_SOURCE[0]})/..")
CODEGEN_PKG=$($GO_CMD list -m -f "{{.Dir}}" k8s.io/code-generator)

cd "$PKG_ROOT"

source "${CODEGEN_PKG}/kube_codegen.sh"

kube::codegen::gen_helpers \
  --boilerplate "${PKG_ROOT}/hack/boilerplate.go.txt" \
  "${PKG_ROOT}/pkg/apis"

kube::codegen::gen_client \
  --boilerplate "${PKG_ROOT}/hack/boilerplate.go.txt" \
  --output-dir "${PKG_ROOT}/pkg/generated" \
  --output-pkg "github.com/istiak/sample-controller/pkg/generated" \
  --with-watch \
  --with-applyconfig \
  "${PKG_ROOT}/pkg/apis"

"$GO_CMD" mod tidy
