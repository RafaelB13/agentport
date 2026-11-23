# agentport

[English](README.md) | **Português (Brasil)** | [Español](README.es.md) | [繁體中文](README.zh-Hant.md)

agentport vai permitir que um agente de código rodando em um servidor SSH use as
portas da sua máquina, com a sua autorização: ver o que está escutando e abrir ou
fechar encaminhamentos pelo túnel SSH que já existe. Ele mantém um bridge HTTP
local, um túnel SSH reverso e um servidor MCP por `stdio`, registrado
automaticamente nos agentes.

> **Em andamento.** Hoje o projeto ainda traz a base herdada do AgentClip: o
> Companion, o túnel, o registro nos agentes e os releases já funcionam, mas as
> ferramentas de portas ainda não existem. O servidor MCP expõe apenas
> `agentport_status`.

## Instalação

No macOS ou Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/rafaelb13/agentport/main/scripts/install.sh | sh
```

No Windows, execute no PowerShell:

```powershell
irm https://raw.githubusercontent.com/rafaelb13/agentport/main/scripts/install.ps1 | iex
```

Os scripts detectam a plataforma, consultam a release mais recente, verificam
o SHA-256 e a autenticidade do build (veja abaixo) e inspecionam a versão já
instalada. Eles baixam e atualizam somente
quando a versão encontrada é mais nova; repetir o instalador é seguro. Para uma
versão específica, use `--version vX.Y.Z` no instalador POSIX ou
`-Version vX.Y.Z` no PowerShell. Os artefatos e checksums também estão nas
[GitHub Releases](https://github.com/rafaelb13/agentport/releases).

### Verificar a autenticidade de um release

O `checksums.txt` de cada release é assinado com o
[cosign](https://github.com/sigstore/cosign) (assinatura keyless pela identidade
do workflow de release), e os artefatos têm atestado de proveniência do GitHub.
Para verificar manualmente, baixe o arquivo do release, o `checksums.txt` e o
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
# macOS (não tem sha256sum nem --ignore-missing)
ARCH=$(uname -m | sed s/x86_64/amd64/) # arm64 or amd64
grep " agentport_${VERSION}_darwin_${ARCH}.tar.gz$" checksums.txt | shasum -a 256 --check

gh attestation verify "agentport_${VERSION}_linux_amd64.tar.gz" \
  --repo rafaelb13/agentport \
  --cert-identity "$IDENTITY"
```

A identidade exige o ref exato da tag do release, então uma execução do workflow
a partir de outra branch não é aceita.

O `gh attestation verify` sem `--bundle` consulta o GitHub e exige `gh auth login`.
Para verificar sem login, baixe também o `attestation.jsonl` da release e acrescente
`--bundle attestation.jsonl`.

### Verificação automática

A partir da `v0.7.1-rc.1`, os instaladores e o `agentport upgrade` confirmam,
além do SHA-256, que o arquivo baixado foi produzido pelo workflow de release
deste repositório na tag que está sendo instalada. Se a confirmação falhar, a
instalação é recusada.

- **`install.sh` e `install.ps1`:** usam `gh attestation verify` com o
  `attestation.jsonl` da release, que dispensa `gh auth login`, quando o `gh`
  está instalado. Se o `gh` **rejeitar** o arquivo, a instalação falha, sem
  tentar um método mais fraco. Sem `gh`, ou se a release não tiver o bundle,
  consultam a API de atestados do GitHub.
- **`agentport upgrade`:** consulta a API de atestados do GitHub.
- **Servidores (`agentport setup` e `agentport upgrade`):** esta máquina baixa o
  binário da plataforma do servidor, faz a mesma verificação do `upgrade` e o
  envia pela conexão SSH. Nenhum instalador é baixado para o servidor: ele só
  executa uma sequência curta e fixa de comandos que confere se o SHA-256 do que
  recebeu é o do binário verificado e move o arquivo para o lugar; um envio
  truncado nunca substitui o binário que já funciona. Um servidor que já tem a
  mesma versão, ou uma mais nova, não é alterado.

A consulta à API confirma que o repositório tem um atestado para exatamente o
SHA-256 do arquivo, feito pelo `release.yml` na tag da versão, o que impede a
troca de arquivos de uma release. Ela confia no TLS e na API do GitHub e **não
verifica a assinatura** em si. Para a verificação criptográfica completa, instale
o `gh` ou use os comandos manuais acima.

Se a API estiver fora do ar ou com o limite de requisições estourado, a
instalação é recusada em vez de seguir sem verificar. Para instalar mesmo assim,
apenas com o SHA-256, defina `AGENTPORT_SKIP_ATTESTATION=1`. No `agentport setup` e no
`agentport upgrade` a variável vale também para os binários enviados aos
servidores. Versões anteriores
à `v0.7.1-rc.1` não têm atestado e são instaladas só com o SHA-256, com um aviso.

## Início rápido

Instale e configure o servidor em uma única chamada:

```bash
agentport setup bastion-m2 --profile m2
```

O comando instala a versão compatível no servidor Linux/macOS via SSH, detecta
todos os harnesses suportados já instalados, registra automaticamente a
integração agentport em cada um deles, cria o perfil local e inicia o Companion.
Quando o SSH ainda aceita apenas senha, o `setup` pede essa senha uma vez para
instalar uma chave Ed25519 privada do agentport; os túneis seguintes não pedem
senha. Se uma chave SSH já autentica o destino, o agentport não a altera.
Em seguida:

```bash
ssh bastion-m2
codex
```

No agente, peça por exemplo: `Verifique o status do agentport.` O agente chama a
ferramenta `agentport_status`, que informa se o túnel até o Companion está
conectado e se há uma versão nova disponível.

## Companion, background e reinicializações

`agentport setup` inicia o Companion local em background (salvo com
`--no-start`); `agentport companion start <perfil>` faz o mesmo manualmente.
O processo mantém o bridge local e abre o túnel SSH
reverso para o destino salvo no perfil. A view e o estado atual podem ser
consultados sem prender o terminal:

```bash
agentport companion status m2
agentport companion open m2
agentport companion stop m2
```

O Companion verifica a release estável no início e mostra um aviso na página
local e na ferramenta MCP `agentport_update_status` quando houver atualização.
Para atualizar o executável local e todos os perfis remotos salvos, use:

```bash
agentport upgrade
```

O comando valida o SHA-256 publicado e o atestado de build da release, mantém
parados os perfis que já estavam
parados e reinicia somente os Companions que estavam ativos.

`agentport companion open m2` abre uma página web local, protegida por token,
com estado do túnel, aviso de versão, último erro e um botão
para parar o Companion. Ela escuta somente em `127.0.0.1`. Veja todos os
detalhes em [Referência de comandos](COMMANDS.md#página-web-do-companion).

O perfil — destino SSH, porta remota e token de pareamento — fica salvo no
host. O processo em execução, o bridge e o túnel não. Por padrão o agentport
**não instala um serviço de inicialização automática**, então, se o **host**
for reiniciado, inicie o perfil desejado novamente:

```bash
agentport companion start m2
```

Para o Companion subir sozinho quando você entrar na sessão, ative a
inicialização automática do perfil. O agentport usa o que cada sistema já tem:
um LaunchAgent no macOS, uma unidade de usuário do systemd no Linux e uma tarefa
do Agendador de Tarefas no Windows.

```bash
agentport companion autostart enable m2
agentport companion autostart status m2
agentport companion autostart disable m2
```

Desativar remove o serviço e para o Companion que ele iniciou; um Companion que
você iniciou por conta própria continua rodando. Se o Companion já estiver
rodando quando o login iniciar o serviço, o serviço não abre um segundo.

Se apenas o **servidor remoto** cair ou for reiniciado enquanto o Companion
local continua rodando, ele tenta restabelecer o túnel SSH automaticamente,
com espera progressiva de 1 a 30 segundos (que recomeça em 1 segundo depois de
um túnel que ficou de pé por pelo menos um minuto). Quando a conexão voltar, o mesmo
perfil e a mesma porta remota voltam a ser usados.

Por padrão, `setup`, `pair` e `connect` usam todos os harnesses suportados que
encontrarem. A instalação/registro da integração é automática: não é preciso
editar JSON nem criar a extensão do Pi manualmente. Use `--agent` para limitar
a configuração a um deles. Para remover apenas a integração do agentport de um
harness, use `uninstall`; o CLI/harness e o perfil local continuam intactos:

```bash
agentport setup bastion-m2 --profile m2 --agent claude
agentport connect m2
agentport uninstall m2 --agent gemini
```

## Compatibilidade de agentes

agentport integra-se ao **harness** — o programa que executa o agente e chama
ferramentas MCP —, não ao modelo escolhido dentro dele. A tabela separa o que
a versão atual configura automaticamente do que ainda é planejamento.

| Harness | Status | Integração |
| --- | --- | --- |
| Codex | Suportado | Detectado e configurado automaticamente quando instalado |
| Claude Code | Suportado | Detectado e configurado automaticamente quando instalado |
| Gemini CLI | Suportado | Detectado e configurado automaticamente quando instalado |
| AGY / Antigravity CLI | Suportado | agentport cria/atualiza automaticamente o MCP global |
| OpenCode | Suportado | agentport cria/atualiza automaticamente o MCP local global |
| Pi Coding Agent | Suportado | agentport instala automaticamente uma extensão global |
| Outro cliente MCP | Manual | Configure `agentport mcp` e as variáveis do perfil manualmente |

| Modelo ou provedor | Status direto | Como usar |
| --- | --- | --- |
| DeepSeek | Não aplicável | Use por meio de um harness compatível, como Pi ou OpenCode |
| MiMo | Não aplicável | Use por meio de um harness MCP; não há um adaptador de modelo direto |

Os adaptadores planejados nunca instalarão clientes de terceiros sem ação
explícita do usuário, nem gravarão o token do agentport em configurações de
projeto versionáveis.

Os adaptadores nunca instalam harnesses de terceiros. `setup`, `pair` e
`connect` detectam os seis CLIs suportados e configuram somente os que já estão
instalados; `--agent` permite escolher um deles.

## Segurança e limites

- O bridge escuta somente em `127.0.0.1`; o servidor o alcança apenas pelo
  túnel SSH ativo.
- O servidor remoto precisa ser confiável: o agente nele alcança o bridge somente
  com o token de sessão que o Companion registra.

## Idioma

Por padrão, a saída dos comandos, o `agentport help` e a página do Companion
estão em inglês. Para português do Brasil, defina `AGENTPORT_LANG=pt-BR` (vale
também para os instaladores); a página do Companion também aceita `?lang=pt-BR`
e segue o idioma do navegador. As mensagens de erro técnicas continuam em
inglês. Veja [Idioma](COMMANDS.md#idioma) na referência de comandos.

```bash
AGENTPORT_LANG=pt-BR agentport help
```

## Documentação

- [Referência de comandos](COMMANDS.md): todos os comandos, opções, página web
  do Companion e comandos internos.
- [Desenvolvimento](DEVELOPMENT.md): build local, testes, validação de CSV e
  processo de release.
- [Segurança](SECURITY.md): como reportar vulnerabilidades e verificar releases.
- [Contribuindo](CONTRIBUTING.md): escopo de contribuições, testes e pull
  requests.

## Estado atual

O agentport está em desenvolvimento. Hoje estão prontos o bridge local
autenticado, a sessão SSH reversa persistente, o perfil pareado, o dashboard do
Companion e a ferramenta MCP `agentport_status`. As ferramentas de portas serão
acrescentadas nas próximas etapas. Os adaptadores automáticos atuais são Codex,
Claude Code, Gemini CLI, AGY / Antigravity CLI, OpenCode e Pi Coding Agent.

## Créditos

O agentport nasceu como fork do [AgentClip](https://github.com/wendellrocha/agentclip), de [Wendell Rocha](https://github.com/wendellrocha), distribuído sob a licença MIT. Reaproveitamos a base do Companion, o túnel SSH reverso, o servidor MCP por stdio, o registro automático nos agentes e o pipeline de releases verificáveis. Obrigado ao projeto original. 💙

## Licença

Distribuído sob a [licença MIT](LICENSE).
