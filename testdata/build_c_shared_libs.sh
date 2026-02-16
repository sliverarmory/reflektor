#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
SOURCE_FILE="${SCRIPT_DIR}/c/basic.c"
OUT_DIR="${1:-${SCRIPT_DIR}/generated}"

if ! command -v zig >/dev/null 2>&1; then
  echo "zig not found in PATH" >&2
  exit 1
fi

mkdir -p "${OUT_DIR}"

if [[ -z "${ZIG_GLOBAL_CACHE_DIR:-}" ]]; then
  export ZIG_GLOBAL_CACHE_DIR="/tmp/reflektor-zig-global-cache"
fi
if [[ -z "${ZIG_LOCAL_CACHE_DIR:-}" ]]; then
  export ZIG_LOCAL_CACHE_DIR="/tmp/reflektor-zig-local-cache"
fi

build_one() {
  local os="$1"
  local arch="$2"
  local target="$3"
  local ext="$4"
  local out="${OUT_DIR}/basic_${os}-${arch}.${ext}"
  local -a args=("-target" "${target}")

  case "${os}" in
    darwin)
      args+=("-dynamiclib" "-fPIC" "-O2" "-g0")
      ;;
    linux)
      args+=("-shared" "-fPIC" "-O2" "-g0")
      ;;
    windows)
      args+=("-shared" "-O2" "-g0")
      ;;
    *)
      echo "unsupported os: ${os}" >&2
      exit 1
      ;;
  esac

  zig cc "${args[@]}" -o "${out}" "${SOURCE_FILE}"
  if [[ "${os}" == "windows" ]]; then
    rm -f "${OUT_DIR}/basic.lib" "${out%.dll}.pdb"
  fi
  echo "${out}"
}

build_one "darwin"  "amd64" "x86_64-macos"       "dylib"
build_one "darwin"  "arm64" "aarch64-macos"      "dylib"
build_one "linux"   "386"   "x86-linux-gnu"      "so"
build_one "linux"   "amd64" "x86_64-linux-gnu"   "so"
build_one "linux"   "arm64" "aarch64-linux-gnu"  "so"
build_one "windows" "386"   "x86-windows-gnu"    "dll"
build_one "windows" "amd64" "x86_64-windows-gnu" "dll"
build_one "windows" "arm64" "aarch64-windows-gnu" "dll"
