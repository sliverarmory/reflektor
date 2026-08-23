#!/usr/bin/env bash

set -euo pipefail

cd /workspace

echo "runtime: $(uname -a)"
echo "go: $(go version)"

if [[ "$(go env GOOS)/$(go env GOARCH)" != "linux/riscv64" ]]; then
  echo "linux/riscv64 BOF container is not running the required target" >&2
  exit 1
fi
if [[ "$(go env CGO_ENABLED)" != "0" ]]; then
  echo "linux/riscv64 BOF runtime must remain CGO-free" >&2
  exit 1
fi

for fixture in \
  "${REFLEKTOR_BOF_FIXTURE_DIR}/fixture_linux_riscv64.o" \
  "${REFLEKTOR_BOF_FIXTURE_DIR}/options_fixture_linux_riscv64.o"; do
  if [[ ! -s "${fixture}" ]]; then
    echo "required prebuilt BOF fixture is absent or empty: ${fixture}" >&2
    exit 1
  fi
  description="$(file -b "${fixture}")"
  if [[ "${description}" != *"ELF 64-bit LSB relocatable, UCB RISC-V"* ]]; then
    echo "prebuilt BOF fixture has the wrong target: ${fixture}: ${description}" >&2
    exit 1
  fi
done

if [[ ! -s "${REFLEKTOR_BOF_CORPUS_DIR}/testdata/e2e-manifest.json" ]]; then
  echo "prepared linux/riscv64 BOF corpus is absent" >&2
  exit 1
fi
corpus_count="$(find "${REFLEKTOR_BOF_CORPUS_DIR}/dist/linux/riscv64" -maxdepth 1 -type f -name '*.o' | wc -l | tr -d '[:space:]')"
if [[ "${corpus_count}" != "25" ]]; then
  echo "prepared linux/riscv64 BOF corpus has ${corpus_count} objects, want 25" >&2
  exit 1
fi

go test ./bof ./internal/bofloader -count=1

go test ./integration \
  -run '^(TestPlatformSupportManifest|TestLoadAndExecuteGeneratedBOF|TestBOFLoadWithOptions|TestBOFPackageDependencyGraphIsIsolated)$' \
  -count=1 -v | tee linux-riscv64-bof-test.log

for test_name in \
  TestPlatformSupportManifest \
  TestLoadAndExecuteGeneratedBOF \
  TestBOFLoadWithOptions \
  TestBOFPackageDependencyGraphIsIsolated; do
  if ! grep -Fq -- "--- PASS: ${test_name} " linux-riscv64-bof-test.log; then
    echo "Required CGO-free linux/riscv64 BOF test did not pass: ${test_name}" >&2
    exit 1
  fi
done
if grep -Eq '^[[:space:]]*--- SKIP:' linux-riscv64-bof-test.log; then
  echo "CGO-free linux/riscv64 BOF test was skipped; refusing to pass CI." >&2
  exit 1
fi

go test ./integration -run '^TestSituationalAwarenessBOFCorpus$' \
  -timeout 20m -count=1 -v | tee linux-riscv64-bof-corpus-test.log
if ! grep -Fq -- '--- PASS: TestSituationalAwarenessBOFCorpus ' linux-riscv64-bof-corpus-test.log; then
  echo "Required linux/riscv64 BOF corpus execution test did not pass" >&2
  exit 1
fi
if grep -Eq '^[[:space:]]*--- SKIP:' linux-riscv64-bof-corpus-test.log; then
  echo "linux/riscv64 BOF corpus execution test was skipped; refusing to pass CI." >&2
  exit 1
fi

/bin/bash /workspace/testdata/docker/run-linux-shared-tests.sh
