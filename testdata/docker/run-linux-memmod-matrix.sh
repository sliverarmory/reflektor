#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
DOCKERFILE="${SCRIPT_DIR}/linux-memmod.Dockerfile"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker not found in PATH" >&2
  exit 1
fi

platforms=(
  "linux/386"
  "linux/amd64"
  "linux/arm64"
)

for platform in "${platforms[@]}"; do
  tag="reflektor-memmod-${platform//\//-}"
  echo "==> building ${tag} (${platform})"
  docker build --platform "${platform}" -f "${DOCKERFILE}" -t "${tag}" "${REPO_ROOT}"

  echo "==> running ${tag} (${platform})"
  docker run --rm --platform "${platform}" "${tag}"
done
