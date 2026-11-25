# Referência de comandos

Esta é a referência da interface de linha de comando do agentport. Nos
exemplos, `m2` é o nome de um perfil local e `devbox` é um destino aceito
pelo SSH (host, alias de `~/.ssh/config` ou `usuario@host`).

## Fluxo recomendado

```bash
agentport setup devbox --profile m2
agentport companion open m2
ssh devbox
codex
```

`setup` cria o perfil, configura os harnesses suportados que já existirem no
servidor e inicia o Companion. Depois disso, use o SSH normalmente e peça ao
agente para verificar o status com `agentport_status`.

## Comandos do usuário

### `setup`

```text
agentport setup <ssh-destination> [--profile NAME]
  [--agent all|codex|claude|gemini|agy|opencode|pi]
  [--version vX.Y.Z] [--remote-port 39123]
  [--skip-agent] [--skip-install] [--no-start]
```

É a forma recomendada de configurar um servidor. Instala a release compatível
no servidor Linux/macOS via SSH, cria ou substitui o perfil local, detecta e
configura os harnesses já instalados e, por padrão, inicia o Companion local.

- `--profile`: nome local do pareamento. Sem ele, o nome é derivado do destino.
- `--agent`: limita a configuração a um harness; `all` é o padrão.
- `--version`: escolhe a versão de release a instalar remotamente.
- `--remote-port`: porta loopback do servidor reservada ao túnel reverso.
- `--skip-agent`: não registra a integração MCP no servidor.
- `--skip-install`: não instala ou atualiza o binário remoto.
- `--no-start`: conclui a configuração sem iniciar o Companion.

Antes das etapas remotas, `setup` testa uma autenticação SSH por chave sem
interação. Se ela funcionar, preserva a configuração existente. Se o destino
aceitar somente senha, gera uma chave Ed25519 privada do agentport, pede a
senha uma vez para adicioná-la ao `authorized_keys` remoto e usa essa chave nos
túneis seguintes. A chave local fica em `os.UserConfigDir()/agentport/keys/`
com permissões privadas; `pair` não executa esse bootstrap.

Executar `setup` outra vez para o mesmo perfil gera um novo token de
pareamento e para o Companion anterior, caso esteja em execução.

### `pair`

```text
agentport pair <profile> <ssh-destination>
  [--agent all|codex|claude|gemini|agy|opencode|pi]
  [--remote-port 39123] [--skip-agent]
```

Cria somente o pareamento local e configura o harness remoto. É útil para
desenvolvimento ou quando o `agentport` já foi instalado manualmente no
servidor. Diferentemente de `setup`, não instala o binário remoto e não inicia
o Companion; faça isso com `agentport companion start <profile>`.

### `connect`

```text
agentport connect <profile>
  [--agent all|codex|claude|gemini|agy|opencode|pi]
```

Lê um perfil existente e (re)configura a integração agentport nos harnesses
remotos. Não reinicia o Companion nem altera o token ou a porta do perfil.

### `uninstall` e `disconnect`

```text
agentport uninstall <profile> --agent <codex|claude|gemini|agy|opencode|pi>
agentport disconnect <profile> --agent <codex|claude|gemini|agy|opencode|pi>
```

Remove somente a entrada MCP ou extensão do agentport do harness indicado no
servidor. `disconnect` é um alias de compatibilidade. Nenhum dos dois remove o
CLI do harness, o binário remoto, o perfil local ou o Companion em execução.

### `companion`

```text
agentport companion <start|stop|status|open|view|run> <profile>
agentport companion autostart <enable|disable|status> <profile>
```

Gerencia a parte local persistente do agentport.

- `start`: inicia o Companion em background. Ele mantém o bridge local e abre
  o túnel SSH reverso.
- `stop`: pede uma parada limpa do Companion e encerra o túnel. Se o Companion
  não parar em 8 s, ele é encerrado à força (mais 3 s de espera). A saída diz
  "parado" ou "não parou a tempo e foi encerrado à força". O bridge é encerrado
  junto quando nenhum outro Companion está rodando.
- `status`: imprime em JSON o perfil, o destino, o estado do túnel e o aviso de
  versão.
- `open` ou `view`: abre a página web local do Companion no navegador padrão.
- `run`: executa o Companion em primeiro plano; use para diagnóstico. `start`
  é preferível no uso normal.
- `autostart enable`: faz o Companion do perfil subir quando você entra na
  sessão, com o que cada sistema já tem: um LaunchAgent em
  `~/Library/LaunchAgents` no macOS, uma unidade de usuário do systemd em
  `~/.config/systemd/user` no Linux e uma tarefa `ONLOGON` do Agendador de
  Tarefas no Windows. O serviço executa `agentport companion serve <perfil>` e
  reinicia o processo se ele falhar. Ativar de novo recarrega a definição, e o
  perfil precisa existir. O serviço não abre um segundo Companion se já houver
  um rodando para o perfil.
- `autostart disable`: para o serviço e remove os arquivos que o `enable`
  criou. O Companion que o serviço iniciou é parado; um que você iniciou por
  conta própria continua rodando. Pode ser repetido sem erro, mas se o sistema
  se recusar a desligar o serviço o comando falha em vez de dizer que deu certo.
- `autostart status`: diz se o perfil sobe no login. No Linux e no Windows a
  resposta vem do próprio sistema (`systemctl is-enabled`, Agendador de
  Tarefas); no macOS, do LaunchAgent existir em `~/Library/LaunchAgents`, que
  é o que o faz carregar no login.

Um serviço de login não herda o seu terminal. Se o perfil está em um diretório
dado por `AGENTPORT_CONFIG_DIR`, o `enable` grava essa variável na definição do
serviço (LaunchAgent e systemd); no Windows, onde a tarefa não carrega variáveis,
o `enable` recusa e explica. O diretório de logs do macOS é criado no `enable`,
porque o launchd abre os arquivos de log antes de iniciar o processo.

### `upgrade`

```text
agentport upgrade
```

Baixa a release estável mais recente para a plataforma local, confere o
SHA-256 publicado em `checksums.txt` e o atestado de build do GitHub (a partir
da `v0.1.0`; `AGENTPORT_SKIP_ATTESTATION=1` ignora essa checagem) e
substitui o executável que está em uso.
Também atualiza todos os destinos dos perfis salvos, usando a identidade SSH
gerenciada pelo perfil quando houver. Para cada servidor, esta máquina baixa e
verifica o binário da plataforma dele (`uname`) e o envia por SSH; nenhum
instalador é baixado para o servidor, e uma versão igual ou mais nova que já
esteja lá não é substituída. Uma falha em um servidor não impede as
tentativas nos demais nem a atualização local; o resumo final identifica cada
perfil que falhou.

A ordem é: baixar e verificar o binário desta máquina (a etapa mais lenta, que a
saída anuncia), atualizar cada servidor, e só então parar os Companions em
execução, trocar o executável local e reiniciar esses Companions. A troca local
fica por último porque é a única parte difícil de desfazer: se algo falhar antes
dela, esta máquina fica como estava, e os túneis seguem no ar durante o trabalho
nos servidores.

Se esta máquina já está na release mais recente, nada é baixado, trocado ou
reiniciado aqui: o comando só atualiza os servidores que ainda estiverem
atrás, e o resumo distingue `updated` de `already up to date`.

Antes da troca local, o comando registra e para os Companions saudáveis. Após
a troca, reinicia somente esses perfis; um Companion que já estava parado
permanece parado. Em Windows, a troca é concluída por um pequeno processo
PowerShell depois que o comando termina, porque o `.exe` em execução fica
bloqueado pelo sistema.

Se o agentport foi instalado com o Homebrew, o comando não substitui o
executável, porque o keg pertence ao Homebrew. Ele detecta a instalação pelo
caminho real do executável (com os links simbólicos resolvidos): um caminho dentro
de `Cellar/agentport/`, ou dentro de `HOMEBREW_PREFIX/Cellar/agentport/`. Nesse
caso imprime `agentport foi instalado com o Homebrew. Execute: brew upgrade agentport`, e então atualiza os servidores salvos para a versão deste binário,
como faz em qualquer instalação. Para avançar o executável, use `brew upgrade agentport`; o próximo `agentport upgrade` atualiza os servidores para essa versão.
Se a detecção falhar, o comando segue o caminho normal. Um executável de build de
desenvolvimento (que não é uma release) não atualiza servidores; o comando só
imprime a instrução.

### Página web do Companion

`agentport companion open m2` abre uma página local protegida por uma URL com
token de visualização. Ela não é publicada na rede: o servidor HTTP escuta em
`127.0.0.1`. Não compartilhe essa URL.

A página aparece em inglês por padrão. Para vê-la em português, abra o endereço
com `?lang=pt-BR`, deixe o navegador preferir `pt-BR` ou inicie o Companion com
`AGENTPORT_LANG=pt-BR` (veja [Idioma](#idioma)). Os nomes dos botões abaixo estão
como aparecem em português; o original em inglês vem entre parênteses na
primeira menção.

A página atualiza a cada dois segundos e mostra:

- perfil e destino SSH;
- estado do túnel (`Conectado` ou `Desconectado`; em inglês, `Connected` ou
  `Disconnected`) e o último erro, quando há;
- aviso de release estável nova, com a versão e `agentport upgrade`;
- botão **Parar Companion** (*Stop Companion*), equivalente ao comando `companion stop`.

Ela é uma visão operacional: não transfere arquivos ao servidor.

### `doctor`

```text
agentport doctor
```

Verifica se há um bridge local saudável e informa endereço e PID. Para o fluxo
com Companion, prefira `agentport companion status <perfil>`, que também inclui
o estado do túnel.

### `logs`

```text
agentport logs <profile>
agentport logs <profile> --export caminho/agentport.log
```

Mostra o caminho do log privado do Companion ou exporta uma cópia completa
para o caminho informado. Para coletar detalhes adicionais durante a
reprodução de um problema, reinicie o perfil com:

```text
agentport companion stop <profile>
agentport companion start <profile> --verbose
```

O modo verbose registra eventos do túnel SSH, além de mensagens
de erro. Ele não registra tokens nem conteúdo.

O Companion consulta a release estável no início e no máximo uma vez a cada 24
horas, compartilhando um cache privado entre perfis. O status também está na
ferramenta MCP `agentport_status`, que recomenda `agentport upgrade` quando há uma versão
nova.

### `help`

```text
agentport help
agentport help <command>
agentport <command> --help
```

Sem argumentos, `agentport` (ou `help`, `-h`, `--help`) lista todos os comandos
agrupados, cada um com uma linha de resumo. `agentport help <comando>` (ou
`agentport <comando> --help`) mostra a sintaxe e explica o comando e suas
opções, incluindo os aliases. Pedir ajuda nunca executa o comando. Um comando
desconhecido diz isso, aponta para `help` e sai com o código 2.

### `version`

```text
agentport version
agentport --version
agentport -v
```

Exibe a versão do binário. Os instaladores usam esse comando para decidir se
uma atualização é necessária.

## Diretórios

Os perfis e as chaves ficam sob o diretório de configuração. O estado de
execução (bridge, Companions, portas, sockets ControlMaster, logs e cache de
releases) fica sob o diretório de cache. As variáveis abaixo mudam esses
diretórios; caminhos relativos são recusados.

| Variável | Efeito |
| --- | --- |
| `AGENTPORT_CONFIG_DIR` | Base da configuração: `agentport/profiles` e `agentport/keys` ficam dentro dela. Padrão: `os.UserConfigDir()`. |
| `AGENTPORT_CACHE_DIR` | Base do cache: `agentport/bridge.json`, `companions/`, `ports/`, `logs/`, `release.json` e `cm/`. Padrão: `XDG_CACHE_HOME` quando definida (em qualquer sistema), senão `os.UserCacheDir()`. |

Os sockets ControlMaster têm limite de tamanho no Unix. Quando o caminho em
`agentport/cm/` ficaria longo demais, o socket vai para `/tmp/agentport-<uid>/`
(diretório `0700`).

## Idioma

A saída da linha de comando, o `help`, as mensagens dos instaladores e a página
web do Companion usam o idioma escolhido pelo ambiente, nesta ordem:

1. `AGENTPORT_LANG`
2. `LC_ALL`
3. `LC_MESSAGES`
4. `LANG`

A primeira variável definida decide, como na biblioteca C. Valores que começam
com `pt` (`pt`, `pt_BR`, `pt_PT`, `pt_BR.UTF-8`) dão português (Brasil); `es` e
`es_*` dão espanhol; `zh_TW`, `zh_HK`, `zh_MO` e `zh-Hant` dão chinês tradicional
(繁體中文). `C`, `POSIX`, `zh_CN`, `zh-Hans` e qualquer outro valor dão inglês
(en-US), que é o idioma-fonte.

- **CLI e instaladores** (`install.sh`, `install.ps1`): a mesma ordem. Exemplo:
  `LANG=pt_BR.UTF-8 agentport help`, ou `AGENTPORT_LANG=en agentport help` para
  forçar o inglês em um sistema em português.
- **Página do Companion**: `?lang=pt-BR` no endereço, depois o idioma que o
  navegador prefere (`Accept-Language`), depois o idioma detectado pelo processo do
  Companion, e por fim inglês.

Hoje só o português (Brasil) tem catálogo. Espanhol e chinês tradicional são
escolhidos quando o ambiente pede, mas as mensagens continuam em inglês até que os
catálogos existam (planejado). As mensagens de erro técnicas ficam em inglês de
propósito, para poderem ser pesquisadas. O que faltar em um catálogo aparece em
inglês.

## Comandos de integração e internos

### `harness`

```text
agentport harness <install|remove> <agy|opencode|pi> --name NAME
  [--port PORT --token TOKEN]
```

Instala ou remove diretamente a configuração global de AGY, OpenCode ou Pi no
usuário atual. `install` requer `--port` e `--token`. Normalmente este comando
é chamado no servidor por `setup`, `pair`, `connect` e `uninstall`; use-o
manualmente apenas para depurar uma integração.

### `mcp`

```text
agentport mcp
```

Inicia o servidor MCP via `stdio`. Requer `AGENTPORT_BRIDGE_PORT` e
`AGENTPORT_SESSION_TOKEN`, que são configurados automaticamente pelos
adaptadores. Não é necessário executá-lo diretamente em um terminal.

### `bridge`

```text
agentport bridge
agentport bridge stop [--force]
```

`agentport bridge` é o processo interno que recebe a inicialização pelo `stdin`
e mantém o bridge HTTP local. É iniciado pelo Companion; não deve ser executado
manualmente.

`agentport bridge stop` para um bridge que ainda esteja rodando. Ele se recusa
enquanto algum Companion o usar; `--force` para mesmo assim.

## Após reinicializações

Os perfis persistem, mas por padrão o agentport não registra autostart no
sistema. Depois de reiniciar o host, inicie explicitamente o perfil desejado:

```bash
agentport companion start m2
```

Para dispensar isso, ative `agentport companion autostart enable m2`.

Se apenas o servidor remoto reiniciar, um Companion que permaneça vivo no host
tentará restabelecer automaticamente o mesmo túnel, com espera progressiva de
1 a 30 segundos.

## Portas do servidor

O agente pede portas pelas ferramentas MCP `ports_forward`, `ports_unforward`,
`ports_list` e `forwards_list`. Cada pedido aparece no Companion como um pedido
pendente, e só o que você aprovar vira uma porta local. Os comandos abaixo são
a mesma decisão feita pelo terminal, sem o painel.

```text
agentport ports [perfil] [--json] [--all]
agentport requests [--profile NOME]
agentport approve <request-id> (--all | --port N) [--local N] [--duration session|1h|pin] [--profile NOME]
agentport deny <request-id> [--profile NOME]
agentport forwards [--profile NOME] [--json]
agentport unforward <porta...> [--profile NOME]
agentport unforward --all [--profile NOME]
```

- `ports` sem perfil lista as portas da máquina em que roda (o servidor, quando
  executado lá). Com perfil, roda a listagem no servidor por SSH e marca as
  portas que este Companion já encaminha.
- `approve --all` aprova todas as portas do pedido; `--port N` aprova só essa e
  pula as demais. Se a porta local estiver ocupada, outra livre é usada e o
  resultado diz qual.
- `--duration session` (padrão) vale enquanto a sessão do agente durar; `1h` fecha
  sozinho depois de uma hora; `pin` fica aberta até você fechar.
- `unforward` fecha a porta mesmo que outro agente ou o painel a use. Um agente
  só solta o próprio uso dela (`ports_unforward`).

O Companion do perfil precisa estar rodando (`agentport companion start NOME`).
Sem ele, os comandos dizem como iniciá-lo.
