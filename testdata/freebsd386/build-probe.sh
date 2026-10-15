#!/bin/sh
set -eu

# Cross-build a diagnostic probe using a disposable copy of the pinned PureGo
# release. This is a feasibility experiment, not a project dependency change.
repo=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
go_bin=${GO:-go}
out=${1:-$(mktemp -d "${TMPDIR:-/tmp}/reflektor-freebsd386.XXXXXX")}
mkdir -p "$out"
out=$(CDPATH= cd -- "$out" && pwd)

version=$("$go_bin" version)
case "$version" in
  *"go1.26.6"*) ;;
  *) echo "Go 1.26.6 required; got: $version" >&2; exit 1 ;;
esac

module_path=$("$go_bin" env GOMODCACHE)/github.com/ebitengine/purego@v0.11.0
if [ ! -d "$module_path" ]; then
  (cd "$repo" && "$go_bin" mod download github.com/ebitengine/purego@v0.11.0)
fi
cp -R "$module_path" "$out/purego"
chmod -R u+w "$out/purego"

change_tag() {
  file=$1
  before=$2
  after=$3
  count=$(grep -Fxc "$before" "$file" || true)
  if [ "$count" != 1 ]; then
    echo "Expected one '$before' build tag in $file; found $count" >&2
    exit 1
  fi
  awk -v before="$before" -v after="$after" \
    '{ if ($0 == before) print after; else print }' "$file" > "$file.new"
  mv "$file.new" "$file"
}

for file in sys_386.s sys_unix_386.s zcallback_386.s; do
  change_tag "$out/purego/$file" '//go:build linux' '//go:build linux || freebsd'
done
change_tag "$out/purego/internal/fakecgo/go_freebsd.go" \
  '//go:build !cgo && (amd64 || arm64)' \
  '//go:build !cgo && (386 || amd64 || arm64)'

cp "$repo/go.mod" "$out/probe.mod"
cp "$repo/go.sum" "$out/probe.sum"
(cd "$repo" && "$go_bin" mod edit -modfile="$out/probe.mod" \
  -replace="github.com/ebitengine/purego=$out/purego")

(cd "$repo" && GOOS=freebsd GOARCH=386 CGO_ENABLED=0 GOTOOLCHAIN=local \
  "$go_bin" build -modfile="$out/probe.mod" \
  -gcflags=github.com/ebitengine/purego/internal/fakecgo=-std \
  -o "$out/probe-patched" ./testdata/freebsd386)

echo "Probe: $out/probe-patched"
echo "Disposable PureGo copy: $out/purego"
