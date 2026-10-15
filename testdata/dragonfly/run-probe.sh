#!/bin/sh
set -eu

source_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
build_dir=${TMPDIR:-/tmp}/reflektor-dragonfly-probe
mkdir -p "$build_dir"

cc -Wall -Wextra -Werror -fPIC -shared -o "$build_dir/libprobe.so" "$source_dir/libprobe.c"
cc -Wall -Wextra -Werror -pthread -o "$build_dir/probe" "$source_dir/probe.c" -ldl
cd "$build_dir"
./probe
