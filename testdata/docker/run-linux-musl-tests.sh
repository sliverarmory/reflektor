#!/bin/sh

set -eu

cd /workspace
test -s /etc/alpine-release
test ! -e /lib/libc.so.6
test ! -e /lib/libdl.so.2

export GOTOOLCHAIN=local
export GOPROXY=off

for cgo_enabled in 0 1; do
  echo "Alpine musl runtime: $(go env GOOS)/$(go env GOARCH), CGO_ENABLED=${cgo_enabled}"
  log="/tmp/reflektor-musl-${cgo_enabled}.log"
  if ! CGO_ENABLED="${cgo_enabled}" go test -tags musltest ./integration \
    -run '^TestAlpineMuslRuntime$' -timeout 10m -count=1 -v >"${log}" 2>&1; then
    cat "${log}"
    exit 1
  fi
  cat "${log}"
  grep -Fq -- '--- PASS: TestAlpineMuslRuntime ' "${log}"
  if grep -Fq -- '--- SKIP:' "${log}"; then
    echo "Alpine musl runtime probe skipped a required test" >&2
    exit 1
  fi
done

# The Go c-shared image owns a second Go runtime and requires a cgo-enabled
# host. Exercise both root load modes independently from the native fixtures.
go_log=/tmp/reflektor-musl-go-c-shared.log
if ! CGO_ENABLED=1 go test ./integration \
  -run '^TestLoadGeneratedGo(LinuxSOAndCallStartW|SharedLibraryRecursiveMode)$' \
  -timeout 10m -count=1 -v >"${go_log}" 2>&1; then
  cat "${go_log}"
  exit 1
fi
cat "${go_log}"
for name in TestLoadGeneratedGoLinuxSOAndCallStartW TestLoadGeneratedGoSharedLibraryRecursiveMode; do
  grep -Fq -- "--- PASS: ${name} " "${go_log}"
done
if grep -Fq -- '--- SKIP:' "${go_log}"; then
  echo "Alpine musl Go c-shared probe skipped a required test" >&2
  exit 1
fi

CGO_ENABLED=0 go build -buildvcs=false -o /tmp/reflektor-musl-cli ./cli
/tmp/reflektor-musl-cli --help >/dev/null
readelf -l /tmp/reflektor-musl-cli | grep -F '/lib/ld-musl-'
gcc -shared -fPIC -O2 -Wl,-z,now -o /tmp/reflektor-musl-basic.so testdata/c/basic.c
output="$(REFLEKTOR_MARKER=/tmp/reflektor-musl-marker \
  /tmp/reflektor-musl-cli /tmp/reflektor-musl-basic.so --call-export StartW)"
test "${output}" = ok
test "$(cat /tmp/reflektor-musl-marker)" = ok

# A static host has no ELF interpreter. It can execute BOFs and custom images
# without system dependencies; images needing dlopen must fail explicitly.
CGO_ENABLED=1 go test -c -tags musltest -ldflags '-linkmode external -extldflags -static' \
  -o /tmp/reflektor-musl-static.test ./integration
if readelf -l /tmp/reflektor-musl-static.test | grep -Fq INTERP; then
  echo "static musl test host unexpectedly has an ELF interpreter" >&2
  exit 1
fi
static_log=/tmp/reflektor-musl-static.log
if ! (cd integration && /tmp/reflektor-musl-static.test -test.run '^TestAlpineStaticHost$' -test.v) \
  >"${static_log}" 2>&1; then
  cat "${static_log}"
  exit 1
fi
cat "${static_log}"
grep -Fq -- '--- PASS: TestAlpineStaticHost ' "${static_log}"
if grep -Fq -- '--- SKIP:' "${static_log}"; then
  echo "static musl runtime probe skipped a required test" >&2
  exit 1
fi

CGO_ENABLED=1 go build -buildvcs=false -ldflags '-linkmode external -extldflags -static' \
  -o /tmp/reflektor-musl-static-cli ./cli
gcc -nostdlib -nodefaultlibs -shared -fPIC -O2 -Wl,-z,now \
  -o /tmp/reflektor-musl-static-args.so testdata/c/args.c
static_cli_output="$(/tmp/reflektor-musl-static-cli /tmp/reflektor-musl-static-args.so \
  --call-export ReflektorArgsInit)"
test "${static_cli_output}" = ok

echo "Alpine musl dynamic and static-host BOF, library, callback, and CLI probes passed"
