#!/usr/bin/env bash

set -euo pipefail

if [[ "$#" -lt 3 || "$#" -gt 4 ]]; then
  echo "usage: $0 <goos> <goarch> <timeout> [log-path]" >&2
  exit 2
fi

expected_goos="$1"
expected_goarch="$2"
test_timeout="$3"

script_directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd -- "${script_directory}/../.." && pwd)"
log_path="${4:-${repository_root}/bof-corpus-${expected_goos}-${expected_goarch}.log}"

: "${REFLEKTOR_BOF_CORPUS_DIR:?REFLEKTOR_BOF_CORPUS_DIR must point to the BOF corpus checkout}"

cd "${repository_root}"

actual_target="$(go env GOOS)/$(go env GOARCH)"
expected_target="${expected_goos}/${expected_goarch}"
if [[ "${actual_target}" != "${expected_target}" ]]; then
  echo "BOF corpus runner is ${actual_target}, want ${expected_target}" >&2
  exit 1
fi
if [[ "$(go env CGO_ENABLED)" != "0" ]]; then
  echo "BOF corpus runtime must remain CGO-free" >&2
  exit 1
fi
if [[ "${expected_target}" == "linux/arm" && "$(go env GOARM)" != "7" ]]; then
  echo "linux/arm BOF corpus runtime must use GOARM=7" >&2
  exit 1
fi

manifest="${REFLEKTOR_BOF_CORPUS_DIR}/testdata/e2e-manifest.json"
artifact_directory="${REFLEKTOR_BOF_CORPUS_DIR}/dist/${expected_goos}/${expected_goarch}"
if [[ ! -s "${manifest}" ]]; then
  echo "BOF corpus manifest is absent or empty: ${manifest}" >&2
  exit 1
fi
if [[ ! -d "${artifact_directory}" ]]; then
  echo "BOF corpus target directory is absent: ${artifact_directory}" >&2
  exit 1
fi

artifact_count="$(find "${artifact_directory}" -type f -name '*.o' | wc -l | tr -d '[:space:]')"
if [[ "${artifact_count}" -le 0 ]]; then
  echo "BOF corpus has no ${expected_target} object files" >&2
  exit 1
fi

mkdir -p "$(dirname -- "${log_path}")"
CGO_ENABLED=0 go test ./integration \
  -run '^TestSituationalAwarenessBOFCorpus$' \
  -timeout "${test_timeout}" -count=1 -v 2>&1 | tee "${log_path}"

if ! grep -Eq '^--- PASS: TestSituationalAwarenessBOFCorpus \(' "${log_path}"; then
  echo "required ${expected_target} BOF corpus parent test did not pass" >&2
  exit 1
fi
if grep -Eq '^[[:space:]]*--- SKIP:' "${log_path}"; then
  echo "${expected_target} BOF corpus test was skipped" >&2
  exit 1
fi

passed_count="$(grep -Ec '^[[:space:]]+--- PASS: TestSituationalAwarenessBOFCorpus/' "${log_path}" || true)"
if [[ "${passed_count}" -ne "${artifact_count}" ]]; then
  echo "${expected_target} passed ${passed_count} BOFs, want ${artifact_count}" >&2
  exit 1
fi

echo "verified ${artifact_count} ${expected_target} BOFs without CGO or skips"
