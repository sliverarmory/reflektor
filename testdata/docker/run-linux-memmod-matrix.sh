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
  "linux/arm/v7"
  "linux/arm64"
  "linux/ppc64le"
  "linux/riscv64"
)

for platform in "${platforms[@]}"; do
  tag="reflektor-memmod-${platform//\//-}"
  dockerfile="${DOCKERFILE}"
  run_command=("/bin/bash" "/workspace/testdata/docker/run-linux-memmod-tests.sh")
  case "${platform}" in
    linux/arm/v7)
      dockerfile="${SCRIPT_DIR}/linux-bof.Dockerfile"
      run_command=("/bin/bash" "/workspace/testdata/docker/run-linux-shared-tests.sh")
      ;;
    linux/ppc64le)
      dockerfile="${SCRIPT_DIR}/linux-ppc64le-bof.Dockerfile"
      run_command=("/bin/bash" "/workspace/testdata/docker/run-linux-shared-tests.sh")
      ;;
    linux/riscv64)
      dockerfile="${SCRIPT_DIR}/linux-riscv64-bof.Dockerfile"
      run_command=("/bin/bash" "/workspace/testdata/docker/run-linux-shared-tests.sh")
      ;;
  esac
  echo "==> building ${tag} (${platform})"
  docker build --platform "${platform}" -f "${dockerfile}" -t "${tag}" "${REPO_ROOT}"

  echo "==> running ${tag} (${platform})"
  docker run --rm --platform "${platform}" "${tag}" "${run_command[@]}"
done
