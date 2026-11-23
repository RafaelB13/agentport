# Harness e2e do agentport

O harness sobe um servidor "remoto" descartável no Docker e verifica, de ponta a
ponta, o caminho que o agentport usa para mapear portas: um ControlMaster do ssh
por servidor e `ssh -S <ctl> -O forward -L 127.0.0.1:L:127.0.0.1:R` / `-O cancel`.
Tudo acontece com 127.0.0.1 e portas efêmeras, sem tocar em `~/.ssh` nem em
containers ou imagens que não são deste harness.

## Requisitos

- macOS ou Linux com `bash`, `ssh`, `ssh-keygen`, `python3`, `curl` e `lsof`.
- Docker com o daemon ativo (`docker info` precisa responder). Testado com
  Docker 29 e Compose v5 em macOS arm64.
- Acesso ao Docker Hub para baixar `debian:stable-slim` na primeira execução.

## Como rodar

```sh
test/e2e/run.sh                     # script direto; imprime PASS/FAIL e sai com 1 se algo falhar
go test -tags e2e -v -count=1 ./test/e2e/   # mesmo harness, via Go
KEEP_IMAGE=1 test/e2e/run.sh        # mantém a imagem local para depurar (a limpeza ainda roda)
```

O `go test` sem a tag `e2e` não roda nada disso.

## O que o script faz

1. Gera uma chave ed25519 descartável em um diretório temporário.
2. Constrói `test/e2e/server/` como `agentport-e2e-server:<run-id>` e sobe o
   container `agentport-e2e-sshd-<run-id>` com `-p 127.0.0.1::22`. A porta do
   host é lida com `docker port`.
3. Espera o sshd aceitar a chave usando um `ssh -F` isolado. A configuração e o
   `known_hosts` ficam no diretório temporário.
4. Confere a configuração efetiva do sshd (`sshd -T`): sem senha, sem root,
   `AllowTcpForwarding local`. Confere também que só a porta 22 é publicada.
5. Abre o ControlMaster (`ssh -M -N -f -S <ctl> -o ControlPersist=yes`).
6. **Conflito:** ocupa a primeira porta livre a partir de 3000 com um processo
   local. O forward de 3000 precisa ser remapeado para a próxima livre, e
   `curl` precisa chegar em `api-3000`. O ocupante tem que continuar com a
   resposta dele.
7. Cria os forwards 3000 e 5173. 5173 tenta a mesma porta e, se estiver livre,
   tem que manter a porta.
8. Confere com `curl` os corpos `api-3000` e `web-5173`.
9. Confere com `lsof -nP -iTCP:<L> -sTCP:LISTEN` que cada listener está em
   `127.0.0.1:<L>` e nunca em `*`.
10. Cancela cada forward com `-O cancel`. Confere que a porta para de responder
    e que o ocupante local continua intacto.
11. Imprime `ss -ltnp` de dentro do container como linha de base para o teste
    futuro do portscan, e confere os endereços esperados.
12. Na limpeza (trap `EXIT`), faz `ssh -O exit`, `docker rm -f` do próprio
    container, `docker rmi` da própria imagem (se `KEEP_IMAGE` não for 1) e apaga
    o diretório temporário. No fim, confere que nada disso sobrou.

## Serviços dentro do container

| Usuário | Endereço        | Corpo        | Observação                          |
|---------|-----------------|--------------|-------------------------------------|
| dev     | 127.0.0.1:3000  | `api-3000`   | encaminhado pelo harness            |
| dev     | 127.0.0.1:5173  | `web-5173`   | encaminhado pelo harness            |
| dev     | 0.0.0.0:8080    | `pub-8080`   | não publicado; só dentro do container |
| dev     | [::1]:9229      | TCP `tcp-9229` | só se o container tiver IPv6      |
| other   | 127.0.0.1:5432  | `other-5432` | para a verificação futura sem pid   |
| (sshd)  | 0.0.0.0:22      | chave ed25519 | a única porta publicada no host    |

## Como ler a saída

Cada check sai como `[PASS]`, `[FAIL]` ou `[SKIP]`, com o detalhe indentado
logo abaixo (a resposta do curl, a linha do lsof, a saída do ss). O resumo final
diz `RESULTADO: PASS` ou `RESULTADO: FAIL`, e o código de saída é 1 em caso de
falha. Um `[SKIP]` não é falha: por exemplo, o container pode não ter IPv6.

## Como depurar

- Falha em "sshd aceita a chave": o script imprime as últimas linhas de
  `docker logs`. Confira se o container ainda está rodando e se a porta publicada
  responde (`docker port agentport-e2e-sshd-<run-id>`).
- Falha no `docker build`: o script imprime as últimas 40 linhas do log. Para ver
  tudo, rode `docker build -t x test/e2e/server`.
- Quer inspecionar depois da execução: rode `KEEP_IMAGE=1 test/e2e/run.sh`, mas
  o container é removido sempre. Para deixá-lo de pé, comente a linha
  `docker rm -f` do `cleanup`.
- Container órfão de uma execução que foi morta com `kill -9` (o trap não roda):
  `docker ps -a --filter name=agentport-e2e-sshd-` mostra os restos. Remova só os
  que você reconhece, com `docker rm -f <nome>`.
- Porta ocupada por outro processo do host: o harness remapeia sozinho, e isso é
  esperado. O `lsof -nP -iTCP:<porta> -sTCP:LISTEN` mostra quem está usando.

## Limitações

- Testa só o caminho de forwards e do ControlMaster. Não testa o binário do
  agentport, porque a fase A não importa pacotes do repositório.
- Não cobre o portscan com pid de processos de outro usuário (fase B).
- Depende do Docker Desktop para o `127.0.0.1::22` no macOS. Em Linux, a
  publicação em `127.0.0.1` usa o `docker-proxy` ou o iptables do daemon.
- O sshd roda como PID 1 e não há init que colete zumbis. Os processos de dev têm
  vida curta e isso não afeta os checks.
