#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${repo_root}"

if [[ "$(uname -m)" != arm64 ]]; then
	echo "Android arm64 emulator probe requires an arm64 macOS runner" >&2
	exit 1
fi

: "${ANDROID_HOME:?Android SDK must be preinstalled on the runner}"
: "${ANDROID_NDK_HOME:?Android NDK must be preinstalled on the runner}"

sdkmanager="${ANDROID_HOME}/cmdline-tools/latest/bin/sdkmanager"
avdmanager="${ANDROID_HOME}/cmdline-tools/latest/bin/avdmanager"
adb="${ANDROID_HOME}/platform-tools/adb"
emulator="${ANDROID_HOME}/emulator/emulator"
image='system-images;android-35;default;arm64-v8a'

for program in "${sdkmanager}" "${avdmanager}" "${adb}" "${emulator}"; do
	if [[ ! -x "${program}" ]]; then
		echo "required Android SDK program missing: ${program}" >&2
		exit 1
	fi
done

scratch="$(mktemp -d /tmp/reflektor-android-arm64.XXXXXX)"
emulator_pid=''
cleanup() {
	if [[ -n "${emulator_pid}" ]]; then
		"${adb}" emu kill >/dev/null 2>&1 || true
		kill "${emulator_pid}" >/dev/null 2>&1 || true
		wait "${emulator_pid}" >/dev/null 2>&1 || true
	fi
	rm -rf -- "${scratch}"
}
trap cleanup EXIT

cc=''
for toolchain in "${ANDROID_NDK_HOME}"/toolchains/llvm/prebuilt/*; do
	if [[ -x "${toolchain}/bin/aarch64-linux-android24-clang" ]]; then
		cc="${toolchain}/bin/aarch64-linux-android24-clang"
		break
	fi
done
if [[ -z "${cc}" ]]; then
	echo "Android arm64 NDK compiler missing under ${ANDROID_NDK_HOME}" >&2
	exit 1
fi

"${cc}" -O2 -Wall -Wextra -Werror -o "${scratch}/execmem_probe" \
	testdata/android/execmem_probe.c -ldl
GOOS=android GOARCH=arm64 CGO_ENABLED=1 CC="${cc}" \
	go build -buildvcs=false -o "${scratch}/ffi_probe" \
	./testdata/android/ffi_probe

# The runner image must already have accepted any required SDK license.
# Answering no keeps this feasibility job from accepting new terms.
printf 'n\n' | "${sdkmanager}" --install "${image}"
if [[ ! -f "${ANDROID_HOME}/system-images/android-35/default/arm64-v8a/system.img" ]]; then
	echo "Android arm64 system image is unavailable; no new SDK license was accepted" >&2
	exit 1
fi
printf 'no\n' | "${avdmanager}" create avd -n reflektor-arm64 \
	-k "${image}" --force

"${emulator}" -avd reflektor-arm64 -no-window -no-audio -no-boot-anim \
	-no-snapshot -gpu swiftshader_indirect >"${scratch}/emulator.log" 2>&1 &
emulator_pid=$!

booted=false
for _ in $(seq 1 120); do
	if [[ "$("${adb}" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r' || true)" == "1" ]]; then
		booted=true
		break
	fi
	sleep 5
done
if [[ "${booted}" != true ]]; then
	tail -100 "${scratch}/emulator.log" >&2
	echo "Android emulator did not complete boot" >&2
	exit 1
fi

sdk_version="$("${adb}" shell getprop ro.build.version.sdk | tr -d '\r')"
abi="$("${adb}" shell getprop ro.product.cpu.abi | tr -d '\r')"
echo "Android API: ${sdk_version}; ABI: ${abi}"
"${adb}" shell id
"${adb}" shell getenforce
if [[ "${sdk_version}" != 35 || "${abi}" != arm64-v8a ]]; then
	echo "unexpected Android guest: API=${sdk_version} ABI=${abi}" >&2
	exit 1
fi

"${adb}" push "${scratch}/execmem_probe" /data/local/tmp/reflektor-execmem-probe
"${adb}" push "${scratch}/ffi_probe" /data/local/tmp/reflektor-ffi-probe
"${adb}" shell chmod 755 /data/local/tmp/reflektor-execmem-probe /data/local/tmp/reflektor-ffi-probe
"${adb}" shell /data/local/tmp/reflektor-execmem-probe
"${adb}" shell /data/local/tmp/reflektor-ffi-probe
