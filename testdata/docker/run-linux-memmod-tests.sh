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
go test ./... -skip '^TestBuild(CSharedLibraryMatrix|RecursiveCSharedLibraryMatrix)$' -count=1 -v | tee linux-test.log

for test_name in \
  TestLoadGeneratedCLinuxSOAndCallStartW \
  TestLoadGeneratedGoLinuxSOAndCallStartW \
  TestLoadGeneratedRustHTTPSharedLibrary \
  TestLoadGeneratedCSharedLibraryRecursiveMode \
  TestLoadGeneratedGoSharedLibraryRecursiveMode \
  TestLoadGeneratedRustSharedLibraryRecursiveMode \
  TestLoadLibraryRecursiveDependencies \
  TestCallExportWithArgs \
  TestNativePackageCallExport \
  TestNativePackageRejectsGoCSharedImage \
  TestNativePackageLinuxDependencyGraphIsIsolated \
  TestNativePackageCallExportWithArgsLinux \
  TestNativePackageRustCallExportWithArgsLinux \
  TestNativePackageELFLifecycleLinux \
  TestNativePackageCSharedConsumerLinux \
  TestLoadLibraryAndCallExport_Linux; do
  if ! grep -Fq -- "--- PASS: ${test_name} " linux-test.log; then
    echo "Required linux/386 test did not pass: ${test_name}" >&2
    exit 1
  fi
done

unexpected_skips="$(grep -E '^--- SKIP:' linux-test.log | grep -Ev 'TestBuild(CSharedLibraryMatrix|RecursiveCSharedLibraryMatrix)' || true)"
if [[ -n "${unexpected_skips}" ]]; then
  echo "linux/386 tests were skipped; refusing to pass CI." >&2
  echo "${unexpected_skips}" >&2
  exit 1
fi

CGO_ENABLED=0 go test ./integration -run '^(TestCallExportWith(Args|PuregoCallback)|TestNativePackage(CallExport|CallExportWithArgsLinux|RustCallExportWithArgsLinux|ELFLifecycleLinux|LinuxDependencyGraphIsIsolated))$' -count=1 -v | tee linux-nocgo-test.log
for test_name in \
  TestCallExportWithArgs \
  TestCallExportWithPuregoCallback \
  TestNativePackageCallExport \
  TestNativePackageLinuxDependencyGraphIsIsolated \
  TestNativePackageCallExportWithArgsLinux \
  TestNativePackageRustCallExportWithArgsLinux \
  TestNativePackageELFLifecycleLinux; do
  if ! grep -Fq -- "--- PASS: ${test_name} " linux-nocgo-test.log; then
    echo "Required CGO-free linux/386 test did not pass: ${test_name}" >&2
    exit 1
  fi
done
