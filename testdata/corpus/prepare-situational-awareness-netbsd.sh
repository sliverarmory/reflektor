#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 1 ]]; then
	echo "usage: $0 <Situational-Awareness-BOFs checkout>" >&2
	exit 2
fi

corpus_dir="$(cd "$1" && pwd)"
reflektor_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
patch_file="${reflektor_root}/testdata/corpus/situational-awareness-netbsd.patch"
expected_revision=693174ff525595c5ec61b08fdccb5f057ad52c94

actual_revision="$(git -C "${corpus_dir}" rev-parse HEAD)"
if [[ "${actual_revision}" != "${expected_revision}" ]]; then
	echo "error: Situational-Awareness-BOFs is at ${actual_revision}, want ${expected_revision}" >&2
	exit 1
fi

if git -C "${corpus_dir}" apply --reverse --check "${patch_file}" 2>/dev/null; then
	echo "NetBSD corpus compatibility patch is already applied"
else
	bash "${reflektor_root}/testdata/corpus/prepare-situational-awareness-freebsd.sh" "${corpus_dir}"
	git -C "${corpus_dir}" apply --check "${patch_file}"
	git -C "${corpus_dir}" apply "${patch_file}"
fi

node "${corpus_dir}/scripts/generate-e2e-manifest.mjs" --write
count="$(jq -er '[.artifacts[] | select(.os == "netbsd" and (.arch == "amd64" or .arch == "arm64"))] | length' "${corpus_dir}/testdata/e2e-manifest.json")"
if [[ "${count}" -ne 50 ]]; then
	echo "error: generated manifest has ${count} NetBSD BOFs, want 50" >&2
	exit 1
fi

echo "prepared Situational-Awareness-BOFs for NetBSD/amd64 and arm64"
