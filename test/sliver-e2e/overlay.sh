#!/usr/bin/env bash

set -euo pipefail

readonly REFLEKTOR_MODULE="github.com/sliverarmory/reflektor"
readonly SLIVER_REF_MARKER=".reflektor-e2e-sliver-ref"
readonly REFLEKTOR_REF_MARKER=".reflektor-e2e-reflektor-ref"

die() {
	echo "reflektor Sliver overlay: $*" >&2
	exit 1
}

usage() {
	cat >&2 <<'USAGE'
usage: overlay.sh prepare <reflektor-root> <sliver-root>
       overlay.sh verify  <reflektor-root> <sliver-root>

prepare injects the current Reflektor checkout through a temporary go.mod
replace, runs Sliver's official `go generate ./implant`, removes the local
replace, and verifies the resulting vendored source byte-for-byte.

Set SLIVER_REF to the immutable 40-hex Sliver commit expected in the checkout.
USAGE
	exit 2
}

canonical_dir() {
	[[ -d "$1" ]] || die "directory does not exist: $1"
	(
		cd -- "$1"
		pwd -P
	)
}

normalize_sha() {
	printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

validate_sha() {
	local label="$1"
	local value="$2"
	[[ "$value" =~ ^[[:xdigit:]]{40}$ ]] || die "$label must be an immutable 40-hex commit, got: $value"
}

verify_ref_marker() {
	local root="$1"
	local marker_name="$2"
	local expected="$3"
	local label="$4"
	local actual=""

	validate_sha "$label" "$expected"
	expected="$(normalize_sha "$expected")"
	if [[ -d "$root/.git" ]]; then
		actual="$(git -C "$root" rev-parse HEAD)"
	elif [[ -f "$root/$marker_name" ]]; then
		actual="$(tr -d '[:space:]' < "$root/$marker_name")"
	else
		die "cannot verify $label: neither Git metadata nor $marker_name exists"
	fi
	actual="$(normalize_sha "$actual")"
	[[ "$actual" == "$expected" ]] || die "$label mismatch: got $actual, want $expected"
}

is_vendored_source() {
	case "$1" in
		*_test.go) return 1 ;;
		*.go|*.s|*.S|*.c|*.h|*.cc|*.cpp|*.cxx|*.m|*.mm|*.f|*.F|*.for|*.f90|*.syso) return 0 ;;
		*) return 1 ;;
	esac
}

verify_source_tree() {
	local source_root="$1"
	local vendor_root="$2"
	local subtree="$3"
	local source_file=""
	local vendor_file=""
	local relative=""
	local count=0
	local source_is_git=0

	[[ -d "$source_root/$subtree" ]] || die "Reflektor source subtree is missing: $subtree"
	[[ -d "$vendor_root/$subtree" ]] || die "Sliver vendor subtree is missing: $subtree"
	if git -C "$source_root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
		source_is_git=1
	fi

	while IFS= read -r source_file; do
		is_vendored_source "$source_file" || continue
		relative="${source_file#"$source_root/"}"
		vendor_file="$vendor_root/$relative"
		[[ -f "$vendor_file" ]] || die "vendored Reflektor source is missing: $relative"
		if [[ "$source_is_git" == "1" ]]; then
			# Compare the committed blob, not a checkout that Git may have
			# rewritten to CRLF on Windows runners.
			git -C "$source_root" cat-file blob "HEAD:$relative" | cmp -s -- - "$vendor_file" ||
				die "vendored Reflektor source differs: $relative"
		else
			cmp -s -- "$source_file" "$vendor_file" || die "vendored Reflektor source differs: $relative"
		fi
		count=$((count + 1))
	done < <(find "$source_root/$subtree" -type f -print | LC_ALL=C sort)

	while IFS= read -r vendor_file; do
		is_vendored_source "$vendor_file" || continue
		relative="${vendor_file#"$vendor_root/"}"
		source_file="$source_root/$relative"
		[[ -f "$source_file" ]] || die "Sliver vendor contains unexpected Reflektor source: $relative"
	done < <(find "$vendor_root/$subtree" -type f -print | LC_ALL=C sort)

	[[ "$count" -gt 0 ]] || die "no Reflektor source files were verified under $subtree"
	echo "Verified $count byte-identical Reflektor source files under $subtree"
}

verify_overlay() {
	local reflektor_root="$1"
	local sliver_root="$2"
	local implant_mod="$sliver_root/implant/go-mod"
	local modules_file="$sliver_root/implant/vendor/modules.txt"
	local vendor_root="$sliver_root/implant/vendor/$REFLEKTOR_MODULE"
	local expected_sliver_ref="${SLIVER_REF:-}"
	local expected_reflektor_ref=""
	local header_count=""

	[[ -f "$implant_mod" ]] || die "Sliver implant/go-mod is missing"
	[[ -f "$modules_file" ]] || die "Sliver implant/vendor/modules.txt is missing"
	[[ -f "$sliver_root/implant/sliver/extension/extension_unix.go" ]] || die "Sliver Unix extension source is missing"

	if [[ -n "$expected_sliver_ref" ]]; then
		verify_ref_marker "$sliver_root" "$SLIVER_REF_MARKER" "$expected_sliver_ref" "Sliver ref"
	fi
	if [[ -f "$sliver_root/$REFLEKTOR_REF_MARKER" ]]; then
		expected_reflektor_ref="$(tr -d '[:space:]' < "$sliver_root/$REFLEKTOR_REF_MARKER")"
		verify_ref_marker "$reflektor_root" "$REFLEKTOR_REF_MARKER" "$expected_reflektor_ref" "Reflektor ref"
	fi

	grep -Fq -- 'github.com/sliverarmory/reflektor/native' \
		"$sliver_root/implant/sliver/extension/extension_unix.go" || die "Sliver's production Unix extension does not import the tag-free Reflektor native package"
	grep -Fq -- "$REFLEKTOR_MODULE/native" "$modules_file" || die "implant/vendor/modules.txt does not include the native package"

	if grep -Fq -- "replace $REFLEKTOR_MODULE" "$implant_mod"; then
		die "implant/go-mod retains a Reflektor replace directive"
	fi
	if grep -Fq -- "# $REFLEKTOR_MODULE =>" "$modules_file"; then
		die "implant/vendor/modules.txt retains a replacement-only Reflektor record"
	fi
	if grep -F -- "# $REFLEKTOR_MODULE " "$modules_file" | grep -Fq -- ' => '; then
		die "implant/vendor/modules.txt retains a local Reflektor replacement path"
	fi
	header_count="$(awk -v prefix="# $REFLEKTOR_MODULE " 'index($0, prefix) == 1 { count++ } END { print count + 0 }' "$modules_file")"
	[[ "$header_count" == "1" ]] || die "expected one normalized Reflektor module header, found $header_count"

	verify_source_tree "$reflektor_root" "$vendor_root" native
	verify_source_tree "$reflektor_root" "$vendor_root" memmod
}

prepare_overlay() {
	local reflektor_root="$1"
	local sliver_root="$2"
	local implant_mod="$sliver_root/implant/go-mod"
	local implant_sum="$sliver_root/implant/go-sum"
	local modules_file="$sliver_root/implant/vendor/modules.txt"
	local overlay_tmp=""
	local original_mod=""
	local original_sum=""
	local edited_mod=""
	local normalized_modules=""
	local restore_pending=0
	local sliver_sha=""
	local reflektor_sha=""

	[[ -n "${SLIVER_REF:-}" ]] || die "SLIVER_REF is required in prepare mode"
	verify_ref_marker "$sliver_root" "$SLIVER_REF_MARKER" "$SLIVER_REF" "Sliver ref"
	[[ -f "$reflektor_root/go.mod" ]] || die "Reflektor go.mod is missing"
	[[ -f "$implant_mod" ]] || die "Sliver implant/go-mod is missing"
	[[ -f "$implant_sum" ]] || die "Sliver implant/go-sum is missing"
	git -C "$sliver_root" diff --quiet -- . || die "Sliver checkout has unstaged changes before overlay preparation"
	git -C "$sliver_root" diff --cached --quiet -- . || die "Sliver checkout has staged changes before overlay preparation"
	git -C "$reflektor_root" diff --quiet -- native memmod cli reflektor.go || die "Reflektor loader source has unstaged changes"
	git -C "$reflektor_root" diff --cached --quiet -- native memmod cli reflektor.go || die "Reflektor loader source has staged changes"

	overlay_tmp="$(mktemp -d "${TMPDIR:-/tmp}/reflektor-sliver-overlay.XXXXXX")"
	original_mod="$overlay_tmp/original-go-mod"
	original_sum="$overlay_tmp/original-go-sum"
	edited_mod="$overlay_tmp/go.mod"
	normalized_modules="$overlay_tmp/modules.txt"
	cp -- "$implant_mod" "$original_mod"
	cp -- "$implant_sum" "$original_sum"
	cp -- "$implant_mod" "$edited_mod"

	cleanup_overlay() {
		if [[ "$restore_pending" == "1" && -f "$original_mod" ]]; then
			cp -- "$original_mod" "$implant_mod"
			cp -- "$original_sum" "$implant_sum"
		fi
		rm -rf -- "$overlay_tmp"
	}
	trap cleanup_overlay EXIT

	GOWORK=off go mod edit -modfile="$edited_mod" "-replace=$REFLEKTOR_MODULE=$reflektor_root"
	cp -- "$edited_mod" "$implant_mod"
	restore_pending=1

	echo "Regenerating Sliver implant vendor with current Reflektor source"
	(
		cd -- "$sliver_root"
		GOWORK=off go generate ./implant
	)

	cp -- "$original_mod" "$implant_mod"
	cp -- "$original_sum" "$implant_sum"
	restore_pending=0
	[[ -f "$modules_file" ]] || die "Sliver vendor generation did not create implant/vendor/modules.txt"
	awk -v module="$REFLEKTOR_MODULE" '
		index($0, "# " module " => ") == 1 { next }
		index($0, "# " module " ") == 1 { sub(/ => .*/, "") }
		{ print }
	' "$modules_file" > "$normalized_modules"
	mv -- "$normalized_modules" "$modules_file"

	sliver_sha="$(normalize_sha "$SLIVER_REF")"
	reflektor_sha="$(git -C "$reflektor_root" rev-parse HEAD)"
	validate_sha "Reflektor ref" "$reflektor_sha"
	printf '%s\n' "$sliver_sha" > "$sliver_root/$SLIVER_REF_MARKER"
	printf '%s\n' "$reflektor_sha" > "$sliver_root/$REFLEKTOR_REF_MARKER"

	cmp -s -- "$original_mod" "$implant_mod" || die "implant/go-mod was not restored exactly after vendor generation"
	cmp -s -- "$original_sum" "$implant_sum" || die "implant/go-sum was not restored exactly after vendor generation"
	verify_overlay "$reflektor_root" "$sliver_root"
	trap - EXIT
	cleanup_overlay
}

[[ "$#" == "3" ]] || usage
mode="$1"
reflektor_root="$(canonical_dir "$2")"
sliver_root="$(canonical_dir "$3")"

case "$mode" in
	prepare) prepare_overlay "$reflektor_root" "$sliver_root" ;;
	verify) verify_overlay "$reflektor_root" "$sliver_root" ;;
	*) usage ;;
esac
