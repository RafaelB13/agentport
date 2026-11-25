#!/bin/sh
# Renders the Homebrew formula of one stable release from the release's
# checksums.txt, so the formula carries the SHA-256 that the release published.
#
# Usage: render-homebrew-formula.sh vX.Y.Z CHECKSUMS_FILE [TEMPLATE] > agentport.rb
#
# Pre-releases (a tag with a suffix such as -rc.1) are refused: the tap only
# carries what `brew install` should get by default.
set -eu

usage() {
	echo "usage: $0 vX.Y.Z CHECKSUMS_FILE [TEMPLATE]" >&2
	exit 2
}

fail() {
	echo "render-homebrew-formula: $*" >&2
	exit 1
}

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
	usage
fi

tag=$1
checksums=$2
script_directory=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
template=${3:-"$script_directory/../packaging/homebrew/agentport.rb.tmpl"}

if ! printf '%s\n' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
	if printf '%s\n' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+-'; then
		fail "pre-release $tag is not published to Homebrew; only stable releases are"
	fi
	fail "version must look like vX.Y.Z, got '$tag'"
fi
version=${tag#v}

[ -f "$checksums" ] || fail "checksums file not found: $checksums"
[ -f "$template" ] || fail "template not found: $template"

# checksum_for PLATFORM prints the SHA-256 that checksums.txt lists for the
# archive of PLATFORM (for example darwin_arm64), or fails.
checksum_for() {
	asset="agentport_${tag}_$1.tar.gz"
	sha=$(awk -v asset="$asset" '
		{
			name = $2
			sub(/^\*/, "", name)
			if (name == asset) {
				print $1
				exit
			}
		}' "$checksums" | tr '[:upper:]' '[:lower:]')
	if [ -z "$sha" ]; then
		fail "no checksum for $asset in $checksums"
	fi
	if ! printf '%s\n' "$sha" | grep -Eq '^[0-9a-f]{64}$'; then
		fail "checksum for $asset is not a SHA-256 digest"
	fi
	printf '%s\n' "$sha"
}

sha_darwin_arm64=$(checksum_for darwin_arm64)
sha_darwin_amd64=$(checksum_for darwin_amd64)
sha_linux_arm64=$(checksum_for linux_arm64)
sha_linux_amd64=$(checksum_for linux_amd64)

rendered=$(awk \
	-v version="$version" \
	-v darwin_arm64="$sha_darwin_arm64" \
	-v darwin_amd64="$sha_darwin_amd64" \
	-v linux_arm64="$sha_linux_arm64" \
	-v linux_amd64="$sha_linux_amd64" '
	{
		gsub(/@VERSION@/, version)
		gsub(/@SHA256_DARWIN_ARM64@/, darwin_arm64)
		gsub(/@SHA256_DARWIN_AMD64@/, darwin_amd64)
		gsub(/@SHA256_LINUX_ARM64@/, linux_arm64)
		gsub(/@SHA256_LINUX_AMD64@/, linux_amd64)
		print
	}' "$template")

# A placeholder the template uses but this script does not fill would ship in
# the formula; refuse instead.
if printf '%s\n' "$rendered" | grep -Eq '@[A-Z0-9_]+@'; then
	fail "the template has a placeholder this script does not fill"
fi

printf '%s\n' "$rendered"
