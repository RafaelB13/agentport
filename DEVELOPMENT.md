# Desenvolvimento

## Pré-requisitos

- Go 1.27 ou superior;
- SSH para um servidor de teste, caso queira validar o fluxo completo;
- Ao menos um harness suportado instalado no servidor: Codex, Claude Code,
  Gemini CLI, AGY / Antigravity CLI, OpenCode ou Pi Coding Agent.

## Build e testes

```bash
go build -o ./agentport ./cmd/agentport
go test -race ./...
go vet ./...
```

O binário local `./agentport` e a pasta `plans/` são ignorados pelo Git.

A versão de desenvolvimento vive em `internal/buildinfo/buildinfo.go`. As
releases usam a tag `vX.Y.Z` para sobrescrevê-la no binário e nos metadados MCP;
para iniciar o próximo ciclo, altere apenas esse arquivo.

## Fluxo de desenvolvimento

Para uma instalação remota manual durante o desenvolvimento:

```bash
go build -o ./agentport ./cmd/agentport
scp ./agentport bastion-m2:~/.local/bin/agentport
ssh bastion-m2 'agentport version'

./agentport pair m2 bastion-m2 --remote-port 39123
./agentport companion start m2
./agentport companion open m2
```

`pair` mantém esse fluxo avançado. `setup` é o caminho recomendado para uma
release publicada; ele não tenta baixar uma versão de desenvolvimento sem que
`--version vX.Y.Z` seja informado.

`pair`, `setup` e `connect` detectam e configuram todos os harnesses suportados
instalados no servidor. AGY e OpenCode recebem entradas globais de usuário; Pi
recebe uma extensão global. Para limitar a um harness ou remover exclusivamente
a entrada MCP/extensão do agentport, use:

```bash
./agentport connect m2 --agent claude
./agentport connect m2 --agent gemini
./agentport uninstall m2 --agent gemini
```

`disconnect` é mantido como alias compatível de `uninstall`. Nenhum dos dois
remove o binário ou desinstala o harness remoto.

## Isolar diretórios em testes e manuais

Para testes manuais, aponte os diretórios para temporários e não toque no
perfil nem no cache reais:

```bash
export AGENTPORT_CONFIG_DIR="$(mktemp -d)" AGENTPORT_CACHE_DIR="$(mktemp -d)"
```

As variáveis estão descritas em [COMMANDS.md](COMMANDS.md#diretórios). Os
testes de pacote usam `testenv`, que redireciona `HOME`, `XDG_*` e as duas
variáveis `AGENTPORT_*` para um diretório temporário.

## Ciclo de vida do Companion

`./agentport companion start m2` retorna ao terminal depois de iniciar o
processo local em background. Esse processo mantém o
bridge e reconecta o túnel SSH quando o servidor remoto ou a conexão cai,
usando espera progressiva de 1 a 30 segundos. Inspecione ou pare-o com:

```bash
./agentport companion status m2
./agentport companion open m2
./agentport companion stop m2
```

O perfil é persistido em `os.UserConfigDir()/agentport/profiles/`, mas o
Companion não é registrado como serviço de inicialização no macOS, Linux ou
Windows. Após reiniciar o host, rode `./agentport companion start m2` de novo.
Um reinício somente do servidor remoto não exige esse comando: o Companion
que ainda estiver rodando tentará abrir novamente o mesmo túnel reverso.

## Validar arquivos e CSV

1. Copie um arquivo, como `vendas.csv`, no Finder, Explorer ou gerenciador de
   arquivos compatível.
2. Confirme na view do Companion que o item foi armado:

   ```bash
   ./agentport companion status m2
   ```

3. Abra uma sessão no servidor, inicie o Codex e peça: `Verifique o status do
   agentport.`
4. O agente deve chamar `agentport_status` e receber `"tunnel": "connected"`.

## Estrutura do código

- `cmd/agentport`: CLI e orquestração dos comandos.
- `internal/bridge`, `internal/daemon`: bridge HTTP local, sessões e a
  comunicação de controle.
- `internal/companion`: perfis, túnel SSH, servidor de controle e página web.
- `internal/mcpserver`: servidor MCP por `stdio` e cliente HTTP do bridge.
- `internal/agents`: adaptadores que registram o MCP em cada agente remoto.
- `internal/harness`: configuração local de AGY, OpenCode e Pi (inclui o
  template da extensão Pi).
- `internal/remote`: comandos SSH de preflight, instalação e chave dedicada.
- `internal/release`, `internal/upgrader`: consulta de releases e atualização.

## Mensagens e idiomas

O inglês (en-US) é o idioma-fonte e o padrão; o português do Brasil (pt-BR) é um
catálogo em `internal/i18n/catalogs/pt-BR.json`. A mensagem em inglês é a própria
chave, e o que faltar no catálogo aparece em inglês.

- **CLI**: o que a pessoa lê (ajuda, resultado, status, avisos) passa por
  `i18n.T("English text %s", arg)`, com um **literal** como primeiro argumento. Os
  comandos e seus textos de ajuda ficam na tabela de `cmd/agentport/commands.go`,
  que também é de onde o `main` despacha.
- **Página do Companion**: `t('English text')` no `app.js` e `{{t:English text}}`
  no `index.html`. O servidor envia à página só as traduções que o script usa.
- **Instaladores**: as tabelas de mensagens no início de `scripts/install.sh` e de
  `scripts/install.ps1`, escolhidas por `AGENTPORT_LANG`.
- **Erros técnicos** ficam em inglês, sem tradução.

Os testes de `internal/i18n` falham se uma mensagem usada não tiver tradução, se
uma tradução ficar órfã, se uma tradução mudar os verbos de formato (`%s`, `%d`)
da mensagem ou se um `i18n.T` não receber um literal (a exceção marcada com
`i18n:dynamic` é a das mensagens escritas no HTML). Ao mudar o texto em inglês de
uma mensagem, mude a chave no catálogo também. Para um novo idioma, acrescente o
catálogo, o arquivo embutido, `Supported` e `Languages`.

## Releases

Uma tag semântica `vX.Y.Z` dispara os workflows de teste e release. A release
publica binários para Linux `amd64`/`arm64`, macOS `amd64`/`arm64` e Windows
`amd64`, além de checksums (assinados com cosign, `checksums.txt.bundle`),
o atestado de proveniência (`attestation.jsonl`), um SBOM CycloneDX
por plataforma (`agentport_vX.Y.Z_<os>_<arch>.cdx.json`, cobertos pelos checksums e
pelo atestado; as dependências mudam com a plataforma) e
instaladores. Antes de assinar, o workflow roda o `govulncheck` em modo binário
sobre cada executável: uma vulnerabilidade conhecida no que será publicado
(inclusive na biblioteca padrão do Go que o compilou) interrompe a release. As
duas ferramentas têm versão fixada no workflow, porque esse job pode assinar.

Os testes offline dos instaladores ficam em `scripts/test-install.sh` e
`scripts/test-install.ps1`, com uma resposta real da API do GitHub em
`scripts/testdata/`. Rode-os com `sh scripts/test-install.sh` e
`pwsh scripts/test-install.ps1`.

```bash
git tag -a vX.Y.Z -m "Release vX.Y.Z"
git push origin vX.Y.Z
```

As notas de release são geradas pelo GitHub a partir dos pull requests. Labels
como `feature`, `bug`, `security`, `documentation`, `dependencies` e
`breaking-change` determinam as categorias. Use `ignore-for-release` ou
`skip-changelog` para omitir um PR. Sem PRs entre a tag anterior e a atual, o
workflow publica os commits desse intervalo como notas de release.
