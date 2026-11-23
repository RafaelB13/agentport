#!/usr/bin/env bash
# Harness e2e do agentport.
#
# Sobe um sshd descartável no Docker, que faz o papel do servidor remoto. Depois
# abre um ControlMaster isolado e verifica os forwards `ssh -O forward` / `-O cancel`
# contra portas reais em 127.0.0.1 do host. Veja README.md.
#
# Uso:  test/e2e/run.sh          (KEEP_IMAGE=1 mantém a imagem para depuração)
# Compatível com bash 3.2 (o bash padrão do macOS).

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVER_DIR="$HERE/server"
RUN_ID="$(date +%Y%m%d%H%M%S)-$$"
IMAGE="agentport-e2e-server:$RUN_ID"
NAME="agentport-e2e-sshd-$RUN_ID"
HOST_ALIAS="agentport-e2e"
KEEP_IMAGE="${KEEP_IMAGE:-0}"

TMP=""
KEY=""
CTL=""
SSH=(ssh)
PIDS=()
SSH_PORT=""
SQ_PORT=""
API_LOCAL=""
WEB_LOCAL=""
CONTAINER_CREATED=0
IMAGE_BUILT=0
PASS_N=0
FAIL_N=0
SKIP_N=0

# ---------- registro de resultados ----------

record() { # record PASS|FAIL|SKIP "rótulo" ["detalhe"]
  case "$1" in
    PASS) PASS_N=$((PASS_N + 1)) ;;
    FAIL) FAIL_N=$((FAIL_N + 1)) ;;
    SKIP) SKIP_N=$((SKIP_N + 1)) ;;
  esac
  printf '[%s] %s\n' "$1" "$2"
  if [[ -n "${3:-}" ]]; then printf '%s\n' "$3" | sed 's/^/        | /'; fi
}

check() { # check "rótulo" comando...  PASS se o comando sair com 0; a saída vira detalhe
  local label="$1" out
  shift
  if out="$("$@" 2>&1)"; then
    record PASS "$label" "$out"
  else
    record FAIL "$label" "$out"
  fi
}

abort() { # abort "motivo" ["detalhe"]: registra falha e encerra (o trap imprime o resumo)
  record FAIL "$1" "${2:-}"
  exit 1
}

# ---------- utilitários de porta e HTTP (host) ----------

pick_port() { # pick_port INICIO: primeira porta >= INICIO livre em 127.0.0.1, 0.0.0.0, ::1 e :: (espelha forward.CanListen)
  python3 -c '
import socket, sys

def served(port):
    # Algo aceita TCP em loopback: pega listeners de wildcard que um bind não vê no macOS/BSD.
    for host in ("127.0.0.1", "::1"):
        try:
            socket.create_connection((host, port), timeout=0.2).close()
            return True
        except OSError:
            pass
    return False

def bindable(port, host, family):
    s = socket.socket(family)
    # Igual ao listener do ssh: SO_REUSEADDR deixa bindar porta com TIME_WAIT (ex.: de curls recentes).
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    try:
        s.bind((host, port))
        return True
    except OSError:
        return False
    finally:
        s.close()

def ipv6_ok():
    try:
        return bindable(0, "::1", socket.AF_INET6)
    except OSError:
        return False

hosts = [("127.0.0.1", socket.AF_INET), ("0.0.0.0", socket.AF_INET)]
if ipv6_ok():
    hosts += [("::1", socket.AF_INET6), ("::", socket.AF_INET6)]

start = int(sys.argv[1])
for port in range(start, start + 200):
    if served(port):
        continue
    if all(bindable(port, h, f) for h, f in hosts):
        print(port)
        break
else:
    sys.exit(1)
' "$1"
}

wait_http() { # wait_http URL: até ~10s
  local i
  for i in $(seq 1 40); do
    if curl -fsS --max-time 2 "$1" >/dev/null 2>&1; then return 0; fi
    sleep 0.25
  done
  return 1
}

start_local_server() { # start_local_server PORTA CORPO: processo local do host, só em 127.0.0.1
  python3 "$SERVER_DIR/serve.py" 127.0.0.1 "$1" "$2" >"$TMP/local-$1.log" 2>&1 &
  PIDS+=("$!")
  wait_http "http://127.0.0.1:$1/" || abort "servidor local em 127.0.0.1:$1 não subiu" "$(cat "$TMP/local-$1.log")"
}

body_has() { # body_has URL TRECHO: imprime o corpo e falha se não contiver TRECHO
  local body
  body="$(curl -fsS --max-time 5 "$1")" || return 1
  printf '%s\n' "$body"
  [[ "$body" == *"$2"* ]]
}

listeners_on() { # listeners_on PORTA: nomes dos listeners TCP em PORTA (lsof)
  lsof -nP -iTCP:"$1" -sTCP:LISTEN -F n 2>/dev/null | sed -n 's/^n//p'
}

only_loopback() { # only_loopback PORTA: todo listener em PORTA deve ser 127.0.0.1:PORTA
  local names
  names="$(listeners_on "$1")"
  if [[ -z "$names" ]]; then echo "nenhum listener em $1"; return 1; fi
  printf '%s\n' "$names"
  if printf '%s\n' "$names" | grep -vFx -- "127.0.0.1:$1" >/dev/null; then
    echo "há listener fora de 127.0.0.1"
    return 1
  fi
}

port_closed() { # port_closed PORTA: nada responde nem escuta mais em PORTA
  if curl -fsS --max-time 3 "http://127.0.0.1:$1/" >/dev/null 2>&1; then
    echo "porta $1 ainda responde"; return 1
  fi
  if [[ -n "$(listeners_on "$1")" ]]; then
    echo "porta $1 ainda tem listener: $(listeners_on "$1")"; return 1
  fi
  echo "porta $1 fechada"
}

# ---------- ssh (cliente isolado) ----------

write_ssh_config() { # usa SSH_PORT, KEY, TMP e CTL; nunca toca ~/.ssh
  cat >"$TMP/ssh_config" <<CFG
Host $HOST_ALIAS
  HostName 127.0.0.1
  Port $SSH_PORT
  User dev
  IdentityFile $KEY
  IdentitiesOnly yes
  IdentityAgent none
  UserKnownHostsFile $TMP/known_hosts
  GlobalKnownHostsFile /dev/null
  StrictHostKeyChecking accept-new
  BatchMode yes
  ControlPath $CTL
  ControlMaster no
  LogLevel ERROR
CFG
}

wait_ssh() { # wait_ssh: até ~30s até o sshd aceitar a chave
  local i
  for i in $(seq 1 60); do
    if "${SSH[@]}" -o ConnectTimeout=2 "$HOST_ALIAS" true >/dev/null 2>&1; then return 0; fi
    sleep 0.5
  done
  return 1
}

fwd_add() { # fwd_add LOCAL REMOTA
  "${SSH[@]}" -S "$CTL" -O forward -L "127.0.0.1:$1:127.0.0.1:$2" "$HOST_ALIAS" && echo "forward 127.0.0.1:$1 -> 127.0.0.1:$2 criado"
}

fwd_cancel() { # fwd_cancel LOCAL REMOTA
  "${SSH[@]}" -S "$CTL" -O cancel -L "127.0.0.1:$1:127.0.0.1:$2" "$HOST_ALIAS" && echo "forward 127.0.0.1:$1 cancelado"
}

root_refused() { # login como root deve falhar
  if "${SSH[@]}" -l root "$HOST_ALIAS" true >/dev/null 2>&1; then
    echo "root conseguiu entrar"; return 1
  fi
  echo "login de root recusado"
}

# ---------- container ----------

sshd_setting() { # sshd_setting "chave valor": confere a configuração efetiva (sshd -T) no container
  local out hit
  out="$(docker exec "$NAME" sshd -T 2>/dev/null)" || { echo "sshd -T falhou"; return 1; }
  hit="$(printf '%s\n' "$out" | grep -Fx -- "$1" || true)"
  if [[ -z "$hit" ]]; then echo "esperado '$1' em sshd -T"; return 1; fi
  echo "$hit"
}

only_sshd_published() { # só 22/tcp publicado, e só em 127.0.0.1
  local ports
  ports="$(docker port "$NAME")"
  printf '%s\n' "$ports"
  [[ "$ports" == "22/tcp -> 127.0.0.1:$SSH_PORT" ]]
}

container_listen() { # container_listen ENDEREÇO:PORTA: confere a coluna Local Address do `ss -ltn`
  local hit
  hit="$(docker exec "$NAME" ss -ltn 2>/dev/null | awk 'NR>1{print $4}' | grep -Fx -- "$1" || true)"
  if [[ -z "$hit" ]]; then echo "não escuta em $1"; return 1; fi
  echo "LISTEN $hit"
}

wait_container_listen() { # espera os serviços do dev e o listener do outro usuário subirem
  local addr i
  for addr in 127.0.0.1:3000 127.0.0.1:5173 0.0.0.0:8080 127.0.0.1:5432; do
    for i in $(seq 1 40); do
      if container_listen "$addr" >/dev/null 2>&1; then break; fi
      sleep 0.25
    done
    container_listen "$addr" >/dev/null 2>&1 || return 1
  done
}

gone_container() { # nenhum container com o nome desta execução
  local left
  left="$(docker ps -a --filter "name=^/${NAME}\$" --format '{{.Names}}')"
  if [[ -n "$left" ]]; then echo "sobrou: $left"; return 1; fi
  echo "nenhum container $NAME"
}

gone_image() { # imagem desta execução removida
  if docker image inspect "$IMAGE" >/dev/null 2>&1; then
    echo "imagem $IMAGE ainda existe"; return 1
  fi
  echo "imagem $IMAGE removida"
}

gone_tmp() { # diretório temporário desta execução removido
  if [[ -e "$TMP" ]]; then echo "sobrou $TMP"; return 1; fi
  echo "diretório temporário removido"
}

# ---------- preparo, limpeza e resumo ----------

preflight() {
  local t missing=""
  for t in docker ssh ssh-keygen python3 curl lsof; do
    command -v "$t" >/dev/null 2>&1 || missing="$missing $t"
  done
  if [[ -n "$missing" ]]; then abort "pré-requisitos ausentes:$missing"; fi
  docker info >/dev/null 2>&1 || abort "o daemon do Docker não responde (docker info falhou)"
  record PASS "pré-requisitos disponíveis (docker, ssh, ssh-keygen, python3, curl, lsof)"
}

cleanup() { # remove só o que esta execução criou
  local p
  for p in ${PIDS[@]+"${PIDS[@]}"}; do { kill "$p" && wait "$p"; } 2>/dev/null || true; done
  if [[ -n "$CTL" && -S "$CTL" ]]; then
    "${SSH[@]}" -S "$CTL" -O exit "$HOST_ALIAS" >/dev/null 2>&1 || true
  fi
  if [[ "$CONTAINER_CREATED" == 1 ]]; then docker rm -f "$NAME" >/dev/null 2>&1 || true; fi
  if [[ "$IMAGE_BUILT" == 1 && "$KEEP_IMAGE" != 1 ]]; then docker rmi "$IMAGE" >/dev/null 2>&1 || true; fi
  if [[ -n "$TMP" && "$(basename "$TMP")" == agentport-e2e.* ]]; then rm -rf "$TMP"; fi
}

on_exit() {
  local rc=$?
  trap - EXIT
  set +e
  echo
  echo "== Limpeza =="
  cleanup
  check "container $NAME removido" gone_container
  if [[ "$IMAGE_BUILT" == 1 && "$KEEP_IMAGE" != 1 ]]; then check "imagem $IMAGE removida" gone_image; fi
  check "diretório temporário removido" gone_tmp
  echo "docker ps -a (agentport-e2e):"
  docker ps -a --filter "name=agentport-e2e" --format '  {{.Names}}  {{.Status}}' 2>/dev/null
  echo
  echo "== Resumo: $PASS_N PASS, $FAIL_N FAIL, $SKIP_N SKIP =="
  if (( rc != 0 || FAIL_N > 0 )); then
    echo "RESULTADO: FAIL"
    exit 1
  fi
  echo "RESULTADO: PASS"
}
trap on_exit EXIT

# ---------- fluxo principal ----------

main() {
  preflight

  TMP="$(mktemp -d "${TMPDIR:-/tmp}/agentport-e2e.XXXXXX")"
  KEY="$TMP/id_ed25519"
  CTL="$TMP/ctl"
  ssh-keygen -q -t ed25519 -N '' -C agentport-e2e -f "$KEY" || abort "ssh-keygen falhou"
  record PASS "chave ed25519 descartável gerada (fora de ~/.ssh)"

  if docker build -t "$IMAGE" "$SERVER_DIR" >"$TMP/build.log" 2>&1; then
    IMAGE_BUILT=1
    record PASS "imagem $IMAGE construída"
  else
    abort "docker build falhou" "$(tail -n 40 "$TMP/build.log")"
  fi

  # SYS_PTRACE: um root de servidor real tem essa capability; sem ela o Docker não deixa
  # ler /proc/<pid>/fd de outro usuário, e o portscan como root não veria o dono de 5432.
  CONTAINER_CREATED=1
  docker run -d --name "$NAME" --cap-add SYS_PTRACE -p 127.0.0.1::22 -e "AUTHORIZED_KEY=$(cat "$KEY.pub")" "$IMAGE" >/dev/null \
    || abort "docker run falhou"
  SSH_PORT="$(docker port "$NAME" 22/tcp | awk -F: 'NR==1{print $NF}')"
  [[ -n "$SSH_PORT" ]] || abort "o container não publicou a porta 22"
  record PASS "sshd publicado em 127.0.0.1:$SSH_PORT (porta efêmera do host)"

  write_ssh_config
  SSH=(ssh -F "$TMP/ssh_config")
  wait_ssh || abort "o sshd não aceitou a chave em ~30s" "$(docker logs --tail 40 "$NAME" 2>&1)"
  record PASS "sshd aceita a chave pública (ssh -F isolado)"

  wait_container_listen || abort "serviços do container não subiram" "$(docker exec "$NAME" ss -ltnp 2>&1)"
  record PASS "serviços do container escutando (api, web, pub, other)"

  check "sshd efetivo: PasswordAuthentication no" sshd_setting "passwordauthentication no"
  check "sshd efetivo: KbdInteractiveAuthentication no" sshd_setting "kbdinteractiveauthentication no"
  check "sshd efetivo: PubkeyAuthentication yes" sshd_setting "pubkeyauthentication yes"
  check "sshd efetivo: PermitRootLogin no" sshd_setting "permitrootlogin no"
  check "sshd efetivo: AllowTcpForwarding yes (tunnel -R do agentport)" sshd_setting "allowtcpforwarding yes"
  check "login de root recusado pelo sshd" root_refused
  check "só a porta 22 é publicada (nada além do sshd)" only_sshd_published

  "${SSH[@]}" -M -N -f -o ControlPersist=yes -S "$CTL" "$HOST_ALIAS" || abort "ControlMaster não subiu"
  check "ControlMaster ativo" "${SSH[@]}" -S "$CTL" -O check "$HOST_ALIAS"

  # Caso de conflito: ocupa a primeira porta livre a partir de 3000 com um processo local.
  SQ_PORT="$(pick_port 3000)" || abort "não há porta livre a partir de 3000"
  start_local_server "$SQ_PORT" "ocupante-$SQ_PORT"
  record PASS "porta local $SQ_PORT ocupada de propósito (caso de conflito)"

  API_LOCAL="$(pick_port 3000)" || abort "não há porta livre para o forward de 3000"
  [[ "$API_LOCAL" != "$SQ_PORT" ]] || abort "o forward de 3000 caiu na porta ocupada $SQ_PORT"
  record PASS "forward de 3000 remapeado: 3000 -> local $API_LOCAL (ocupante em $SQ_PORT)"
  check "forward 3000 -> 127.0.0.1:$API_LOCAL criado" fwd_add "$API_LOCAL" 3000

  WEB_LOCAL="$(pick_port 5173)" || abort "não há porta livre para o forward de 5173"
  if [[ "$WEB_LOCAL" == 5173 ]]; then
    record PASS "forward de 5173 manteve a mesma porta local (estava livre)"
  else
    record SKIP "5173 indisponível agora (ocupada no host); forward usou a próxima livre: $WEB_LOCAL"
  fi
  check "forward 5173 -> 127.0.0.1:$WEB_LOCAL criado" fwd_add "$WEB_LOCAL" 5173

  echo "Portas escolhidas: ssh=$SSH_PORT api=$API_LOCAL web=$WEB_LOCAL ocupante=$SQ_PORT"

  check "GET via forward 3000 (127.0.0.1:$API_LOCAL) contém api-3000" \
    body_has "http://127.0.0.1:$API_LOCAL/" "api-3000"
  check "GET via forward 5173 (127.0.0.1:$WEB_LOCAL) contém web-5173" \
    body_has "http://127.0.0.1:$WEB_LOCAL/" "web-5173"
  check "ocupante local em $SQ_PORT segue intacto (não foi sequestrado)" \
    body_has "http://127.0.0.1:$SQ_PORT/" "ocupante-$SQ_PORT"

  check "lsof: forward 3000 escuta só em 127.0.0.1 (não em *)" only_loopback "$API_LOCAL"
  check "lsof: forward 5173 escuta só em 127.0.0.1 (não em *)" only_loopback "$WEB_LOCAL"

  check "-O cancel do forward 3000" fwd_cancel "$API_LOCAL" 3000
  check "porta $API_LOCAL para de responder após o cancel" port_closed "$API_LOCAL"
  check "-O cancel do forward 5173" fwd_cancel "$WEB_LOCAL" 5173
  check "porta $WEB_LOCAL para de responder após o cancel" port_closed "$WEB_LOCAL"
  check "ocupante local segue respondendo após os cancels" \
    body_has "http://127.0.0.1:$SQ_PORT/" "ocupante-$SQ_PORT"

  echo "== Baseline: portas em LISTEN dentro do container (ss -ltnp) =="
  check "ss -ltnp dentro do container" docker exec "$NAME" ss -ltnp
  check "container: api em 127.0.0.1:3000" container_listen 127.0.0.1:3000
  check "container: web em 127.0.0.1:5173" container_listen 127.0.0.1:5173
  check "container: pub em 0.0.0.0:8080" container_listen 0.0.0.0:8080
  check "container: other em 127.0.0.1:5432" container_listen 127.0.0.1:5432
  check "container: sshd em 0.0.0.0:22" container_listen 0.0.0.0:22
  if container_listen '[::1]:9229' >/dev/null 2>&1; then
    check "container: tcp em [::1]:9229" container_listen '[::1]:9229'
  else
    record SKIP "container sem IPv6: [::1]:9229 não foi iniciado"
  fi
}

main "$@"
