#!/bin/sh
# Tests the authenticity checks of scripts/install.sh without touching the
# network: curl and gh are replaced by stubs that serve local fixtures.
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
installer="$root/scripts/install.sh"
fixtures="$root/scripts/testdata"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT HUP INT TERM

failures=0
pass() { printf 'ok   %s\n' "$1"; }
fail() {
  printf 'FAIL %s\n' "$1"
  failures=$((failures + 1))
}
expect() { # expect <description> <command...>: passes when the command succeeds
  description="$1"
  shift
  if "$@"; then pass "$description"; else fail "$description"; fi
}

# --- 1. statement_matches against the real GitHub response for v0.7.1-rc.1 ---

real_digest="5232f1dfbfac70b8919c4dac7681441fa2ec0c05ec608ada2c7d9d9256a1ee71"
real_payload="$(tr -d '\n' <"$fixtures/attestation-v0.7.1-rc.1.json" \
  | grep -o '"payload"[[:space:]]*:[[:space:]]*"[^"]*"' | head -n 1 | sed 's/.*:[[:space:]]*"\(.*\)"/\1/')"

matches() { # matches <digest> <repository> <version>
  (
    digest_under_test="$1" repository_under_test="$2" version_under_test="$3"
    # A sourced script inherits the positional parameters, which its own
    # option parsing would reject.
    set --
    export AGENTPORT_INSTALLER_LIB=1
    # shellcheck disable=SC1090
    . "$installer"
    # The sourced installer reads these variables, which shellcheck cannot see.
    # shellcheck disable=SC2034
    actual_checksum="$digest_under_test" repository="$repository_under_test" requested_version="$version_under_test"
    printf '%s' "$real_payload" | decode_base64 | statement_matches
  )
}

# The fixture is a real attestation of the AgentClip release it was forked from,
# so its repository is wendellrocha/agentclip; it must not match agentport's.
expect "real payload matches its archive, repository and tag" matches "$real_digest" wendellrocha/agentclip v0.7.1-rc.1
if matches 0000000000000000000000000000000000000000000000000000000000000000 wendellrocha/agentclip v0.7.1-rc.1; then fail "real payload refused for another digest"; else pass "real payload refused for another digest"; fi
if matches "$real_digest" wendellrocha/agentclip v0.7.1; then fail "real payload refused for another tag"; else pass "real payload refused for another tag"; fi
if matches "$real_digest" attacker/agentport v0.7.1-rc.1; then fail "real payload refused for another repository"; else pass "real payload refused for another repository"; fi
if matches "$real_digest" RafaelB13/agentport v0.7.1-rc.1; then fail "real payload refused for the agentport repository"; else pass "real payload refused for the agentport repository"; fi

# A real attestation of agentport cannot be made here, so a synthetic statement
# in the same format stands in for one: it must match RafaelB13/agentport only.
synthetic_statement='{"subject":[{"name":"agentport_v0.7.1-rc.1_linux_amd64.tar.gz","digest":{"sha256":"'"$real_digest"'"}}],"predicate":{"buildDefinition":{"externalParameters":{"workflow":{"ref":"refs/tags/v0.7.1-rc.1","repository":"https://github.com/RafaelB13/agentport","path":".github/workflows/release.yml"}}}}}'
synthetic_matches() { # synthetic_matches <digest> <repository> <version>
  (
    digest_under_test="$1" repository_under_test="$2" version_under_test="$3"
    set --
    export AGENTPORT_INSTALLER_LIB=1
    # shellcheck disable=SC1090
    . "$installer"
    # shellcheck disable=SC2034
    actual_checksum="$digest_under_test" repository="$repository_under_test" requested_version="$version_under_test"
    printf '%s' "$synthetic_statement" | statement_matches
  )
}
expect "synthetic agentport statement matches its archive, repository and tag" synthetic_matches "$real_digest" RafaelB13/agentport v0.7.1-rc.1
expect "synthetic statement matches when the repository differs only in case" synthetic_matches "$real_digest" rafaelb13/agentport v0.7.1-rc.1
if synthetic_matches "$real_digest" RafaelB13/agentport v0.7.1; then fail "synthetic statement refused for another tag"; else pass "synthetic statement refused for another tag"; fi
if synthetic_matches "$real_digest" wendellrocha/agentclip v0.7.1-rc.1; then fail "synthetic statement refused for another repository"; else pass "synthetic statement refused for another repository"; fi

# --- 2. the whole installer with stubbed curl and gh ---

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$(uname -m)" in x86_64 | amd64) arch=amd64 ;; *) arch=arm64 ;; esac
version="v0.7.1"
asset="agentport_${version}_${os}_${arch}.tar.gz"
repository="example/agentport"

mkdir -p "$work/release/agentport_${version}_${os}_${arch}" "$work/bin" "$work/tools"

# The installer runs with a PATH that holds only the tools it needs plus the
# stubs, so a real gh on this machine can never leak into a scenario.
for tool in sh env tar gzip awk uname mktemp rm mkdir install sed head tr grep cat cp mv chmod base64 sha256sum shasum openssl basename dirname ls wc sort cut; do
  found="$(command -v "$tool" 2>/dev/null || true)"
  case "$found" in /*) ln -s "$found" "$work/tools/$tool" ;; esac
done
printf '#!/bin/sh\necho %s\n' "$version" >"$work/release/agentport_${version}_${os}_${arch}/agentport"
chmod +x "$work/release/agentport_${version}_${os}_${arch}/agentport"
tar -C "$work/release" -czf "$work/archive.tar.gz" "agentport_${version}_${os}_${arch}"
if command -v sha256sum >/dev/null 2>&1; then
  digest="$(sha256sum "$work/archive.tar.gz" | awk '{ print $1 }')"
else
  digest="$(shasum -a 256 "$work/archive.tar.gz" | awk '{ print $1 }')"
fi
printf '%s  %s\n' "$digest" "$asset" >"$work/checksums.txt"
printf 'gh bundle' >"$work/attestation.jsonl"

# api_body <digest> <ref> <repository> <path> writes a GitHub attestations response.
api_body() {
  statement="{\"subject\":[{\"name\":\"x\",\"digest\":{\"sha256\":\"$1\"}}],\"predicate\":{\"buildDefinition\":{\"externalParameters\":{\"workflow\":{\"ref\":\"$2\",\"repository\":\"https://github.com/$3\",\"path\":\"$4\"}}}}}"
  encoded="$(printf '%s' "$statement" | base64 | tr -d '\n')"
  printf '{"attestations":[{"bundle":{"dsseEnvelope":{"payload":"%s"}}}]}' "$encoded"
}

cat >"$work/bin/curl" <<'STUB'
#!/bin/sh
out=""
write_status=""
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then out="$arg"; fi
  if [ "$prev" = "-w" ]; then write_status="$arg"; fi
  prev="$arg"
  url="$arg"
done
prev=""
serve() { cp "$1" "$out"; }
case "$url" in
  */attestation.jsonl)
    [ "${HAVE_BUNDLE:-1}" = "1" ] || exit 22
    serve "$WORK/attestation.jsonl"
    ;;
  */checksums.txt) serve "$WORK/checksums.txt" ;;
  */agentport_*.tar.gz) serve "$WORK/archive.tar.gz" ;;
  https://api.github.com/*/attestations/sha256:*)
    echo "$url" >>"$WORK/api.log"
    [ "${API_UNREACHABLE:-0}" = "1" ] && exit 7
    if [ "${API_STATUS:-200}" = "200" ]; then cp "$WORK/api.json" "$out"; else echo '{}' >"$out"; fi
    [ -n "$write_status" ] && printf '%s' "${API_STATUS:-200}"
    exit 0
    ;;
  *) echo "curl stub: unexpected URL $url" >&2; exit 22 ;;
esac
STUB
cat >"$work/bin/gh" <<'STUB'
#!/bin/sh
echo "$*" >>"$WORK/gh.log"
case "$*" in
  "attestation verify --help") [ "${GH_SUPPORTS:-1}" = "1" ] ;;
  "attestation verify "*) exit "${GH_VERIFY_RC:-0}" ;;
  *) exit 1 ;;
esac
STUB
chmod +x "$work/bin/curl" "$work/bin/gh"

# run_install <description> <expected exit> [VAR=value ...] runs a fresh install.
run_install() {
  description="$1"
  expected="$2"
  shift 2
  rm -rf "$work/install" "$work/api.log" "$work/gh.log"
  : >"$work/api.log"
  : >"$work/gh.log"
  set +e
  env WORK="$work" PATH="$work/bin:$work/tools" AGENTPORT_REPOSITORY="$repository" AGENTPORT_INSTALL_DIR="$work/install" "$@" \
    sh "$installer" --version "$version" >"$work/out.log" 2>&1
  code=$?
  set -e
  if [ "$code" = "$expected" ]; then pass "$description"; else
    fail "$description (exit $code, want $expected)"
    sed 's/^/     | /' "$work/out.log"
  fi
}
installed() { [ -x "$work/install/agentport" ]; }
gh_ran_verify() { grep -q '^attestation verify /' "$work/gh.log"; }
api_was_queried() { [ -s "$work/api.log" ]; }
good_api() { api_body "$digest" "refs/tags/$version" "$repository" ".github/workflows/release.yml" >"$work/api.json"; }

good_api
run_install "gh present and accepting: installs" 0 GH_VERIFY_RC=0
expect "  the binary was installed" installed
expect "  gh verified the downloaded archive" gh_ran_verify
if api_was_queried; then fail "  the API is not consulted when gh succeeded"; else pass "  the API is not consulted when gh succeeded"; fi

run_install "gh rejecting the archive: refuses, even if the API would accept" 1 GH_VERIFY_RC=1
if installed; then fail "  nothing was installed"; else pass "  nothing was installed"; fi
if api_was_queried; then fail "  no fallback to the weaker API check"; else pass "  no fallback to the weaker API check"; fi

run_install "gh without attestation support: falls back to the API" 0 GH_SUPPORTS=0
expect "  the API was queried for the archive digest" grep -q "sha256:$digest" "$work/api.log"
expect "  the binary was installed" installed

run_install "release without the bundle asset: falls back to the API" 0 HAVE_BUNDLE=0
expect "  the API was queried" api_was_queried

rm -f "$work/bin/gh"
run_install "no gh installed and a matching attestation: installs" 0
expect "  the binary was installed" installed
expect "  the API was queried for the archive digest" grep -q "sha256:$digest" "$work/api.log"

api_body "$digest" "refs/heads/main" "$repository" ".github/workflows/release.yml" >"$work/api.json"
run_install "attestation made from a branch: refuses" 1
api_body "$digest" "refs/tags/v9.9.9" "$repository" ".github/workflows/release.yml" >"$work/api.json"
run_install "attestation made for another tag: refuses" 1
api_body "$digest" "refs/tags/$version" "attacker/agentport" ".github/workflows/release.yml" >"$work/api.json"
run_install "attestation made in another repository: refuses" 1
api_body "$digest" "refs/tags/$version" "$repository" ".github/workflows/evil.yml" >"$work/api.json"
run_install "attestation made by another workflow: refuses" 1
api_body "1111111111111111111111111111111111111111111111111111111111111111" "refs/tags/$version" "$repository" ".github/workflows/release.yml" >"$work/api.json"
run_install "attestation covering another file: refuses" 1
if installed; then fail "  nothing was installed"; else pass "  nothing was installed"; fi

good_api
run_install "no attestation (HTTP 404): refuses" 1 API_STATUS=404
run_install "API rate limited (HTTP 403): refuses and explains the opt-out" 1 API_STATUS=403
expect "  the message mentions AGENTPORT_SKIP_ATTESTATION" grep -q AGENTPORT_SKIP_ATTESTATION "$work/out.log"
run_install "API unreachable: refuses" 1 API_UNREACHABLE=1

run_install "explicit opt-out installs with only the SHA-256 check" 0 API_STATUS=404 AGENTPORT_SKIP_ATTESTATION=1
expect "  the opt-out was announced" grep -q 'AGENTPORT_SKIP_ATTESTATION=1' "$work/out.log"
if api_was_queried; then fail "  the API is not consulted after the opt-out"; else pass "  the API is not consulted after the opt-out"; fi

# A release that predates attestations cannot have one. Agentport's first
# release, v0.1.0, is attested, so only versions below it are exempt.
version="v0.0.9"
asset="agentport_${version}_${os}_${arch}.tar.gz"
mkdir -p "$work/release/agentport_${version}_${os}_${arch}"
cp "$work/release/agentport_v0.7.1_${os}_${arch}/agentport" "$work/release/agentport_${version}_${os}_${arch}/agentport"
tar -C "$work/release" -czf "$work/archive.tar.gz" "agentport_${version}_${os}_${arch}"
if command -v sha256sum >/dev/null 2>&1; then digest="$(sha256sum "$work/archive.tar.gz" | awk '{ print $1 }')"; else digest="$(shasum -a 256 "$work/archive.tar.gz" | awk '{ print $1 }')"; fi
printf '%s  %s\n' "$digest" "$asset" >"$work/checksums.txt"
run_install "release before v0.1.0 installs with a warning" 0 API_STATUS=404
expect "  the warning says it predates attestations" grep -q 'predates build attestations' "$work/out.log"

# The language is chosen by AGENTPORT_LANG alone: Portuguese only when asked,
# and English for anything else, including a system locale.
run_install "the same install in Brazilian Portuguese" 0 API_STATUS=404 AGENTPORT_LANG=pt-BR
expect "  the warning is in Portuguese" grep -q 'anterior aos atestados' "$work/out.log"
expect "  the PATH instruction is in Portuguese" grep -q 'ao PATH e abra um novo terminal' "$work/out.log"
expect "  and no English status line is left" sh -c '! grep -q "Downloading agentport\|Version found\|Binary available\|to PATH\|Installed version\|predates" "$1"' _ "$work/out.log"
run_install "an unsupported language is English" 0 API_STATUS=404 AGENTPORT_LANG=fr LANG=pt_BR.UTF-8 LC_ALL=pt_BR.UTF-8
expect "  the system locale is not consulted" grep -q 'predates build attestations' "$work/out.log"
expect "  the status lines are English" grep -q 'Downloading agentport' "$work/out.log"
expect "  the PATH instruction is English" grep -q 'to PATH, then open a new terminal' "$work/out.log"

# v0.1.0 is the first attested release: it must be verified, never skipped.
version="v0.1.0"
asset="agentport_${version}_${os}_${arch}.tar.gz"
mkdir -p "$work/release/agentport_${version}_${os}_${arch}"
printf '#!/bin/sh\necho %s\n' "$version" >"$work/release/agentport_${version}_${os}_${arch}/agentport"
chmod +x "$work/release/agentport_${version}_${os}_${arch}/agentport"
tar -C "$work/release" -czf "$work/archive.tar.gz" "agentport_${version}_${os}_${arch}"
if command -v sha256sum >/dev/null 2>&1; then digest="$(sha256sum "$work/archive.tar.gz" | awk '{ print $1 }')"; else digest="$(shasum -a 256 "$work/archive.tar.gz" | awk '{ print $1 }')"; fi
printf '%s  %s\n' "$digest" "$asset" >"$work/checksums.txt"
run_install "v0.1.0 without an attestation: refuses, no warning" 1 API_STATUS=404 GH_SUPPORTS=0
expect "  the API was queried" api_was_queried
if grep -q 'predates build attestations' "$work/out.log"; then fail "  v0.1.0 is not treated as predating attestations"; else pass "  v0.1.0 is not treated as predating attestations"; fi
good_api
run_install "v0.1.0 with a matching attestation: installs" 0 GH_SUPPORTS=0
expect "  the binary was installed" installed

# A checksum mismatch is still fatal, before any attestation is consulted.
printf '%s  %s\n' "0000000000000000000000000000000000000000000000000000000000000000" "$asset" >"$work/checksums.txt"
run_install "SHA-256 mismatch: refuses" 1 AGENTPORT_SKIP_ATTESTATION=1

# --- 3. a fake release served from local files, through AGENTPORT_DOWNLOAD_BASE ---
# No network: file:// URLs are read by the real curl, and GITHUB_TOKEN is unset.
# Attestations are skipped because there is no GitHub release to attest.

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{ print $1 }'
  else
    shasum -a 256 "$1" | awk '{ print $1 }'
  fi
}

offline_version="v9.8.7"
offline_asset="agentport_${offline_version}_${os}_${arch}.tar.gz"
offline_release="$work/offline-release"
offline_stage="$work/offline-stage/agentport_${offline_version}_${os}_${arch}"
mkdir -p "$offline_release" "$offline_stage"
printf '#!/bin/sh\necho %s\n' "$offline_version" >"$offline_stage/agentport"
chmod +x "$offline_stage/agentport"
tar -C "$work/offline-stage" -czf "$offline_release/$offline_asset" "agentport_${offline_version}_${os}_${arch}"
offline_digest="$(sha256_of "$offline_release/$offline_asset")"
printf '%s  %s\n' "$offline_digest" "$offline_asset" >"$offline_release/checksums.txt"

# run_offline <description> <expected exit> [VAR=value ...] runs a fresh install
# of the offline release; the variables are exported on top of the defaults.
run_offline() {
  description="$1"
  expected="$2"
  shift 2
  rm -rf "$work/offline-install"
  set +e
  (
    unset GITHUB_TOKEN
    export AGENTPORT_INSTALL_DIR="$work/offline-install"
    export AGENTPORT_DOWNLOAD_BASE="file://$offline_release"
    export AGENTPORT_SKIP_ATTESTATION=1
    # shellcheck disable=SC2163 # Arguments are NAME=value pairs; exporting them on purpose.
    for assignment in "$@"; do export "$assignment"; done
    exec sh "$installer" --version "$offline_version"
  ) >"$work/offline.log" 2>&1
  code=$?
  set -e
  if [ "$code" = "$expected" ]; then pass "$description"; else
    fail "$description (exit $code, want $expected)"
    sed 's/^/     | /' "$work/offline.log"
  fi
}

run_offline "offline release installs and reports its version" 0
expect "  the binary was installed" test -x "$work/offline-install/agentport"
expect "  the installer printed the version" grep -q "$offline_version" "$work/offline.log"

printf '%s  %s\n' "0000000000000000000000000000000000000000000000000000000000000000" "$offline_asset" >"$offline_release/checksums.txt"
run_offline "offline checksum mismatch: refuses" 1
if [ -e "$work/offline-install/agentport" ]; then fail "  nothing was installed"; else pass "  nothing was installed"; fi
printf '%s  %s\n' "$offline_digest" "agentport_${offline_version}_plan9_amd64.tar.gz" >"$offline_release/checksums.txt"
run_offline "offline checksums without the archive: refuses" 1
expect "  the message names the missing checksum" grep -q "Checksum for $offline_asset was not found" "$work/offline.log"
printf '%s  %s\n' "$offline_digest" "$offline_asset" >"$offline_release/checksums.txt"

run_offline "dry run prints the resolved locations and installs nothing" 0 AGENTPORT_DRY_RUN=1
expect "  the archive location is printed" grep -q "file://$offline_release/$offline_asset" "$work/offline.log"
if [ -e "$work/offline-install" ]; then fail "  the install directory was not created"; else pass "  the install directory was not created"; fi

# A GitHub release lists its assets by API URL; the lookup must pick the right one.
mkdir -p "$work/lookup"
cat >"$work/lookup/release.json" <<'JSON'
{"tag_name":"v9.8.7","assets":[{"url":"https://api.github.com/repos/example/agentport/releases/assets/111","id":111,"node_id":"A1","name":"checksums.txt","uploader":{"login":"bot"}},{"url":"https://api.github.com/repos/example/agentport/releases/assets/222","id":222,"node_id":"B2","name":"agentport_v9.8.7_linux_amd64.tar.gz","uploader":{"login":"bot"}}]}
JSON
asset_lookup() { # asset_lookup NAME prints the API URL of NAME from the fixture
  wanted_asset="$1"
  (
    set --
    export AGENTPORT_INSTALLER_LIB=1
    # shellcheck disable=SC1090
    . "$installer"
    # shellcheck disable=SC2034
    temporary_directory="$work/lookup"
    asset_api_url "$wanted_asset"
  )
}
expect "the checksums asset is found by its API URL" test "$(asset_lookup checksums.txt)" = "https://api.github.com/repos/example/agentport/releases/assets/111"
expect "the archive asset is found by its API URL" test "$(asset_lookup agentport_v9.8.7_linux_amd64.tar.gz)" = "https://api.github.com/repos/example/agentport/releases/assets/222"
expect "an absent asset has no URL" test -z "$(asset_lookup agentport_v9.8.7_plan9_amd64.tar.gz)"

echo
if [ "$failures" -ne 0 ]; then
  echo "$failures check(s) failed" >&2
  exit 1
fi
echo "all installer checks passed"
