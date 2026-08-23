#!/usr/bin/env bash

set -euo pipefail

cd /workspace

echo "runtime: $(uname -a)"
echo "go: $(go version)"

if [[ "$(go env GOOS)/$(go env GOARCH)" != "linux/arm" || "$(go env GOARM)" != "7" ]]; then
  echo "linux/arm BOF container is not running the required GOARM=7 hard-float target" >&2
  exit 1
fi

for fixture in \
  "${REFLEKTOR_BOF_FIXTURE_DIR}/fixture_linux_arm.o" \
  "${REFLEKTOR_BOF_FIXTURE_DIR}/options_fixture_linux_arm.o"; do
  if [[ ! -s "${fixture}" ]]; then
    echo "required prebuilt BOF fixture is absent or empty: ${fixture}" >&2
    exit 1
  fi
  description="$(file -b "${fixture}")"
  if [[ "${description}" != *"ELF 32-bit LSB relocatable, ARM"* ]]; then
    echo "prebuilt BOF fixture has the wrong target: ${fixture}: ${description}" >&2
    exit 1
  fi
done

if [[ ! -s "${REFLEKTOR_BOF_CORPUS_DIR}/testdata/e2e-manifest.json" ]]; then
  echo "prepared linux/arm BOF corpus is absent" >&2
  exit 1
fi
corpus_count="$(find "${REFLEKTOR_BOF_CORPUS_DIR}/dist/linux/arm" -maxdepth 1 -type f -name '*.o' | wc -l | tr -d '[:space:]')"
if [[ "${corpus_count}" != "25" ]]; then
  echo "prepared linux/arm BOF corpus has ${corpus_count} objects, want 25" >&2
  exit 1
fi

go test ./bof ./internal/bofloader -count=1

go test ./integration \
  -run '^(TestPlatformSupportManifest|TestLinuxARMRuntimeVariant|TestLoadAndExecuteGeneratedBOF|TestBOFLoadWithOptions|TestBOFPackageDependencyGraphIsIsolated)$' \
  -count=1 -v | tee linux-arm-bof-test.log

for test_name in \
  TestPlatformSupportManifest \
  TestLinuxARMRuntimeVariant \
  TestLoadAndExecuteGeneratedBOF \
  TestBOFLoadWithOptions \
  TestBOFPackageDependencyGraphIsIsolated; do
  if ! grep -Fq -- "--- PASS: ${test_name} " linux-arm-bof-test.log; then
    echo "Required CGO-free linux/arm BOF test did not pass: ${test_name}" >&2
    exit 1
  fi
done
if grep -Eq '^[[:space:]]*--- SKIP:' linux-arm-bof-test.log; then
  echo "CGO-free linux/arm BOF test was skipped; refusing to pass CI." >&2
  exit 1
fi

GOARM=7,softfloat go test ./integration -run '^TestLinuxARMRuntimeVariant$' \
  -count=1 -v | tee linux-arm-softfloat-rejection-test.log
if ! grep -Fq -- '--- PASS: TestLinuxARMRuntimeVariant ' linux-arm-softfloat-rejection-test.log; then
  echo "linux/arm soft-float rejection was not proven" >&2
  exit 1
fi
if grep -Eq '^[[:space:]]*--- SKIP:' linux-arm-softfloat-rejection-test.log; then
  echo "linux/arm soft-float rejection test was skipped; refusing to pass CI." >&2
  exit 1
fi

go test ./integration -run '^TestSituationalAwarenessBOFCorpus$' \
  -timeout 20m -count=1 -v | tee linux-arm-bof-corpus-test.log
if ! grep -Fq -- '--- PASS: TestSituationalAwarenessBOFCorpus ' linux-arm-bof-corpus-test.log; then
  echo "Required linux/arm BOF corpus execution test did not pass" >&2
  exit 1
fi
if grep -Eq '^[[:space:]]*--- SKIP:' linux-arm-bof-corpus-test.log; then
  echo "linux/arm BOF corpus execution test was skipped; refusing to pass CI." >&2
  exit 1
fi

/bin/bash /workspace/testdata/docker/run-linux-shared-tests.sh
