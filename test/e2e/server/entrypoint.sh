#!/bin/sh
# Entrypoint do servidor e2e: instala a chave pública do cliente, sobe os
# serviços do usuário dev, o listener do usuário other e, por fim, o sshd.
set -eu

if [ -z "${AUTHORIZED_KEY:-}" ]; then
  echo "entrypoint: AUTHORIZED_KEY não definido" >&2
  exit 1
fi

# Chaves de host são geradas aqui, e não na imagem, para cada container ter as suas.
ssh-keygen -A >/dev/null
mkdir -p /run/sshd

install -d -m 700 -o dev -g dev /home/dev/.ssh
printf '%s\n' "$AUTHORIZED_KEY" > /home/dev/.ssh/authorized_keys
chown dev:dev /home/dev/.ssh/authorized_keys
chmod 600 /home/dev/.ssh/authorized_keys

# Serviços do usuário dev: o que o agentport deve encaminhar.
runuser -u dev -- python3 /opt/e2e/serve.py 127.0.0.1 3000 api-3000 &
runuser -u dev -- python3 /opt/e2e/serve.py 127.0.0.1 5173 web-5173 &
runuser -u dev -- python3 /opt/e2e/serve.py 0.0.0.0 8080 pub-8080 &

# [::1]:9229 só sobe se o container tiver IPv6.
if python3 -c 'import socket; s = socket.socket(socket.AF_INET6); s.bind(("::1", 9229))' 2>/dev/null; then
  runuser -u dev -- python3 /opt/e2e/serve.py ::1 9229 tcp-9229 --tcp &
else
  echo "entrypoint: IPv6 indisponível; [::1]:9229 não iniciado" >&2
fi

# Outro usuário: para a verificação futura de processos de outro usuário (sem pid).
runuser -u other -- python3 /opt/e2e/serve.py 127.0.0.1 5432 other-5432 &

exec /usr/sbin/sshd -D -e
