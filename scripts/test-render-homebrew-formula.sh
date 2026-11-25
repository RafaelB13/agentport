#!/bin/sh
# Tests scripts/render-homebrew-formula.sh against a fake checksums.txt: the
# right digest must land in the right platform block, and bad input must fail.
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
renderer="$root/scripts/render-homebrew-formula.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/agentport-formula-test.XXXXXX")
trap 'rm -rf "$work"' EXIT INT TERM

failures=0

pass() {
	echo "ok   $1"
}

failed() {
	echo "FAIL $1" >&2
	failures=$((failures + 1))
}

# repeat CHARACTER prints 64 copies of it: a well-formed, recognisable digest.
repeat() {
	printf '%64s' '' | tr ' ' "$1"
}

tag=v1.2.3
sha_darwin_arm64=$(repeat a)
sha_darwin_amd64=$(repeat b)
sha_linux_arm64=$(repeat c)
sha_linux_amd64=$(repeat d)
sha_windows=$(repeat e)

checksums="$work/checksums.txt"
{
	printf '%s  agentport_%s_darwin_arm64.tar.gz\n' "$sha_darwin_arm64" "$tag"
	printf '%s  agentport_%s_darwin_amd64.tar.gz\n' "$sha_darwin_amd64" "$tag"
	printf '%s *agentport_%s_linux_arm64.tar.gz\n' "$sha_linux_arm64" "$tag"
	printf '%s  agentport_%s_linux_amd64.tar.gz\n' "$sha_linux_amd64" "$tag"
	printf '%s  agentport_%s_windows_amd64.zip\n' "$sha_windows" "$tag"
	printf '%s  agentport_%s_linux_amd64.cdx.json\n' "$(repeat f)" "$tag"
} >"$checksums"

formula="$work/agentport.rb"
if sh "$renderer" "$tag" "$checksums" >"$formula"; then
	pass "renders $tag from a checksums file with extra entries"
else
	failed "rendering $tag failed"
fi

expect_line() {
	if grep -Fqx "$1" "$formula"; then
		pass "formula has: $1"
	else
		failed "formula is missing: $1"
	fi
}

expect_line '  version "1.2.3"'
expect_line '      url "https://github.com/rafaelb13/agentport/releases/download/v1.2.3/agentport_v1.2.3_darwin_arm64.tar.gz"'
expect_line "      sha256 \"$sha_darwin_arm64\""
expect_line '      url "https://github.com/rafaelb13/agentport/releases/download/v1.2.3/agentport_v1.2.3_darwin_amd64.tar.gz"'
expect_line "      sha256 \"$sha_darwin_amd64\""
expect_line '      url "https://github.com/rafaelb13/agentport/releases/download/v1.2.3/agentport_v1.2.3_linux_arm64.tar.gz"'
expect_line "      sha256 \"$sha_linux_arm64\""
expect_line '      url "https://github.com/rafaelb13/agentport/releases/download/v1.2.3/agentport_v1.2.3_linux_amd64.tar.gz"'
expect_line "      sha256 \"$sha_linux_amd64\""

if grep -q '@[A-Z0-9_]*@' "$formula"; then
	failed "formula still has placeholders"
else
	pass "no placeholders left"
fi

if grep -q "$sha_windows" "$formula"; then
	failed "the Windows digest leaked into the Homebrew formula"
else
	pass "the Windows archive is not in the formula"
fi

if command -v ruby >/dev/null 2>&1; then
	if ruby -c "$formula" >/dev/null; then
		pass "ruby -c accepts the rendered formula"
	else
		failed "ruby -c rejects the rendered formula"
	fi
fi

expect_refusal() {
	label=$1
	shift
	if sh "$renderer" "$@" >/dev/null 2>"$work/stderr"; then
		failed "$label was accepted"
	else
		pass "$label is refused: $(head -n 1 "$work/stderr")"
	fi
}

expect_refusal "a pre-release tag" v1.3.0-rc.1 "$checksums"
expect_refusal "a version without the v prefix" 1.2.3 "$checksums"
expect_refusal "a missing argument" "$tag"

missing="$work/missing.txt"
grep -v 'linux_arm64' "$checksums" >"$missing"
expect_refusal "a checksums file without one archive" "$tag" "$missing"

malformed="$work/malformed.txt"
sed 's/^a\{64\}/not-a-digest/' "$checksums" >"$malformed"
expect_refusal "a malformed digest" "$tag" "$malformed"

if [ "$failures" -ne 0 ]; then
	echo "$failures check(s) failed" >&2
	exit 1
fi
echo "all render-homebrew-formula checks passed"
