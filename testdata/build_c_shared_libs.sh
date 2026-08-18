#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
SOURCE_FILE="${SCRIPT_DIR}/c/basic.c"
RECURSIVE_LEAF_SOURCE="${SCRIPT_DIR}/c/recursive_leaf.c"
RECURSIVE_MIDDLE_SOURCE="${SCRIPT_DIR}/c/recursive_middle.c"
RECURSIVE_ROOT_SOURCE="${SCRIPT_DIR}/c/recursive_root.c"
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

build_recursive_one() {
  local os="$1"
  local arch="$2"
  local target="$3"
  local ext="$4"
  local graph_dir="${OUT_DIR}/recursive_${os}-${arch}"
  local leaf="${graph_dir}/libreflektor_leaf.${ext}"
  local middle="${graph_dir}/libreflektor_middle.${ext}"
  local root="${graph_dir}/reflektor_recursive_root.${ext}"
  mkdir -p "${graph_dir}"

  case "${os}" in
    darwin)
      zig cc -target "${target}" -O2 -g0 -dynamiclib -fPIC \
        '-Wl,-install_name,@rpath/libreflektor_leaf.dylib' \
        -o "${leaf}" "${RECURSIVE_LEAF_SOURCE}"
      zig cc -target "${target}" -O2 -g0 -dynamiclib -fPIC \
        '-Wl,-install_name,@rpath/libreflektor_middle.dylib' \
        '-Wl,-rpath,@loader_path' \
        -o "${middle}" "${RECURSIVE_MIDDLE_SOURCE}" \
        -L"${graph_dir}" -lreflektor_leaf
      zig cc -target "${target}" -O2 -g0 -dynamiclib -fPIC \
        '-Wl,-rpath,@loader_path' \
        -o "${root}" "${RECURSIVE_ROOT_SOURCE}" \
        -L"${graph_dir}" -lreflektor_middle
      ;;
    linux)
      zig cc -target "${target}" -O2 -g0 -shared -fPIC \
        -Wl,-z,now -Wl,-z,defs -Wl,-soname,libreflektor_leaf.so \
        -o "${leaf}" "${RECURSIVE_LEAF_SOURCE}"
      zig cc -target "${target}" -O2 -g0 -shared -fPIC \
        -Wl,-z,now -Wl,-z,defs -Wl,-soname,libreflektor_middle.so \
        -Wl,--enable-new-dtags '-Wl,-rpath,$ORIGIN' \
        -o "${middle}" "${RECURSIVE_MIDDLE_SOURCE}" \
        -L"${graph_dir}" -Wl,--no-as-needed -lreflektor_leaf
      zig cc -target "${target}" -O2 -g0 -shared -fPIC \
        -Wl,-z,now -Wl,-z,defs -Wl,-soname,reflektor_recursive_root.so \
        -Wl,--enable-new-dtags '-Wl,-rpath,$ORIGIN' \
        -o "${root}" "${RECURSIVE_ROOT_SOURCE}" \
        -L"${graph_dir}" -Wl,--no-as-needed -lreflektor_middle
      ;;
    windows)
      local leaf_import="${graph_dir}/libreflektor_leaf.lib"
      local middle_import="${graph_dir}/libreflektor_middle.lib"
      zig cc -target "${target}" -O2 -g0 -shared \
        -Wl,--out-implib,"${leaf_import}" \
        -o "${leaf}" "${RECURSIVE_LEAF_SOURCE}"
      zig cc -target "${target}" -O2 -g0 -shared \
        -Wl,--out-implib,"${middle_import}" \
        -o "${middle}" "${RECURSIVE_MIDDLE_SOURCE}" "${leaf_import}"
      zig cc -target "${target}" -O2 -g0 -shared \
        -o "${root}" "${RECURSIVE_ROOT_SOURCE}" "${middle_import}"
      ;;
    *)
      echo "unsupported os: ${os}" >&2
      exit 1
      ;;
  esac

  echo "${root}"
}

build_one "darwin"  "amd64" "x86_64-macos"       "dylib"
build_one "darwin"  "arm64" "aarch64-macos"      "dylib"
build_one "linux"   "386"   "x86-linux-gnu"      "so"
build_one "linux"   "amd64" "x86_64-linux-gnu"   "so"
build_one "linux"   "arm64" "aarch64-linux-gnu"  "so"
build_one "windows" "386"   "x86-windows-gnu"    "dll"
build_one "windows" "amd64" "x86_64-windows-gnu" "dll"
build_one "windows" "arm64" "aarch64-windows-gnu" "dll"

build_recursive_one "darwin"  "amd64" "x86_64-macos"        "dylib"
build_recursive_one "darwin"  "arm64" "aarch64-macos"       "dylib"
build_recursive_one "linux"   "386"   "x86-linux-gnu"       "so"
build_recursive_one "linux"   "amd64" "x86_64-linux-gnu"    "so"
build_recursive_one "linux"   "arm64" "aarch64-linux-gnu"   "so"
build_recursive_one "windows" "386"   "x86-windows-gnu"     "dll"
build_recursive_one "windows" "amd64" "x86_64-windows-gnu"  "dll"
build_recursive_one "windows" "arm64" "aarch64-windows-gnu" "dll"
