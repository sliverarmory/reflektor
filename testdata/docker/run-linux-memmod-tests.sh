#!/usr/bin/env bash

set -euo pipefail

cd /workspace

echo "runtime: $(uname -a)"
echo "go: $(go version)"
echo "zig: $(zig version)"

export CGO_ENABLED=0
export GOCACHE=/tmp/go-build-cache
export GOMODCACHE=/tmp/go-mod-cache
export ZIG_GLOBAL_CACHE_DIR=/tmp/zig-global-cache
export ZIG_LOCAL_CACHE_DIR=/tmp/zig-local-cache

# Validate the linux memmod backend against the C shared library test case.
go test ./memmod -run TestLoadLibraryAndCallExport_Linux -count=1 -v

# Validate the root package linux shared-library load case too.
go test ./... -run TestLoadGeneratedCLinuxSOAndCallStartW -count=1 -v

# Validate the root package linux shared-library load case using a Go c-shared fixture.
go test ./... -run TestLoadGeneratedGoLinuxSOAndCallStartW -count=1 -v
