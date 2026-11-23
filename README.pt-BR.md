# agentport

[English](README.md) | **Português (Brasil)** | [Español](README.es.md) | [繁體中文](README.zh-Hant.md)

O agentport permite que um agente de código rodando em um servidor SSH use as
portas da sua máquina, com a sua autorização. O agente pode ver o que está
escutando no servidor e pedir que uma porta seja aberta na sua máquina, pela
conexão SSH que você já tem. Você decide o que é aberto e quando é fechado.

> **Em andamento.** A base já funciona: o Companion, o túnel SSH reverso, o
> servidor MCP, o registro automático nos agentes, os instaladores e os releases
> verificáveis. As ferramentas de portas, o painel e o início automático ainda
> estão sendo construídos. A tabela de estado abaixo diz o que está pronto e o
> que está planejado.

## O que é

O agentport tem três partes:

- **O Companion**, um processo em segundo plano na sua máquina. Ele mantém um
  bridge local em `127.0.0.1`, segura um túnel SSH reverso para cada servidor
  pareado e reconecta com espera de 1 a 30 segundos quando a ligação cai.
- **O servidor MCP** (`agentport mcp`), que o agente de código inicia no
  servidor por `stdio`. O agentport o registra nos agentes que encontra lá.
- **O painel** (planejado), uma página na sua máquina em que você aprova, vê,
  edita e fecha as portas que o agente pediu.

Hoje você configura um servidor, conecta com `ssh` e usa o agente com a
ferramenta `agentport_status`, que informa o estado do túnel e se existe um
release mais novo. As ferramentas de portas são planejadas.

> **Espaço para captura de tela.** O painel e a página do Companion serão
> mostrados aqui quando o painel for entregue (fase F5).

O agentport é um fork do [AgentClip](https://github.com/wendellrocha/agentclip),
que faz o caminho contrário (da sua máquina para o servidor). Veja
[Créditos](#créditos).

## Estado

| Fase | O que é | Estado |
| --- | --- | --- |
| F1 | Base do projeto: fork do AgentClip, renomeado, clipboard removido. CLI, Companion, `setup`, servidor MCP com `agentport_status`, releases | ✓ Pronto |
| F3 | Motor de forward (`internal/forward`) e identificação de quem ocupa uma porta do host (`internal/hostport`) | ✓ Pronto no código; nenhum comando o usa ainda |
| F1b | Fórmula do Homebrew gerada pelo workflow de release, `agentport upgrade` ciente do Homebrew, estes quatro READMEs | Em andamento |
| F2 | Scanner de portas do servidor (`internal/portscan`), Linux e macOS, com filtros para daemons do sistema | ✓ Pronto no código; o comando `agentport ports` é planejado |
| F4 | Ferramentas MCP de portas (`ports_list`, `ports_forward`, `ports_unforward`, `forwards_list`), endpoints do bridge, aprovações, sessões | Planejado |
| F5 | Painel, início automático ao conectar (`LocalCommand`), notificações, diálogo de porta ocupada | Planejado |
| F5b | Terminal web para o servidor, no painel | Planejado |
| F6 | Host Windows: forwards sem ControlMaster | Planejado |
| Testes | Harness em Docker em `test/e2e`: um sshd real, forwards pelo ControlMaster, conflito de portas e o scanner no Linux | ✓ Pronto |
| F7 | Validação ponta a ponta em servidores reais, release `v0.1.0` | Planejado |

O que está marcado como *planejado* não existe na versão atual. Não conte com
isso ainda.

## Como funciona

```text
HOST (sua máquina)                           SERVIDOR (alcançado por SSH)

 Companion                                    agente (Claude Code, Codex, ...)
   bridge local em 127.0.0.1                    |  stdio, iniciado pelo agente
        ^                                       v
        |                                      agentport mcp
        +---- túnel SSH reverso (ssh -R) -------+
              uma conexão por servidor
```

1. `agentport setup` instala o agentport no servidor, registra o servidor MCP nos
   agentes que encontra lá, salva um **perfil** na sua máquina (o servidor, um
   token de pareamento e uma porta remota) e inicia o Companion.
2. O Companion abre um `ssh` para o servidor com um túnel reverso. Pelo túnel, o
   bridge fica acessível ao servidor, de modo que o `agentport mcp` o alcança em
   uma porta do loopback do servidor.
3. O agente inicia o `agentport mcp` no servidor por `stdio`. O servidor MCP fala
   com o bridge com um token de sessão, pelo túnel.
4. Planejado: as ferramentas de portas pedem ao Companion que abra uma porta. O
   Companion a abre pela mesma conexão SSH (`ssh -O forward -L`, com
   ControlMaster no macOS e no Linux), de modo que a porta chega só ao
   `127.0.0.1` da sua máquina. Nada fica escutando na sua rede.

Cada servidor tem o seu perfil e o seu Companion, então dois servidores nunca
compartilham token ou túnel.

## Requisitos

**Sua máquina (host)**

- macOS (Apple silicon ou Intel), Linux (amd64 ou arm64) ou Windows (amd64). O
  instalador do Windows e o Companion são compilados e testados no CI; o motor
  de forward para Windows é planejado (F6).
- Cliente OpenSSH (`ssh`) no `PATH`. O Companion o usa para o túnel, então lê o
  seu `~/.ssh/config`, as suas chaves e o ssh-agent como de costume.
- Go 1.27 ou mais novo, só se você compilar a partir do código.

**O servidor**

- Linux ou macOS, amd64 ou arm64, com um servidor SSH em que você consegue
  entrar. O primeiro `setup` pode pedir a senha uma vez; depois disso, usa uma
  chave.
- Um shell POSIX, porque o `setup` executa pelo SSH um conjunto curto e fixo de
  comandos.
- Pelo menos um agente de código compatível instalado (veja a tabela abaixo). O
  agentport não instala agentes.

**Agentes compatíveis (harnesses)**

O agentport integra-se ao *harness*, o programa que executa o agente e chama as
suas ferramentas, e não ao modelo usado por ele. `setup`, `pair` e `connect`
detectam os harnesses já instalados e configuram somente esses.

| Harness | Estado | Integração |
| --- | --- | --- |
| Claude Code | Suportado | Detectado e configurado automaticamente quando instalado |
| Codex | Suportado | Detectado e configurado automaticamente quando instalado |
| Gemini CLI | Suportado | Detectado e configurado automaticamente quando instalado |
| AGY / Antigravity CLI | Suportado | O agentport cria ou atualiza a entrada MCP global |
| OpenCode | Suportado | O agentport cria ou atualiza a entrada MCP global |
| Pi Coding Agent | Suportado | O agentport instala uma extensão global |
| Outro cliente MCP | Manual | Aponte para `agentport mcp` e defina as variáveis do perfil à mão |

DeepSeek e MiMo são modelos, não harnesses: use-os por meio de um harness
suportado, como o Pi ou o OpenCode.

## Instalação

### Homebrew

> **Planejado.** O workflow de release já gera a fórmula, mas o tap
> `rafaelb13/homebrew-tap` ainda não foi publicado, então os comandos abaixo
> ainda não funcionam. Use o script de instalação ou compile a partir do código
> até o tap existir.

Depois que o tap for publicado:

```bash
brew install rafaelb13/tap/agentport
agentport version
brew upgrade agentport
brew uninstall agentport
```

O Homebrew é o dono da instalação. Se você instalou pelo Homebrew, o
`agentport upgrade` não substitui o executável: ele imprime `agentport foi
instalado com o Homebrew. Execute: brew upgrade agentport` e termina com
status 0. O início automático não precisa de `brew services`: o Companion sobe
com `agentport companion autostart` (veja [Início automático](#início-automático)).

### Script de instalação

No macOS ou no Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/rafaelb13/agentport/main/scripts/install.sh | sh
```

Para escolher uma versão ou um diretório:

```bash
curl -fsSL https://raw.githubusercontent.com/rafaelb13/agentport/main/scripts/install.sh | sh -s -- --version vX.Y.Z --install-dir "$HOME/.local/bin"
```

No Windows, no PowerShell:

```powershell
irm https://raw.githubusercontent.com/rafaelb13/agentport/main/scripts/install.ps1 | iex
```

Para passar uma versão, baixe o script e execute-o com `-Version`:

```powershell
.\install.ps1 -Version vX.Y.Z
```

Os scripts detectam a plataforma, consultam o release mais recente, verificam
o release (veja abaixo) e comparam com a versão instalada. Eles baixam somente
quando o release é mais novo, então executar de novo é seguro. Opções:

| Opção | Shell | PowerShell | Variável de ambiente |
| --- | --- | --- | --- |
| Versão | `--version vX.Y.Z` | `-Version vX.Y.Z` | `AGENTPORT_VERSION` |
| Diretório de instalação | `--install-dir CAMINHO` | `-InstallDir CAMINHO` | `AGENTPORT_INSTALL_DIR` |
| Repositório | | | `AGENTPORT_REPOSITORY` |
| Pular a checagem de atestado | | | `AGENTPORT_SKIP_ATTESTATION=1` |
| Idioma | | | `AGENTPORT_LANG` |

No Windows, o diretório padrão é `%LOCALAPPDATA%\Programs\agentport\bin`. Os
releases também estão na página de
[GitHub Releases](https://github.com/rafaelb13/agentport/releases).

### A partir do código

```bash
git clone https://github.com/rafaelb13/agentport.git
cd agentport
go build -o agentport ./cmd/agentport
```

Coloque o `agentport` resultante no seu `PATH`. Um binário compilado a partir do
código não tem número de release, então `agentport version` não mostra uma
versão de release e a checagem de atualização não se aplica a ele.

### Verificar um release

Cada release publica o `checksums.txt`, assinado com o
[cosign](https://github.com/sigstore/cosign) (keyless, ligado à identidade do
workflow de release), e um atestado de build do GitHub. Cada arquivo tem uma
entrada SHA-256, e o atestado cobre os arquivos, os SBOMs e os checksums. Para
verificar à mão, baixe o `checksums.txt`, o `checksums.txt.bundle` e o
`attestation.jsonl` do release:

```bash
VERSION=vX.Y.Z
# A identidade exige a tag exata do release, então uma execução de outra branch não é aceita.
IDENTITY="https://github.com/rafaelb13/agentport/.github/workflows/release.yml@refs/tags/${VERSION}"

cosign verify-blob \
  --bundle checksums.txt.bundle \
  --certificate-identity "$IDENTITY" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt

# Linux
sha256sum --check --ignore-missing checksums.txt
# macOS (não tem sha256sum nem --ignore-missing)
ARCH=$(uname -m | sed s/x86_64/amd64/)
grep " agentport_${VERSION}_darwin_${ARCH}.tar.gz$" checksums.txt | shasum -a 256 --check

gh attestation verify "agentport_${VERSION}_linux_amd64.tar.gz" \
  --repo rafaelb13/agentport \
  --cert-identity "$IDENTITY"
```

O `gh attestation verify` sem `--bundle` consulta o GitHub e precisa de `gh auth
login`. Para verificar sem entrar, acrescente `--bundle attestation.jsonl`.

**Verificação automática.** A partir da `v0.7.1-rc.1`, os instaladores e o
`agentport upgrade` confirmam, além do SHA-256, que o arquivo foi produzido pelo
workflow de release deste repositório na tag que está sendo instalada. Se essa
confirmação falhar, a instalação é recusada. O script de instalação usa o
`gh attestation verify` quando o `gh` está instalado e, sem ele, consulta a API
de atestados do GitHub. O `agentport upgrade` usa a API. Versões anteriores à
`v0.7.1-rc.1` não têm atestado; são instaladas só com a checagem de SHA-256, e o
instalador avisa disso.

Se a API de atestados estiver fora do ar ou com o limite de requisições
estourado, a instalação é recusada em vez de seguir sem a checagem. Para
instalar só com a checagem de SHA-256, defina `AGENTPORT_SKIP_ATTESTATION=1`. O
`agentport setup` e o `agentport upgrade` aplicam a mesma definição aos binários
que enviam aos servidores.

## Configurar um servidor

O `agentport setup` configura um servidor em um só comando:

```bash
agentport setup bastion-m2 --profile m2
ssh bastion-m2
codex
```

Aqui, `bastion-m2` é um alias SSH do seu `~/.ssh/config`. Depois, peça ao agente
que verifique o link: "Verifique o status do agentport." Ele chama
`agentport_status`.

A sintaxe do `setup`:

```text
agentport setup <destino-ssh> [--profile NOME] [--agent all|codex|claude|gemini|agy|opencode|pi] [--version vX.Y.Z] [--remote-port 39123] [--skip-agent] [--skip-install] [--no-start]
```

O que o `setup` faz, em ordem:

1. **Verifica o SSH.** Primeiro tenta um login por chave sem interação. Se
   funcionar, a sua configuração SSH atual é mantida como está.
2. **Cria uma chave dedicada, se preciso.** Se o servidor aceita só senha, o
   `setup` cria uma chave Ed25519 usada só pelo agentport, pede a senha uma vez
   para incluir a chave no `authorized_keys` do servidor e usa essa chave nos
   túneis seguintes. A chave fica no diretório de configuração do seu usuário
   (`agentport/keys/`), com permissões privadas.
3. **Instala o binário no servidor**, em `~/.local/bin/agentport`. Esta máquina
   baixa e verifica o binário da plataforma do servidor (ela lê o `uname` do
   servidor) e o envia por SSH. Um servidor que já tem a mesma versão ou uma mais
   nova não é alterado. Para pular esta etapa, use `--skip-install`.
4. **Registra o agentport nos agentes** encontrados no servidor, a menos que
   `--skip-agent` seja usado. `--agent` limita a um harness.
5. **Salva o perfil** na sua máquina, com o destino, o token de pareamento e a
   porta remota. Os perfis ficam em `<diretório de configuração do usuário>/agentport/profiles/`
   (no macOS, `~/Library/Application Support/agentport/profiles/`). O arquivo
   guarda o token de pareamento, então mantenha-o privado.
6. **Inicia o Companion**, a menos que `--no-start` seja usado.

Executar o `setup` de novo para o mesmo perfil gera um novo token de pareamento e
para o Companion que estava rodando para ele.

### Opções

| Opção | Significado |
| --- | --- |
| `--profile NOME` | Nome local do pareamento. Sem ele, o nome é derivado do destino. |
| `--agent NOME` | Limita a configuração a um harness: `codex`, `claude`, `gemini`, `agy`, `opencode`, `pi` ou `all` (padrão). |
| `--version vX.Y.Z` | Release a instalar no servidor. |
| `--remote-port N` | Porta do loopback do servidor usada pelo túnel reverso. Padrão `39123`. |
| `--skip-agent` | Não registra a integração MCP no servidor. |
| `--skip-install` | Não instala nem atualiza o binário no servidor. |
| `--no-start` | Conclui sem iniciar o Companion. |

### Destinos no servidor

O destino é o que você passaria ao `ssh`: um alias do `~/.ssh/config`, um nome de
host ou `usuário@host`. Porta, chave e salto ficam no config do SSH, então use um
alias para um servidor que precise deles:

```text
# ~/.ssh/config
Host bastion-m2
  HostName 10.0.0.12
  User rafael
  Port 2222
  IdentityFile ~/.ssh/id_ed25519
  ProxyJump bastion
```

**Planejado:** o `setup` vai receber a porta, a chave e o host de salto como
opções (`-p`, `-i`, `-J`), e também endereços IPv6 com porta
(`usuário@[2001:db8::12]:2222`). Até lá, use um alias.

## Início automático

O Companion roda na sua máquina, então precisa estar rodando para que o túnel
exista. Há duas formas de ele subir sozinho.

**Ao fazer login (disponível agora).** `agentport companion autostart enable m2`
instala o mecanismo de login do próprio sistema: um LaunchAgent em
`~/Library/LaunchAgents`, no macOS; uma unidade de usuário do systemd em
`~/.config/systemd/user`, no Linux; e uma tarefa do Agendador de Tarefas que roda
no logon, no Windows. O serviço executa `agentport companion serve m2` e o reinicia
se ele falhar. Ele não abre um segundo Companion quando já há um rodando.

```bash
agentport companion autostart enable m2
agentport companion autostart status m2
agentport companion autostart disable m2
```

O `disable` para o Companion que o serviço iniciou. Um Companion que você iniciou
por conta própria continua rodando.

**Ao conectar no servidor (planejado, F5).** O `setup` vai adicionar, depois de
perguntar a você, um bloco ao seu `~/.ssh/config`, para que conectar no servidor
inicie o Companion se ele não estiver rodando:

```text
# agentport: perfil m2
Match host 10.0.0.12 user rafael
  PermitLocalCommand yes
  LocalCommand agentport companion ensure m2
```

O bloco será delimitado pelo comentário `# agentport: perfil`, então apagar essas
linhas o remove. Até lá, não há bloco para remover.

**Se o servidor reiniciar** enquanto o Companion continua rodando na sua máquina,
o Companion restabelece o túnel sozinho, com espera de 1 a 30 segundos. O mesmo
perfil e a mesma porta remota são usados de novo.

## Uso no dia a dia

Conecte-se ao servidor como de costume e inicie o agente:

```bash
ssh bastion-m2
codex
```

Verifique o link quando quiser, pedindo ao agente:

> Verifique o status do agentport.

O agente chama `agentport_status`, que informa se o túnel até o seu Companion
está conectado e se há um release estável mais novo. Para ver o mesmo na sua
máquina:

```bash
agentport companion status m2
```

**Planejado (F4 e F5).** Quando as ferramentas de portas forem entregues, você
poderá pedir:

> Mapeie as portas deste projeto e abra o app em localhost.

O agente lista as portas em escuta no servidor e pede para abrir as que precisa,
com um motivo para cada uma. Cada pedido aparece no painel como um só pedido em
lote, mesmo com muitas portas; você aprova ou recusa todas juntas ou uma a uma.
As portas aprovadas abrem em `127.0.0.1` da sua máquina, e o agente informa a
URL local. Se a porta local estiver ocupada, o agentport usa a próxima livre e
avisa.

## O painel

**Planejado (F5).** O painel é uma página na sua máquina, servida em
`127.0.0.1` e protegida por token. Ele vai mostrar:

- **Pedidos do agente** no topo: porta, motivo, comando, aprovar ou recusar, e
  por quanto tempo a aprovação vale (nesta sessão, uma hora ou sempre). Um pedido
  em lote vira um cartão com caixas de seleção e "Aprovar todas".
- **Portas abertas** como `:remota -> localhost:local`, com nome, comando, tráfego
  ao vivo, abrir em nova aba, fixar, detalhes, editar e fechar. Fechar pede
  confirmação e mostra quais sessões de agente serão avisadas.
- **Agrupar e fechar várias**: agrupar por pedido, por sessão de agente ou em
  lista; selecionar várias portas; fechar um grupo ou a seleção com uma só
  confirmação.
- **Trocar de servidor** com portas abertas pergunta se você quer fechá-las,
  mantê-las ou cancelar, com a opção de lembrar a escolha.
- **Portas encerradas**, com a hora e quem fechou, e **reabrir**, que usa a mesma
  configuração ou propõe uma porta local livre se a antiga estiver ocupada.
- **Portas detectadas** no servidor e um menu para abrir uma à mão (atalho `/`).
- **Detalhes do processo** sob demanda: PID, usuário, CPU, memória, diretório,
  processo pai, contêiner, linha de comando, saúde da porta e o motivo dado pelo
  agente.
- **Atividade**, notificações, atalhos e uma **barra superior** com o servidor
  atual, a troca de servidor, o terminal web, a política de aprovação, o idioma,
  o tema (sistema, claro, escuro), a ajuda e o encerramento da sessão.
- **Terminal web** (F5b): um terminal para o servidor que abre pelo painel. Ele
  usa a conexão SSH do perfil e fica em `127.0.0.1` com o token do painel. Pode
  ser desligado por perfil.
- **Conectar a um servidor**: sugestões encontradas na sua máquina (veja
  [Modo standalone](#modo-standalone)).

**Hoje**, a página do Companion é uma visão operacional simples, em `127.0.0.1`,
com um token que você abre com `agentport companion open m2`. Ela mostra o perfil
e o destino, o estado do túnel (`Conectado` ou `Desconectado`) com o último erro,
um aviso quando existe um release estável mais novo, e o botão **Parar Companion**,
que equivale a `agentport companion stop m2`. Ela não transfere arquivos.

## Terminal

O painel tem uma aba **Terminal** que abre um shell no servidor conectado, para rodar um comando rápido sem sair do navegador. Ela reaproveita a conexão SSH do túnel (sem novo login), é servida só para o painel em `127.0.0.1` e aceita conexões apenas da página do próprio painel. Até quatro terminais podem estar abertos ao mesmo tempo. Fechar o terminal, ou parar o Companion, encerra a sessão SSH no servidor. O conteúdo do terminal nunca é gravado em logs. No Windows, o terminal não está disponível nesta versão.

## Ferramentas MCP

O agente enxerga estas ferramentas pelo servidor MCP no servidor.

| Ferramenta | Entrada | Retorna |
| --- | --- | --- |
| `agentport_status` | nenhuma | **Disponível agora.** O estado do túnel, a versão do agentport e se existe um release estável mais novo (nesse caso recomenda `agentport upgrade`). |
| `ports_list` | `include_system?`, `only_user?` | **Planejado.** As portas TCP em escuta no servidor: porta, endereço de bind, protocolo, PID, processo, uma linha de comando curta e se já existe um forward. |
| `ports_forward` | `reason`, `group?`, `ports[]` de `{remote_port, label?, path?}` | **Planejado.** Pede a sua aprovação como um pedido em lote. Para cada porta retorna o estado (`active`, `pending_approval`, `denied`, `skipped`), a porta local realmente usada e a URL com o `path`. Espera até 2 minutos; se você não respondeu, retorna `pending_approval`, e o agente pode consultar `forwards_list` depois. |
| `ports_unforward` | `ports[]` ou `all` | **Planejado.** Fecha os forwards desta sessão. Nunca fecha uma porta de outra sessão. |
| `forwards_list` | nenhuma | **Planejado.** Os forwards ativos com a origem (agente, automático ou manual), o tempo ativo e os bytes. Os forwards de outras sessões aparecem marcados como tais e não podem ser fechados por esta sessão. |

O agente nunca escolhe a sua porta local, e nenhuma ferramenta permite que ele
pare um processo na sua máquina. Parar um processo é decisão sua, tomada no
painel.

Se o Companion não estiver rodando quando uma ferramenta for chamada, a
ferramenta responde que o Companion está offline e orienta o agente a executar
`agentport companion start <perfil>`, que ele repassa a você.

## Várias sessões de agente

**Planejado (F4).** Um Companion atende cada servidor. Cada sessão de agente nesse
servidor (por exemplo, dois Claude Code em projetos diferentes, ou Claude Code e
Codex) roda o seu próprio `agentport mcp`, e todas falam com o mesmo Companion.
Cada sessão é identificada pelo agente, pelo PID, pela pasta do projeto e pelo
horário em que começou.

- Só o Companion escolhe portas locais, um pedido por vez; por isso duas sessões
  nunca recebem a mesma porta local.
- Se duas sessões pedirem a mesma porta remota, ela é aberta uma vez. As duas
  recebem a mesma URL e são donas dela. A porta fecha quando nenhuma dona precisar
  mais dela, ou quando você a fechar.
- O `ports_unforward` tira apenas a sessão que chamou da lista de donas das suas
  portas. Uma porta que outra sessão ainda usa continua aberta.
- Quando uma sessão termina, as portas que só ela usava ficam marcadas como
  "sessão encerrada" e fecham depois de 20 minutos, a menos que você as tenha
  fixado. Se o agente voltar à mesma pasta nesse prazo, ele as recupera.

## Portas ocupadas no host

Quando uma porta que você quer já está em uso na sua máquina, o Companion verifica
quem a segura. O código que identifica o dono e o para existe hoje
(`internal/hostport`). A janela que pergunta o que fazer é **planejada (F5)**.

| Quem segura a porta | O que a janela oferece |
| --- | --- |
| Um processo seu (node, vite, python) | Usar outra porta, ou parar o processo: SIGTERM e, após 5 segundos, SIGKILL (`taskkill` no Windows). |
| Um serviço que reinicia sozinho (`brew services`, LaunchAgent, systemd user) | Usar outra porta, ou parar o serviço com o comando certo (por exemplo `brew services stop php`), mostrado antes de executar. |
| Um contêiner Docker | Usar outra porta, ou `docker stop <contêiner>`. Nunca o daemon do Docker. |
| Outro usuário ou o sistema | Usar outra porta. Parar fica desativado e explicado. O painel nunca pede senha de administrador. |

Depois de uma parada, o Companion verifica a porta de novo. Se ela continuar
ocupada, usa a porta alternativa e registra isso na atividade.

## Modo standalone

**Planejado.** `agentport open` vai abrir o painel sem nenhum perfil, com uma tela
para conectar a um servidor. Ela vai sugerir conexões encontradas na sua máquina,
sem enviar nada para lugar nenhum:

- os hosts de `~/.ssh/config` e dos seus arquivos `Include` (entradas com
  curinga, como `Host *.corp`, são ignoradas), com usuário, porta, chave e host de
  salto resolvidos por `ssh -G <alias>`;
- a configuração do Remote-SSH do VS Code;
- as sessões `ssh` abertas agora, marcadas como "conectado agora";
- no Windows, as sessões salvas do PuTTY.

O histórico do terminal (`~/.zsh_history`, `~/.bash_history`) só é usado se você
marcar uma opção para essa busca. O `known_hosts` não é fonte, porque os nomes
costumam estar em hash. Cada sugestão tem **Testar** (`ssh -o BatchMode=yes`, para
ver se a chave autentica, se pede senha e se o agentport já está instalado) e
**Configurar** (o mesmo fluxo do `setup`). Você também pode digitar
`usuário@ip`.

## Idiomas

O agentport tem quatro idiomas: inglês (padrão), português (Brasil), espanhol e
繁體中文 (chinês tradicional, como usado em Taiwan).

A linha de comando, o `help`, os instaladores e o Companion escolhem o idioma pelo
ambiente, nesta ordem:

1. `AGENTPORT_LANG`
2. `LC_ALL`
3. `LC_MESSAGES`
4. `LANG`

A primeira variável definida decide, como na biblioteca C. `pt`, `pt_BR`, `pt_PT` e
valores parecidos dão português (Brasil); `es` e `es_*` dão espanhol; `zh_TW`,
`zh_HK`, `zh_MO` e `zh-Hant` dão chinês tradicional. `C`, `POSIX` e qualquer outro
valor, inclusive `zh_CN` e `zh-Hans`, dão inglês. Assim, `LANG=pt_BR.UTF-8
agentport help` imprime em português, e `AGENTPORT_LANG=en` força o inglês.

A página do Companion usa, nesta ordem: `?lang=pt-BR` no endereço, o idioma
preferido do navegador, o idioma que o processo do Companion detectou como acima e,
por fim, o inglês.

**Hoje** só o português (Brasil) tem catálogo. Espanhol e chinês tradicional são
escolhidos quando o ambiente pede, mas as mensagens continuam em inglês até que os
catálogos sejam escritos (planejado). As mensagens de erro técnicas ficam em inglês
de propósito, para poderem ser pesquisadas. Nomes de comandos, caminhos e nomes de
processos não são traduzidos.

**Planejado:** a mesma ordem no painel (F5), e os catálogos de `es` e `zh-Hant`.

## Segurança

- O bridge escuta somente em `127.0.0.1`. O servidor o alcança apenas pelo túnel
  SSH que o Companion abriu.
- O servidor faz parte da sua base de confiança. Um agente no servidor só alcança
  o bridge com o token de sessão que o Companion registra para ele.
- O token de pareamento fica no arquivo do seu perfil. Não coloque perfis nem
  tokens em arquivos que vão para o repositório de um projeto.
- A página do Companion fica em `127.0.0.1` e exige um token que está na própria
  URL. Não compartilhe essa URL: ela é uma visão do seu Companion.
- Os releases são gerados por um único workflow, com checksum SHA-256, assinatura
  cosign, atestado de build e uma verificação `govulncheck` dos binários. Veja
  [Verificar um release](#verificar-um-release).

**Planejado (F4 e F5)**, para as ferramentas de portas e o painel:

- Os forwards fazem bind somente em `127.0.0.1` da sua máquina. Não há opção para
  `0.0.0.0` na versão 1.
- O destino de um forward é sempre `127.0.0.1:<porta>` no servidor, então o
  túnel não pode ser usado para alcançar outros hosts da rede do servidor.
- Você aprova cada porta que o agente pede, a menos que escolha "sempre" para ela.
- Portas bloqueadas (22, 2375, 2376) e um limite de forwards simultâneos por perfil.
- Parar um processo na sua máquina só é possível pelo painel, com confirmação, e
  nunca com privilégios de administrador. Não é uma ferramenta MCP.

## Atualização

O `agentport upgrade` baixa o release estável mais recente para esta máquina,
confere o SHA-256 e o atestado, e substitui este executável. Depois atualiza todos
os servidores salvos, na ordem abaixo.

1. Esta máquina baixa e verifica o binário (a etapa mais lenta; a saída avisa).
2. Cada servidor é atualizado. Uma falha em um servidor não interrompe os demais,
   e o resumo nomeia cada perfil que falhou.
3. Só então os Companions em execução são parados, o executável local é
   substituído e esses Companions são iniciados de novo. A troca local fica por
   último porque é a única etapa difícil de desfazer.

Se esta máquina já está na release mais recente, nada é baixado nem substituído
aqui; só os servidores que estiverem atrás são atualizados.

**Com o Homebrew**, o binário pertence ao keg do Homebrew, então o `agentport
upgrade` não o substitui. Ele imprime `agentport foi instalado com o Homebrew.
Execute: brew upgrade agentport` e depois atualiza todos os servidores salvos para
a versão deste binário, como em qualquer instalação. Para levar esta máquina a um
release mais novo, execute `brew upgrade agentport`; o próximo `agentport upgrade`
traz os servidores para essa versão.

Defina `AGENTPORT_SKIP_ATTESTATION=1` somente se você aceitar apenas a checagem de
SHA-256.

O Companion verifica se existe um release estável mais novo ao iniciar e, no
máximo, uma vez a cada 24 horas, compartilhando essa checagem entre os perfis. Um
release mais novo aparece na página do Companion e em `agentport_status`.

## Desinstalação

1. **Pare o início automático e o Companion** de cada perfil:

   ```bash
   agentport companion autostart disable m2
   agentport companion stop m2
   ```

2. **Remova a entrada do agentport de cada harness** no servidor:

   ```bash
   agentport uninstall m2 --agent claude
   ```

   Repita para cada harness que você configurou. Isso remove somente a entrada ou
   a extensão do agentport. Não remove o próprio agente.

3. **Remova o binário** do servidor, se nenhum outro perfil o usar:

   ```bash
   ssh bastion-m2 'rm -f ~/.local/bin/agentport'
   ```

4. **Remova os arquivos locais**: o perfil em
   `<diretório de configuração do usuário>/agentport/profiles/m2.json`, a chave
   dedicada em `<diretório de configuração do usuário>/agentport/keys/` (se uma
   foi criada) e os logs em `<diretório de cache do usuário>/agentport/logs/`.

5. **Remova o binário** da sua máquina:
   - Homebrew: `brew uninstall agentport`.
   - Script de instalação ou código-fonte: apague o arquivo `agentport` do diretório
     de instalação (por padrão `~/.local/bin`, no macOS e no Linux, e
     `%LOCALAPPDATA%\Programs\agentport\bin`, no Windows).

**Planejado:** o `uninstall` vai remover também o bloco do `~/.ssh/config` que o
`setup` adiciona, e um comando vai apagar um perfil.

## Solução de problemas

**O Companion não está rodando.** Inicie-o com `agentport companion start m2`, ou
ative o [início automático](#início-automático). O `agentport companion status m2`
informa se ele está rodando e por que o túnel está caído.

**O túnel cai o tempo todo.** Veja o caminho do log com `agentport logs m2`, ou
exporte-o com `agentport logs m2 --export ./agentport-m2.log`. Para registrar
eventos do túnel enquanto você reproduz o problema:

```bash
agentport companion stop m2
agentport companion start m2 --verbose
```

O modo verbose registra os eventos e os erros do túnel. Ele nunca registra tokens
nem conteúdo.

**O `setup` continua pedindo a senha.** O servidor não está aceitando login por
chave pública para o seu usuário, ou o `~/.ssh/authorized_keys` não pode ser
escrito. Verifique primeiro se o login por chave funciona com o `ssh` comum; o
`setup` só consegue adicionar a chave dedicada quando a senha funciona uma vez.

**"could not verify the build attestation".** A API de atestados não foi
alcançada, ou o release não tem atestado. Verifique a sua rede ou instale o `gh`
e tente de novo. Defina `AGENTPORT_SKIP_ATTESTATION=1` somente se você aceitar a
checagem de SHA-256 apenas.

**"SSH destination must not start with a dash".** O destino parece uma opção.
Informe um host, um alias ou `usuário@host`.

**O `upgrade` diz que o agentport foi instalado com o Homebrew.** Execute
`brew upgrade agentport`, como a mensagem indica.

**O `agentport doctor` diz que o bridge está indisponível.** Inicie o Companion de
um perfil. O `doctor` verifica só o bridge local; o `companion status` também
informa o túnel.

## Desenvolvimento

Build, testes, processo de release e catálogos de mensagens estão em
[DEVELOPMENT.md](DEVELOPMENT.md). As regras de contribuição estão em
[CONTRIBUTING.md](CONTRIBUTING.md), e a política de segurança em
[SECURITY.md](SECURITY.md). A referência de comandos é o [COMMANDS.md](COMMANDS.md).

**Harness ponta a ponta.** `test/e2e` sobe um `sshd` descartável no Docker e verifica,
contra ele, os forwards por um ControlMaster real, os conflitos de porta e o scanner
de portas dentro do Linux. Precisa do Docker rodando e não toca no seu `~/.ssh`:

```bash
test/e2e/run.sh
go test -tags e2e -v -count=1 ./test/e2e/
```

Um `go test ./...` comum não executa o harness.

**Planejado (F7):** o mesmo harness vai executar o `setup` e as ferramentas de portas
de ponta a ponta, quando forem integrados.

## Créditos

O agentport nasceu como fork do [AgentClip](https://github.com/wendellrocha/agentclip),
de [Wendell Rocha](https://github.com/wendellrocha), distribuído sob a licença MIT.
Reaproveitamos a base do Companion, o túnel SSH reverso, o servidor MCP por stdio,
o registro automático nos agentes e o pipeline de releases verificáveis. Obrigado
ao projeto original. 💙

## Licença

Distribuído sob a [licença MIT](LICENSE). A licença mantém o copyright do projeto
original AgentClip, junto com o copyright do agentport.
