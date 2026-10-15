#!/bin/sh

set -eu

if [ "$(uname -s)" != "OpenBSD" ] || [ "$(uname -m)" != "amd64" ]; then
	echo "OpenBSD/amd64 guest required" >&2
	exit 1
fi

repo_root=$(cd "$(dirname "$0")/../.." && pwd)
scratch=$(mktemp -d /tmp/reflektor-openbsd-feasibility.XXXXXX)
trap 'rm -rf "$scratch"' EXIT

cc -O2 -Wall -Wextra -Werror -o "$scratch/execmem_probe" \
	"$repo_root/testdata/openbsd/execmem_probe.c"
"$scratch/execmem_probe"
