# Android arm64 feasibility probe

This probe runs on GitHub's arm64 `macos-15` runner. It uses the
runner's preinstalled Android SDK and NDK to build two Android arm64 programs,
then boots an Android API 35 arm64 emulator and runs them through `adb shell`.

* `execmem_probe.c` checks Bionic `dlopen`/`dlsym` and a writable to executable
  memory transition by executing an eight byte arm64 function.
* `ffi_probe/main.go` checks that a cgo enabled Go binary can use PureGo to call
  Bionic `getpid` and round trip a callback.

The SDK installer receives `n` on standard input. The job fails if the system
image is unavailable without accepting a new license.

This is a shell domain prerequisite check. It does not exercise a Reflektor BOF,
its Go or C library APIs, recursive loading, or an Android app process. Android
arm64 therefore remains unsupported in `platform-support.json`. Support needs
those end to end fixtures, including the app process policy, before promotion.
