# Servidor "remoto" descartável do harness e2e do agentport.
# Só o sshd é publicado. Os serviços de dev e o listener do outro usuário
# ficam restritos ao container.
FROM debian:stable-slim

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
 && apt-get install -y --no-install-recommends openssh-server python3 iproute2 \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --create-home --shell /bin/bash dev \
 && useradd --create-home --shell /bin/bash other \
 && mkdir -p /run/sshd /opt/e2e \
 && rm -f /etc/ssh/ssh_host_*

COPY sshd_config /etc/ssh/sshd_config
COPY serve.py entrypoint.sh /opt/e2e/
RUN chmod 755 /opt/e2e/serve.py /opt/e2e/entrypoint.sh

EXPOSE 22
ENTRYPOINT ["/opt/e2e/entrypoint.sh"]
