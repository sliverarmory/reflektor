#!/usr/bin/env bash

set -euo pipefail

cd /workspace

echo "runtime: $(uname -a)"
echo "go: $(go version)"
echo "zig: $(zig version)"
echo "rustc: $(rustc --version)"
echo "cargo: $(cargo --version)"

export CGO_ENABLED=1
export CC="zig cc"
export CXX="zig c++"
export GOCACHE=/tmp/go-build-cache
export GOMODCACHE=/tmp/go-mod-cache
export ZIG_GLOBAL_CACHE_DIR=/tmp/zig-global-cache
export ZIG_LOCAL_CACHE_DIR=/tmp/zig-local-cache

# Run every platform-applicable test. The cross-platform C build matrix has its
# own CI job because it needs Darwin-aware binary inspection tools.
go test ./... -skip '^TestBuildCSharedLibraryMatrix$' -count=1 -v | tee linux-test.log

for test_name in \
  TestLoadGeneratedCLinuxSOAndCallStartW \
  TestLoadGeneratedGoLinuxSOAndCallStartW \
  TestLoadGeneratedRustHTTPSharedLibrary \
  TestLoadLibraryAndCallExport_Linux; do
  if ! grep -Fq -- "--- PASS: ${test_name} " linux-test.log; then
    echo "Required linux/386 test did not pass: ${test_name}" >&2
    exit 1
  fi
done

unexpected_skips="$(grep -E '^--- SKIP:' linux-test.log | grep -Fv 'TestBuildCSharedLibraryMatrix' || true)"
if [[ -n "${unexpected_skips}" ]]; then
  echo "linux/386 tests were skipped; refusing to pass CI." >&2
  echo "${unexpected_skips}" >&2
  exit 1
fi
