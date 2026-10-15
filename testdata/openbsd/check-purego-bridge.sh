#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${repo_root}"

scratch="$(mktemp -d /tmp/reflektor-openbsd-purego.XXXXXX)"
trap 'rm -rf -- "${scratch}"' EXIT

export GOTOOLCHAIN=local
export GOCACHE="${GOCACHE:-${scratch}/go-cache}"

if GOOS=openbsd GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false \
	-gcflags=github.com/ebitengine/purego/internal/fakecgo=-std \
	-o "${scratch}/ffi-probe" ./testdata/openbsd/ffi_probe \
	>"${scratch}/build.log" 2>&1; then
	echo "OpenBSD/amd64 PureGo foreign calls and callbacks now cross-build; run the full platform support gate."
	exit 0
fi

if ! grep -Fq '# github.com/ebitengine/purego' "${scratch}/build.log" || \
	! grep -Fq 'undefined:' "${scratch}/build.log"; then
	cat "${scratch}/build.log" >&2
	echo "OpenBSD/amd64 foreign-call probe failed for an unexpected reason" >&2
	exit 1
fi

cat "${scratch}/build.log"
echo "OpenBSD/amd64 PureGo foreign-call bridge is not implemented in the pinned dependency."
