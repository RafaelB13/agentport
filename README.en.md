# agentport

[Português](README.md) · **English**

agentport will let a coding agent running on an SSH server use the ports of your
machine, with your authorization: see what is listening and open or close
forwards through the SSH tunnel that already exists. It runs a local HTTP bridge,
a reverse SSH tunnel and an MCP server over `stdio`, registered automatically in
the agents.

> **Work in progress.** The project still carries the AgentClip base: the
> Companion, the tunnel, the agent registration and the releases work, but the
> port tools do not exist yet. The MCP server exposes only `agentport_status`.

> The rest of the documentation (commands, development, contributing) is in
> Portuguese for now. The commands and options themselves are the same.

## Installation

On macOS or Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/rafaelb13/agentport/main/scripts/install.sh | sh
```

On Windows, run in PowerShell:

```powershell
irm https://raw.githubusercontent.com/rafaelb13/agentport/main/scripts/install.ps1 | iex
```

The scripts detect the platform, look up the latest release, verify its SHA-256
and the authenticity of the build (see below) and inspect the version that is
already installed. They download and update only when the version found is
newer; running the installer again is safe. For a specific version, use
`--version vX.Y.Z` with the POSIX installer or `-Version vX.Y.Z` with
PowerShell. Artifacts and checksums are also on
[GitHub Releases](https://github.com/rafaelb13/agentport/releases).

### Verifying the authenticity of a release

Each release's `checksums.txt` is signed with
[cosign](https://github.com/sigstore/cosign) (keyless, using the identity of the
release workflow), and the artifacts carry a GitHub provenance attestation. To
verify by hand, download the release file, `checksums.txt` and
`checksums.txt.bundle`:

```bash
VERSION=vX.Y.Z
IDENTITY="https://github.com/rafaelb13/agentport/.github/workflows/release.yml@refs/tags/${VERSION}"

cosign verify-blob \
  --bundle checksums.txt.bundle \
  --certificate-identity "$IDENTITY" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt

# Linux
sha256sum --check --ignore-missing checksums.txt
# macOS (it has neither sha256sum nor --ignore-missing)
ARCH=$(uname -m | sed s/x86_64/amd64/) # arm64 or amd64
grep " agentport_${VERSION}_darwin_${ARCH}.tar.gz$" checksums.txt | shasum -a 256 --check

gh attestation verify "agentport_${VERSION}_linux_amd64.tar.gz" \
  --repo rafaelb13/agentport \
  --cert-identity "$IDENTITY"
```

The identity requires the exact ref of the release tag, so a run of the workflow
from any other branch is not accepted.

`gh attestation verify` without `--bundle` queries GitHub and requires
`gh auth login`. To verify without logging in, also download the release's
`attestation.jsonl` and add `--bundle attestation.jsonl`.

### Automatic verification

From `v0.7.1-rc.1` on, the installers and `agentport upgrade` confirm, besides
the SHA-256, that the downloaded file was produced by this repository's release
workflow on the tag being installed. If the confirmation fails, the
installation is refused.

- **`install.sh` and `install.ps1`:** when `gh` is installed they use
  `gh attestation verify` with the release's `attestation.jsonl`, which needs no
  `gh auth login`. If `gh` **rejects** the file, the installation fails, without
  falling back to a weaker method. Without `gh`, or if the release has no
  bundle, they query the GitHub attestations API.
- **`agentport upgrade`:** queries the GitHub attestations API.
- **Servers (`agentport setup` and `agentport upgrade`):** this machine downloads
  the binary for the server's platform, verifies it the way `upgrade` does and
  sends it over the SSH connection. No installer is downloaded to the server: it
  only runs a short, fixed sequence of commands that checks the SHA-256 of what
  it received against the verified binary's and moves the file into place; a
  truncated transfer never replaces the binary that already works. A server that
  already has the same version, or a newer one, is left unchanged.

The API lookup confirms that the repository holds an attestation for exactly the
file's SHA-256, made by `release.yml` on the version's tag, which stops release
files from being swapped. It trusts TLS and the GitHub API and **does not verify
the signature** itself. For the full cryptographic verification, install `gh` or
use the manual commands above.

If the API is down or its rate limit is exhausted, the installation is refused
instead of proceeding unverified. To install anyway, with the SHA-256 check
only, set `AGENTPORT_SKIP_ATTESTATION=1`. With `agentport setup` and
`agentport upgrade` the variable also applies to the binaries sent to servers.
Versions before `v0.7.1-rc.1` have
no attestation and are installed with the SHA-256 only, with a warning.

## Quick start

Install and configure a server in a single call:

```bash
agentport setup bastion-m2 --profile m2
```

The command installs the matching version on the Linux/macOS server over SSH,
detects every supported harness that is already installed, registers the
agentport integration with each of them automatically, creates the local
profile and starts the Companion. When SSH still accepts only a password,
`setup` asks for it once to install a private agentport Ed25519 key; later
tunnels do not ask for a password. If an SSH key already authenticates the
destination, agentport leaves it alone. Then:

```bash
ssh bastion-m2
codex
```

In the agent, ask for example: `Check the agentport status.` The agent calls the
`agentport_status` tool, which reports whether the tunnel to the Companion is
connected and whether a newer version is available.

## Companion, background and restarts

`agentport setup` starts the local Companion in the background (unless you pass
`--no-start`); `agentport companion start <profile>` does the same by hand. The
process keeps the local bridge running and opens the
reverse SSH tunnel to the destination saved in the profile. The view and the
current state can be queried without tying up the terminal:

```bash
agentport companion status m2
agentport companion open m2
agentport companion stop m2
```

The Companion checks for the stable release at startup and shows a notice on the
local page and in the `agentport_update_status` MCP tool when an update exists.
To update the local executable and every saved remote profile, use:

```bash
agentport upgrade
```

The command validates the published SHA-256 and the release's build
attestation, keeps stopped the profiles that were already stopped and restarts
only the Companions that were active.

`agentport companion open m2` opens a local, token-protected web page with the
tunnel state, the update notice, the last error and a button
to stop the Companion. It listens on `127.0.0.1` only. See all the details in the
[Command reference](COMMANDS.md#página-web-do-companion) (in Portuguese).

The profile — SSH destination, remote port and pairing token — is saved on the
host. The running process, the bridge and the tunnel are not. By default
agentport **does not install an automatic startup service**, so if the **host**
restarts, start the profile you want again:

```bash
agentport companion start m2
```

To have the Companion start by itself when you log in, turn on autostart for the
profile. agentport uses what each system already has: a LaunchAgent on macOS, a
systemd user unit on Linux and a Task Scheduler task on Windows.

```bash
agentport companion autostart enable m2
agentport companion autostart status m2
agentport companion autostart disable m2
```

Turning it off removes the service and stops the Companion it started; one you
started yourself keeps running. If the Companion is already running when the
login starts the service, the service does not open a second one.

If only the **remote server** goes down or restarts while the local Companion
keeps running, it tries to re-establish the SSH tunnel automatically, backing
off progressively from 1 to 30 seconds (starting over at 1 second after a
tunnel that stayed up for at least a minute). When the connection returns, the same
profile and the same remote port are used again.

By default, `setup`, `pair` and `connect` use every supported harness they find.
Installing and registering the integration is automatic: you do not need to
edit JSON or create the Pi extension by hand. Use `--agent` to limit the
configuration to one of them. To remove only the agentport integration from a
harness, use `uninstall`; the harness CLI and the local profile stay intact:

```bash
agentport setup bastion-m2 --profile m2 --agent claude
agentport connect m2
agentport uninstall m2 --agent gemini
```

## Agent compatibility

agentport integrates with the **harness** — the program that runs the agent and
calls MCP tools — not with the model chosen inside it. The table separates what
the current version configures automatically from what is still planned.

| Harness | Status | Integration |
| --- | --- | --- |
| Codex | Supported | Detected and configured automatically when installed |
| Claude Code | Supported | Detected and configured automatically when installed |
| Gemini CLI | Supported | Detected and configured automatically when installed |
| AGY / Antigravity CLI | Supported | agentport creates/updates the global MCP automatically |
| OpenCode | Supported | agentport creates/updates the global local MCP automatically |
| Pi Coding Agent | Supported | agentport installs a global extension automatically |
| Another MCP client | Manual | Configure `agentport mcp` and the profile variables by hand |

| Model or provider | Direct status | How to use it |
| --- | --- | --- |
| DeepSeek | Not applicable | Use it through a compatible harness, such as Pi or OpenCode |
| MiMo | Not applicable | Use it through an MCP harness; there is no direct model adapter |

Planned adapters will never install third-party clients without an explicit user
action, nor write the agentport token into project settings that could be
committed.

Adapters never install third-party harnesses. `setup`, `pair` and `connect`
detect the six supported CLIs and configure only those that are already
installed; `--agent` lets you choose one of them.

## Security and limits

- The bridge listens on `127.0.0.1` only; the server reaches it only through the
  active SSH tunnel.
- The remote server must be trusted: the agent there reaches the bridge only
  with the session token that the Companion registers.

## Language

By default, command output, `agentport help` and the Companion page are in
English. For Brazilian Portuguese set `AGENTPORT_LANG=pt-BR` (it applies to the
installers too); the Companion page also accepts `?lang=pt-BR` and follows the
browser's language. Technical error messages stay in English. See
[Language](COMMANDS.md#idioma) in the command reference (in Portuguese).

```bash
AGENTPORT_LANG=pt-BR agentport help
```

## Documentation

- [Command reference](COMMANDS.md): every command, option, the Companion web
  page and the internal commands (in Portuguese).
- [Development](DEVELOPMENT.md): local build, tests, CSV validation and the
  release process (in Portuguese).
- [Security](SECURITY.md): how to report vulnerabilities and verify releases.
- [Contributing](CONTRIBUTING.md): the scope of contributions, tests and pull
  requests (in Portuguese).

## Current state

agentport is a work in progress. Ready today: the authenticated local bridge, the
persistent reverse SSH session, the paired profile, the Companion dashboard and
the `agentport_status` MCP tool. The port tools come in the next steps. The
current automatic adapters are Codex, Claude Code, Gemini CLI, AGY / Antigravity
CLI, OpenCode and Pi Coding Agent.

## Credits

agentport started as a fork of [AgentClip](https://github.com/wendellrocha/agentclip), by [Wendell Rocha](https://github.com/wendellrocha), distributed under the MIT license. We reuse the Companion base, the reverse SSH tunnel, the MCP server over stdio, the automatic registration in the agents and the verifiable release pipeline. Thanks to the original project. 💙

## License

Distributed under the [MIT license](LICENSE).
