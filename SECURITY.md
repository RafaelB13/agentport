# Política de segurança

**Português** · [English](#security-policy)

O agentport conecta um agente remoto à máquina local por um túnel autenticado.
Esse canal precisa ser seguro. Levamos relatos de segurança a sério.

## Como reportar uma vulnerabilidade

Use o reporte privado de vulnerabilidades do GitHub:
<https://github.com/rafaelb13/agentport/security/advisories/new>

Não abra issues públicas, nem inclua tokens ou dados de servidores reais no
relato. Descreva o impacto, os passos para reproduzir e
a versão afetada (`agentport --version`).

Você pode esperar uma confirmação de recebimento em alguns dias. As correções
são publicadas em uma nova release e creditadas no advisory, se você quiser.

## Versões suportadas

Apenas a release mais recente recebe correções de segurança. Atualize com
`agentport upgrade`.

## Verificação de releases

Os releases têm `checksums.txt` assinado com cosign e atestado de proveniência.
Os instaladores e o `agentport upgrade` confirmam automaticamente o atestado a
partir da `v0.7.1-rc.1`. Veja "Verificar a autenticidade de um release" no
[README](README.md), que também descreve os limites dessa verificação.

Cada release também publica um SBOM CycloneDX por plataforma
(`agentport_vX.Y.Z_<os>_<arch>.cdx.json`) com os módulos compilados naquele
binário, coberto pelos mesmos checksums e pelo atestado. O workflow de release roda o `govulncheck` em cada binário antes de
assinar e é interrompido se encontrar uma vulnerabilidade conhecida.

## Escopo

Estão no escopo: autenticação do bridge e do Companion, túnel SSH, instaladores
e o processo de upgrade. Consulte também as
áreas sensíveis em [CONTRIBUTING.md](CONTRIBUTING.md).

---

# Security policy

agentport connects a remote agent to the local machine through an authenticated
tunnel, and that channel must stay secure. We take security reports seriously.

## How to report a vulnerability

Use GitHub's private vulnerability reporting:
<https://github.com/rafaelb13/agentport/security/advisories/new>

Please do not open public issues, and do not include tokens or data from real
servers in your report. Describe the impact, the steps to
reproduce and the affected version (`agentport --version`).

You can expect an acknowledgement within a few days. Fixes are published in a
new release and credited in the advisory, if you want.

## Supported versions

Only the latest release receives security fixes. Update with
`agentport upgrade`.

## Release verification

Releases carry a `checksums.txt` signed with cosign and a provenance
attestation. The installers and `agentport upgrade` confirm the attestation
automatically from `v0.7.1-rc.1` on. See "Verifying the authenticity of a
release" in the [English README](README.en.md), which also describes the limits
of that verification.

Each release also publishes one CycloneDX SBOM per platform
(`agentport_vX.Y.Z_<os>_<arch>.cdx.json`) listing the modules compiled into that
binary, covered by the same checksums and attestation. The release workflow scans every binary with `govulncheck`
before signing and stops if it finds a known vulnerability.

## Scope

In scope: authentication of the bridge and the Companion, the SSH tunnel, the
installers and the upgrade process. See also the
sensitive areas in [CONTRIBUTING.md](CONTRIBUTING.md) (in Portuguese).
