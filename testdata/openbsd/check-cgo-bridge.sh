#!/bin/sh

set -eu

if [ "$(uname -s)" != "OpenBSD" ] || [ "$(uname -m)" != "amd64" ]; then
	echo "OpenBSD/amd64 guest required" >&2
	exit 1
fi

repo_root=$(cd "$(dirname "$0")/../.." && pwd)
scratch=$(mktemp -d /tmp/reflektor-openbsd-cgo.XXXXXX)
cleanup() {
	# Go's module cache is read-only by default.
	chmod -R u+w "$scratch/gopath/pkg/mod" 2>/dev/null || :
	rm -rf "$scratch"
}
trap cleanup EXIT

archive="$scratch/go1.26.6.openbsd-amd64.tar.gz"
ftp -o "$archive" https://go.dev/dl/go1.26.6.openbsd-amd64.tar.gz
expected=1fe83868b4d2f8263bfb0a46d83ad82701cbb9ef11f8eb8c23d3fe001f5027c0
actual=$(sha256 -q "$archive")
if [ "$actual" != "$expected" ]; then
	echo "Go 1.26.6 OpenBSD/amd64 archive checksum mismatch: $actual" >&2
	exit 1
fi
tar -xzf "$archive" -C "$scratch"

cd "$scratch"
GOTOOLCHAIN=local GO111MODULE=off CGO_ENABLED=1 \
	GOPATH="$scratch/gopath" GOCACHE="$scratch/gocache" \
	"$scratch/go/bin/go" build -o "$scratch/cgo-ffi-probe" \
	"$repo_root/testdata/openbsd/cgo_ffi_probe/main.go"
"$scratch/cgo-ffi-probe"

# The native CGo runtime is present, but PureGo's OpenBSD ABI is still missing.
# Keep checking that this fails only for the known bridge gap.
mkdir "$scratch/purego-probe"
cp "$repo_root/testdata/openbsd/ffi_probe/main.go" "$scratch/purego-probe/main.go"
cp "$repo_root/go.sum" "$scratch/purego-probe/go.sum"
cat > "$scratch/purego-probe/go.mod" <<'EOF'
module reflektor-openbsd-purego-probe

go 1.26.6

require github.com/ebitengine/purego v0.11.0
EOF
cd "$scratch/purego-probe"
if GOTOOLCHAIN=local CGO_ENABLED=1 GOPATH="$scratch/gopath" \
	GOCACHE="$scratch/gocache" "$scratch/go/bin/go" build \
	-o "$scratch/purego-probe-binary" . >"$scratch/purego-build.log" 2>&1; then
	echo "OpenBSD/amd64 PureGo CGo bridge now compiles; run the full support gate."
elif grep -Fq '# github.com/ebitengine/purego' "$scratch/purego-build.log" && \
	grep -Fq 'undefined: maxRegAllocStructSize' "$scratch/purego-build.log" && \
	grep -Fq 'undefined: syscallArgs' "$scratch/purego-build.log"; then
	cat "$scratch/purego-build.log"
	echo "OpenBSD/amd64 PureGo CGo ABI remains unimplemented."
else
	cat "$scratch/purego-build.log" >&2
	echo "OpenBSD/amd64 PureGo CGo probe failed for an unexpected reason" >&2
	exit 1
fi
