#!/usr/bin/env bash

set -euo pipefail

cd /workspace

echo "runtime: $(uname -a)"
echo "go: $(go version)"

if [[ "$(go env GOOS)/$(go env GOARCH)" != "linux/ppc64le" ]]; then
  echo "linux/ppc64le BOF container is not running the required target" >&2
  exit 1
fi
if [[ "$(go env CGO_ENABLED)" != "0" ]]; then
  echo "linux/ppc64le BOF runtime must remain CGO-free" >&2
  exit 1
fi

for fixture in \
  "${REFLEKTOR_BOF_FIXTURE_DIR}/fixture_linux_ppc64le.o" \
  "${REFLEKTOR_BOF_FIXTURE_DIR}/options_fixture_linux_ppc64le.o"; do
  if [[ ! -s "${fixture}" ]]; then
    echo "required prebuilt BOF fixture is absent or empty: ${fixture}" >&2
    exit 1
  fi
  description="$(file -b "${fixture}")"
  if [[ "${description}" != *"ELF 64-bit LSB relocatable, 64-bit PowerPC or cisco 7500, OpenPOWER ELF V2 ABI"* ]]; then
    echo "prebuilt BOF fixture has the wrong target or ABI: ${fixture}: ${description}" >&2
    exit 1
  fi
  machine="$(od -An -tx1 -j18 -N2 "${fixture}" | tr -d '[:space:]')"
  if [[ "${machine}" != "1500" ]]; then
    echo "prebuilt BOF fixture has the wrong ELF machine: ${fixture}: ${machine}" >&2
    exit 1
  fi
  flags="$(od -An -tx1 -j48 -N4 "${fixture}" | tr -d '[:space:]')"
  if [[ "${flags}" != "02000000" ]]; then
    echo "prebuilt BOF fixture does not declare the PowerPC64 ELFv2 ABI: ${fixture}: ${flags}" >&2
    exit 1
  fi
done

if [[ ! -s "${REFLEKTOR_BOF_CORPUS_DIR}/testdata/e2e-manifest.json" ]]; then
  echo "prepared linux/ppc64le BOF corpus is absent" >&2
  exit 1
fi
corpus_count="$(find "${REFLEKTOR_BOF_CORPUS_DIR}/dist/linux/ppc64le" -maxdepth 1 -type f -name '*.o' | wc -l | tr -d '[:space:]')"
if [[ "${corpus_count}" != "25" ]]; then
  echo "prepared linux/ppc64le BOF corpus has ${corpus_count} objects, want 25" >&2
  exit 1
fi

go test ./bof ./internal/bofloader -count=1

go test ./integration \
  -run '^(TestPlatformSupportManifest|TestLoadAndExecuteGeneratedBOF|TestBOFLoadWithOptions|TestBOFPackageDependencyGraphIsIsolated)$' \
  -count=1 -v | tee linux-ppc64le-bof-test.log

for test_name in \
  TestPlatformSupportManifest \
  TestLoadAndExecuteGeneratedBOF \
  TestBOFLoadWithOptions \
  TestBOFPackageDependencyGraphIsIsolated; do
  if ! grep -Fq -- "--- PASS: ${test_name} " linux-ppc64le-bof-test.log; then
    echo "Required CGO-free linux/ppc64le BOF test did not pass: ${test_name}" >&2
    exit 1
  fi
done
if grep -Eq '^[[:space:]]*--- SKIP:' linux-ppc64le-bof-test.log; then
  echo "CGO-free linux/ppc64le BOF test was skipped; refusing to pass CI." >&2
  exit 1
fi

go test ./integration -run '^TestSituationalAwarenessBOFCorpus$' \
  -timeout 20m -count=1 -v | tee linux-ppc64le-bof-corpus-test.log
if ! grep -Fq -- '--- PASS: TestSituationalAwarenessBOFCorpus ' linux-ppc64le-bof-corpus-test.log; then
  echo "Required linux/ppc64le BOF corpus execution test did not pass" >&2
  exit 1
fi
if grep -Eq '^[[:space:]]*--- SKIP:' linux-ppc64le-bof-corpus-test.log; then
  echo "linux/ppc64le BOF corpus execution test was skipped; refusing to pass CI." >&2
  exit 1
fi

/bin/bash /workspace/testdata/docker/run-linux-shared-tests.sh
