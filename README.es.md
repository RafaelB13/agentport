# agentport

[English](README.md) | [Português (Brasil)](README.pt-BR.md) | **Español** | [繁體中文](README.zh-Hant.md)

agentport permite que un agente de código que corre en un servidor SSH use los
puertos de tu máquina, con tu autorización. El agente puede ver qué está
escuchando en el servidor y pedir que se abra un puerto en tu máquina, a través
de la conexión SSH que ya tienes. Tú decides qué se abre y cuándo se cierra.

> **En desarrollo.** La base ya funciona: el Companion, el túnel SSH inverso, el
> servidor MCP, el registro automático en los agentes, los instaladores y los
> releases verificables. Las herramientas de puertos, el panel y el inicio
> automático todavía se están construyendo. La tabla de estado de abajo indica
> qué está listo y qué está planificado.

## Qué es

agentport tiene tres partes:

- **El Companion**, un proceso en segundo plano en tu máquina. Mantiene un puente
  local en `127.0.0.1`, sostiene un túnel SSH inverso hacia cada servidor
  emparejado y se reconecta con una espera de 1 a 30 segundos cuando el enlace se
  cae.
- **El servidor MCP** (`agentport mcp`), que el agente de código inicia en el
  servidor mediante `stdio`. agentport lo registra en los agentes que encuentra
  allí.
- **El panel** (planificado), una página en tu máquina donde apruebas, ves,
  editas y cierras los puertos que el agente pidió.

Hoy puedes configurar un servidor, conectarte con `ssh` y usar el agente con la
herramienta `agentport_status`, que informa el estado del túnel y si existe un
release más nuevo. Las herramientas de puertos están planificadas.

> **Marcador para una captura de pantalla.** El panel y la página del Companion
> se mostrarán aquí cuando llegue el panel (fase F5).

agentport es una bifurcación de [AgentClip](https://github.com/wendellrocha/agentclip),
que va en el sentido contrario (de tu máquina al servidor). Consulta
[Créditos](#créditos).

## Estado

| Fase | Qué es | Estado |
| --- | --- | --- |
| F1 | Base del proyecto: bifurcación de AgentClip, renombrado, clipboard eliminado. CLI, Companion, `setup`, servidor MCP con `agentport_status`, releases | ✓ Listo |
| F3 | Motor de forward (`internal/forward`) y revisión de quién ocupa un puerto del host (`internal/hostport`) | ✓ Listo en el código; ningún comando lo usa todavía |
| F1b | Fórmula de Homebrew generada por el workflow de release, `agentport upgrade` consciente de Homebrew, estos cuatro READMEs | En curso |
| F2 | Escáner de puertos del servidor (`internal/portscan`), Linux y macOS, con filtros para demonios del sistema | ✓ Listo en el código; el comando `agentport ports` está planificado |
| F4 | Herramientas MCP de puertos (`ports_list`, `ports_forward`, `ports_unforward`, `forwards_list`), endpoints del puente, aprobaciones, sesiones | Planificado |
| F5 | Panel, inicio automático al conectar (`LocalCommand`), notificaciones, diálogo de puerto ocupado | Planificado |
| F5b | Terminal web hacia el servidor, en el panel | Planificado |
| F6 | Host Windows: forwards sin ControlMaster | Planificado |
| Pruebas | Harness con Docker en `test/e2e`: un sshd real, forwards por ControlMaster, conflictos de puertos y el escáner en Linux | ✓ Listo |
| F7 | Validación de extremo a extremo en servidores reales, release `v0.1.0` | Planificado |

Lo que está marcado como *planificado* no está en la versión actual. No cuentes
con ello todavía.

## Cómo funciona

```text
HOST (tu máquina)                            SERVIDOR (alcanzado por SSH)

 Companion                                    agente (Claude Code, Codex, ...)
   puente local en 127.0.0.1                    |  stdio, iniciado por el agente
        ^                                       v
        |                                      agentport mcp
        +---- túnel SSH inverso (ssh -R) -------+
              una conexión por servidor
```

1. `agentport setup` instala agentport en el servidor, registra su servidor MCP
   en los agentes que encuentra allí, guarda un **perfil** en tu máquina (el
   servidor, un token de emparejamiento y un puerto remoto) e inicia el Companion.
2. El Companion abre un `ssh` hacia el servidor con un túnel inverso. Por el túnel,
   el puente queda accesible para el servidor, de modo que `agentport mcp` lo
   alcanza en un puerto del loopback del servidor.
3. El agente inicia `agentport mcp` en el servidor mediante `stdio`. El servidor
   MCP habla con el puente con un token de sesión, a través del túnel.
4. Planificado: las herramientas de puertos piden al Companion que abra un puerto.
   El Companion lo abre por la misma conexión SSH (`ssh -O forward -L`, con
   ControlMaster en macOS y Linux), de modo que el puerto llega solo al
   `127.0.0.1` de tu máquina. Nada escucha en tu red.

Cada servidor tiene su propio perfil y su propio Companion, así que dos servidores
nunca comparten token ni túnel.

## Requisitos

**Tu máquina (host)**

- macOS (Apple silicon o Intel), Linux (amd64 o arm64) o Windows (amd64). El
  instalador de Windows y el Companion se compilan y prueban en el CI; el motor de
  forward para Windows está planificado (F6).
- Cliente OpenSSH (`ssh`) en el `PATH`. El Companion lo usa para el túnel, así que
  lee tu `~/.ssh/config`, tus claves y el ssh-agent como de costumbre.
- Go 1.27 o más nuevo, solo si compilas desde el código fuente.

**El servidor**

- Linux o macOS, amd64 o arm64, con un servidor SSH al que puedas entrar. El primer
  `setup` puede pedir la contraseña una vez; después se usa una clave.
- Un shell POSIX, porque `setup` ejecuta por SSH un conjunto corto y fijo de
  comandos.
- Al menos un agente de código compatible instalado (ver la tabla de abajo).
  agentport no instala agentes.

**Agentes compatibles (harnesses)**

agentport se integra con el *harness*, el programa que ejecuta el agente y llama
a sus herramientas, no con el modelo que usa. `setup`, `pair` y `connect` detectan
los harnesses ya instalados y configuran solo esos.

| Harness | Estado | Integración |
| --- | --- | --- |
| Claude Code | Compatible | Se detecta y se configura automáticamente si está instalado |
| Codex | Compatible | Se detecta y se configura automáticamente si está instalado |
| Gemini CLI | Compatible | Se detecta y se configura automáticamente si está instalado |
| AGY / Antigravity CLI | Compatible | agentport crea o actualiza la entrada MCP global |
| OpenCode | Compatible | agentport crea o actualiza la entrada MCP global |
| Pi Coding Agent | Compatible | agentport instala una extensión global |
| Otro cliente MCP | Manual | Apúntalo a `agentport mcp` y define tú las variables del perfil |

DeepSeek y MiMo son modelos, no harnesses: úsalos a través de un harness
compatible, como Pi u OpenCode.

## Instalación

### Homebrew

> **Planificado.** El workflow de release ya genera la fórmula, pero el tap
> `rafaelb13/homebrew-tap` todavía no está publicado, así que los comandos de abajo
> aún no funcionan. Usa el script de instalación o compila desde el código fuente
> hasta que exista el tap.

Cuando el tap esté publicado:

```bash
brew install rafaelb13/tap/agentport
agentport version
brew upgrade agentport
brew uninstall agentport
```

Homebrew es el dueño de la instalación. Si instalaste con Homebrew, `agentport
upgrade` no reemplaza el binario: imprime `agentport was installed with Homebrew.
Run: brew upgrade agentport` y termina con estado 0. El inicio automático no
necesita `brew services`: el Companion arranca con `agentport companion autostart`
(consulta [Inicio automático](#inicio-automático)).

### Script de instalación

En macOS o Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/rafaelb13/agentport/main/scripts/install.sh | sh
```

Para elegir una versión o un directorio:

```bash
curl -fsSL https://raw.githubusercontent.com/rafaelb13/agentport/main/scripts/install.sh | sh -s -- --version vX.Y.Z --install-dir "$HOME/.local/bin"
```

En Windows, en PowerShell:

```powershell
irm https://raw.githubusercontent.com/rafaelb13/agentport/main/scripts/install.ps1 | iex
```

Para pasar una versión, descarga el script y ejecútalo con `-Version`:

```powershell
.\install.ps1 -Version vX.Y.Z
```

Los scripts detectan tu plataforma, consultan el release más reciente, verifican el
release (ver abajo) y comparan con la versión instalada. Solo descargan cuando el
release es más nuevo, así que volver a ejecutarlos es seguro. Opciones:

| Opción | Shell | PowerShell | Variable de entorno |
| --- | --- | --- | --- |
| Versión | `--version vX.Y.Z` | `-Version vX.Y.Z` | `AGENTPORT_VERSION` |
| Directorio de instalación | `--install-dir RUTA` | `-InstallDir RUTA` | `AGENTPORT_INSTALL_DIR` |
| Repositorio | | | `AGENTPORT_REPOSITORY` |
| Omitir la comprobación de atestado | | | `AGENTPORT_SKIP_ATTESTATION=1` |
| Idioma | | | `AGENTPORT_LANG` |

En Windows, el directorio predeterminado es `%LOCALAPPDATA%\Programs\agentport\bin`.
Los releases también aparecen en la página de
[GitHub Releases](https://github.com/rafaelb13/agentport/releases).

### Desde el código fuente

```bash
git clone https://github.com/rafaelb13/agentport.git
cd agentport
go build -o agentport ./cmd/agentport
```

Pon el `agentport` resultante en tu `PATH`. Un binario compilado desde el código
fuente no tiene número de release, así que `agentport version` no muestra un número
de release y la comprobación de actualización no se aplica a él.

### Verificar un release

Cada release publica `checksums.txt`, firmado con
[cosign](https://github.com/sigstore/cosign) (keyless, ligado a la identidad del
workflow de release), y un atestado de build de GitHub. Cada archivo tiene una
entrada SHA-256, y el atestado cubre los archivos, los SBOM y los checksums. Para
verificar a mano, descarga el `checksums.txt`, el `checksums.txt.bundle` y el
`attestation.jsonl` del release:

```bash
VERSION=vX.Y.Z
# La identidad exige la etiqueta exacta del release, así que una ejecución desde otra rama no se acepta.
IDENTITY="https://github.com/rafaelb13/agentport/.github/workflows/release.yml@refs/tags/${VERSION}"

cosign verify-blob \
  --bundle checksums.txt.bundle \
  --certificate-identity "$IDENTITY" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt

# Linux
sha256sum --check --ignore-missing checksums.txt
# macOS (no tiene sha256sum ni --ignore-missing)
ARCH=$(uname -m | sed s/x86_64/amd64/)
grep " agentport_${VERSION}_darwin_${ARCH}.tar.gz$" checksums.txt | shasum -a 256 --check

gh attestation verify "agentport_${VERSION}_linux_amd64.tar.gz" \
  --repo rafaelb13/agentport \
  --cert-identity "$IDENTITY"
```

`gh attestation verify` sin `--bundle` consulta a GitHub y necesita `gh auth login`.
Para verificar sin iniciar sesión, agrega `--bundle attestation.jsonl`.

**Comprobación automática.** Desde `v0.7.1-rc.1` en adelante, los instaladores y
`agentport upgrade` confirman, además del SHA-256, que el archivo lo produjo el
workflow de release de este repositorio en la etiqueta que se está instalando. Si
esa confirmación falla, la instalación se rechaza. El script de instalación usa
`gh attestation verify` cuando `gh` está instalado y, si no, consulta la API de
atestados de GitHub. `agentport upgrade` usa la API. Las versiones anteriores a
`v0.7.1-rc.1` no tienen atestado; se instalan solo con la comprobación de SHA-256, y
el instalador lo indica.

Si la API de atestados no responde o se agotó el límite de solicitudes, la
instalación se rechaza en lugar de continuar sin la comprobación. Para instalar solo
con la comprobación de SHA-256, define `AGENTPORT_SKIP_ATTESTATION=1`. `agentport
setup` y `agentport upgrade` aplican la misma configuración a los binarios que
envían a los servidores.

## Configurar un servidor

`agentport setup` configura un servidor en un solo comando:

```bash
agentport setup bastion-m2 --profile m2
ssh bastion-m2
codex
```

Aquí, `bastion-m2` es un alias SSH de tu `~/.ssh/config`. Luego pide al agente que
compruebe el enlace: "Verifica el estado de agentport.". Llama a `agentport_status`.

La sintaxis de `setup`:

```text
agentport setup <destino-ssh> [--profile NOMBRE] [--agent all|codex|claude|gemini|agy|opencode|pi] [--version vX.Y.Z] [--remote-port 39123] [--skip-agent] [--skip-install] [--no-start]
```

Qué hace `setup`, en orden:

1. **Comprueba SSH.** Primero intenta un inicio de sesión con clave sin interacción.
   Si funciona, tu configuración SSH actual se mantiene tal cual.
2. **Crea una clave dedicada si hace falta.** Si el servidor solo acepta contraseña,
   `setup` crea una clave Ed25519 que usa solo agentport, pide la contraseña una vez
   para añadir la clave al `authorized_keys` del servidor y usa esa clave en los
   túneles siguientes. La clave está en el directorio de configuración de tu usuario
   (`agentport/keys/`), con permisos privados.
3. **Instala el binario en el servidor**, en `~/.local/bin/agentport`. Esta máquina
   descarga y verifica el binario de la plataforma del servidor (lee el `uname` del
   servidor) y lo envía por SSH. Un servidor que ya tiene la misma versión o una más
   nueva no se modifica. Para omitir este paso, usa `--skip-install`.
4. **Registra agentport en los agentes** que encuentra en el servidor, salvo que uses
   `--skip-agent`. `--agent` lo limita a un harness.
5. **Guarda el perfil** en tu máquina, con el destino, el token de emparejamiento y
   el puerto remoto. Los perfiles están en `<directorio de configuración del
   usuario>/agentport/profiles/` (en macOS, `~/Library/Application
   Support/agentport/profiles/`). El archivo guarda el token de emparejamiento, así
   que mantenlo privado.
6. **Inicia el Companion**, salvo que uses `--no-start`.

Volver a ejecutar `setup` para el mismo perfil genera un nuevo token de
emparejamiento y detiene el Companion que estaba corriendo para él.

### Opciones

| Opción | Significado |
| --- | --- |
| `--profile NOMBRE` | Nombre local del emparejamiento. Si no se indica, se deriva del destino. |
| `--agent NOMBRE` | Limita la configuración a un harness: `codex`, `claude`, `gemini`, `agy`, `opencode`, `pi` o `all` (predeterminado). |
| `--version vX.Y.Z` | Release que se instala en el servidor. |
| `--remote-port N` | Puerto del loopback del servidor que usa el túnel inverso. Predeterminado `39123`. |
| `--skip-agent` | No registra la integración MCP en el servidor. |
| `--skip-install` | No instala ni actualiza el binario en el servidor. |
| `--no-start` | Termina sin iniciar el Companion. |

### Destinos en el servidor

El destino es lo que le darías a `ssh`: un alias de `~/.ssh/config`, un nombre de
host o `usuario@host`. El puerto, la clave y el salto viven en la configuración de
SSH, así que usa un alias para un servidor que los necesite:

```text
# ~/.ssh/config
Host bastion-m2
  HostName 10.0.0.12
  User rafael
  Port 2222
  IdentityFile ~/.ssh/id_ed25519
  ProxyJump bastion
```

**Planificado:** `setup` recibirá el puerto, la clave y el host de salto como
opciones (`-p`, `-i`, `-J`), y también direcciones IPv6 con puerto
(`usuario@[2001:db8::12]:2222`). Hasta entonces, usa un alias.

## Inicio automático

El Companion corre en tu máquina, así que tiene que estar activo para que exista el
túnel. Hay dos formas de que se inicie solo.

**Al iniciar sesión (disponible ahora).** `agentport companion autostart enable m2`
instala el mecanismo de inicio de sesión del propio sistema: un LaunchAgent en
`~/Library/LaunchAgents` en macOS; una unidad de usuario de systemd en
`~/.config/systemd/user` en Linux; y una tarea del Programador de tareas que se
ejecuta al iniciar sesión en Windows. El servicio ejecuta `agentport companion serve
m2` y lo reinicia si falla. No abre un segundo Companion cuando ya hay uno activo.

```bash
agentport companion autostart enable m2
agentport companion autostart status m2
agentport companion autostart disable m2
```

`disable` detiene el Companion que inició el servicio. Un Companion que iniciaste tú
mismo sigue corriendo.

**Al conectarte al servidor (planificado, F5).** `setup` añadirá, después de
preguntarte, un bloque a tu `~/.ssh/config`, para que conectarte al servidor inicie
el Companion si no está corriendo:

```text
# agentport: perfil m2
Match host 10.0.0.12 user rafael
  PermitLocalCommand yes
  LocalCommand agentport companion ensure m2
```

El bloque estará delimitado por su comentario `# agentport: perfil`, así que borrar
esas líneas lo quita. Hasta entonces, no hay bloque que quitar.

**Si el servidor se reinicia** mientras el Companion sigue corriendo en tu máquina,
el Companion restablece el túnel por sí mismo, con una espera de 1 a 30 segundos. Se
vuelven a usar el mismo perfil y el mismo puerto remoto.

## Uso diario

Conéctate al servidor como de costumbre e inicia el agente:

```bash
ssh bastion-m2
codex
```

Comprueba el enlace cuando quieras, pidiéndole al agente:

> Verifica el estado de agentport.

El agente llama a `agentport_status`, que te dice si el túnel hacia tu Companion
está conectado y si hay un release estable más nuevo. Para ver lo mismo en tu
máquina:

```bash
agentport companion status m2
```

**Planificado (F4 y F5).** Cuando lleguen las herramientas de puertos, podrás pedir:

> Mapea los puertos de este proyecto y abre la app en localhost.

El agente lista los puertos en escucha del servidor y pide abrir los que necesita,
con un motivo para cada uno. Cada solicitud aparece en el panel como una sola
solicitud por lotes, aunque tenga muchos puertos; los apruebas o rechazas todos
juntos o uno a uno. Los puertos aprobados se abren en `127.0.0.1` de tu máquina, y
el agente te indica la URL local. Si el puerto local está ocupado, agentport usa el
siguiente libre y lo avisa.

## El panel

**Planificado (F5).** El panel es una página en tu máquina, servida en `127.0.0.1` y
protegida por un token. Mostrará:

- **Solicitudes del agente** arriba: puerto, motivo, comando, aprobar o rechazar, y
  cuánto dura la aprobación (esta sesión, una hora o siempre). Una solicitud por
  lotes se convierte en una tarjeta con casillas y "Aprobar todas".
- **Puertos abiertos** como `:remoto -> localhost:local`, con nombre, comando,
  tráfico en vivo, abrir en una pestaña nueva, fijar, detalles, editar y cerrar.
  Cerrar pide confirmación y muestra qué sesiones de agente serán avisadas.
- **Agrupar y cerrar varios**: agrupar por solicitud, por sesión de agente o en
  lista plana; seleccionar varios puertos; cerrar un grupo o la selección con una
  sola confirmación.
- **Cambiar de servidor** con puertos abiertos pregunta si quieres cerrarlos,
  mantenerlos o cancelar, con la opción de recordar la elección.
- **Puertos cerrados**, con la hora y quién los cerró, y **reabrir**, que usa la
  misma configuración o propone un puerto local libre si el anterior está ocupado.
- **Puertos detectados** en el servidor y un menú para abrir uno a mano (atajo `/`).
- **Detalles del proceso** bajo demanda: PID, usuario, CPU, memoria, directorio,
  proceso padre, contenedor, línea de comando, salud del puerto y el motivo del
  agente.
- **Actividad**, notificaciones, atajos y una **barra superior** con el servidor
  actual, el cambio de servidor, la terminal web, la política de aprobación, el
  idioma, el tema (sistema, claro, oscuro), la ayuda y el fin de la sesión.
- **Terminal web** (F5b): una terminal hacia el servidor que se abre desde el panel.
  Usa la conexión SSH del perfil y se queda en `127.0.0.1` con el token del panel.
  Se puede desactivar por perfil.
- **Conectar a un servidor**: sugerencias encontradas en tu máquina (ver
  [Modo standalone](#modo-standalone)).

**Hoy**, la página del Companion es una vista operativa sencilla, en `127.0.0.1`,
con un token que abres con `agentport companion open m2`. Muestra el perfil y el
destino, el estado del túnel (`Conectado` o `Desconectado`) con el último error, un
aviso cuando existe un release estable más nuevo, y el botón **Parar Companion**,
que equivale a `agentport companion stop m2`. No transfiere archivos.

## Terminal

El panel tiene una pestaña **Terminal** que abre una shell en el servidor conectado, para ejecutar un comando rápido sin salir del navegador. Reutiliza la conexión SSH del túnel (sin un segundo inicio de sesión), se sirve solo al panel en `127.0.0.1` y acepta conexiones únicamente desde la página propia del panel. Pueden estar abiertos hasta cuatro terminales a la vez. Cerrar el terminal, o detener el Companion, termina la sesión SSH en el servidor. El contenido del terminal nunca se escribe en los registros. En Windows, el terminal no está disponible en esta versión.

## Herramientas MCP

El agente ve estas herramientas a través del servidor MCP en el servidor.

| Herramienta | Entrada | Devuelve |
| --- | --- | --- |
| `agentport_status` | ninguna | **Disponible ahora.** El estado del túnel, la versión de agentport y si existe un release estable más nuevo (en ese caso recomienda `agentport upgrade`). |
| `ports_list` | `include_system?`, `only_user?` | **Planificado.** Los puertos TCP en escucha del servidor: puerto, dirección de bind, protocolo, PID, proceso, una línea de comando corta y si ya existe un forward. |
| `ports_forward` | `reason`, `group?`, `ports[]` de `{remote_port, label?, path?}` | **Planificado.** Pide tu aprobación como una solicitud por lotes. Por cada puerto devuelve el estado (`active`, `pending_approval`, `denied`, `skipped`), el puerto local realmente usado y la URL con el `path`. Espera hasta 2 minutos; si no has respondido, devuelve `pending_approval`, y el agente puede consultar `forwards_list` después. |
| `ports_unforward` | `ports[]` o `all` | **Planificado.** Cierra los forwards de esta sesión. Nunca cierra un puerto de otra sesión. |
| `forwards_list` | ninguna | **Planificado.** Los forwards activos con su origen (agente, automático o manual), el tiempo activo y los bytes. Los forwards de otras sesiones aparecen marcados como tales y esta sesión no puede cerrarlos. |

El agente nunca elige tu puerto local, y ninguna herramienta le permite detener un
proceso en tu máquina. Detener un proceso es decisión tuya, tomada en el panel.

Si el Companion no está corriendo cuando se llama a una herramienta, la herramienta
responde que el Companion está fuera de línea e indica al agente que ejecute
`agentport companion start <perfil>`, que el agente te transmite.

## Varias sesiones de agente

**Planificado (F4).** Un Companion atiende a cada servidor. Cada sesión de agente en
ese servidor (por ejemplo, dos sesiones de Claude Code en proyectos distintos, o
Claude Code y Codex) ejecuta su propio `agentport mcp`, y todas hablan con el mismo
Companion. Cada sesión se identifica por su agente, su PID, la carpeta del proyecto
y la hora en que empezó.

- Solo el Companion elige puertos locales, una solicitud a la vez; por eso dos
  sesiones nunca reciben el mismo puerto local.
- Si dos sesiones piden el mismo puerto remoto, se abre una sola vez. Ambas reciben
  la misma URL y son sus dueñas. El puerto se cierra cuando ninguna dueña lo necesita
  o cuando tú lo cierras.
- `ports_unforward` solo quita a la sesión que lo llama de la lista de dueñas de sus
  puertos. Un puerto que otra sesión todavía usa sigue abierto.
- Cuando una sesión termina, los puertos que solo ella usaba quedan marcados como
  "sesión terminada" y se cierran a los 20 minutos, salvo que los hayas fijado. Si el
  agente vuelve a la misma carpeta en ese plazo, los recupera.

## Puertos ocupados en el host

Cuando un puerto que quieres ya está en uso en tu máquina, el Companion revisa quién
lo tiene. El código que identifica al dueño y lo detiene existe hoy
(`internal/hostport`). El diálogo que te pregunta qué hacer es **planificado (F5)**.

| Quién tiene el puerto | Lo que ofrece el diálogo |
| --- | --- |
| Un proceso tuyo (node, vite, python) | Usar otro puerto, o detener el proceso: SIGTERM y, tras 5 segundos, SIGKILL (`taskkill` en Windows). |
| Un servicio que se reinicia solo (`brew services`, LaunchAgent, systemd user) | Usar otro puerto, o detener el servicio con el comando correcto (por ejemplo `brew services stop php`), mostrado antes de ejecutarse. |
| Un contenedor de Docker | Usar otro puerto, o `docker stop <contenedor>`. Nunca el daemon de Docker. |
| Otro usuario o el sistema | Usar otro puerto. Detener queda desactivado y explicado. El panel nunca pide contraseña de administrador. |

Después de una detención, el Companion vuelve a comprobar el puerto. Si sigue
ocupado, usa el puerto alternativo y lo registra en la actividad.

## Modo standalone

**Planificado.** `agentport open` abrirá el panel sin ningún perfil, con una pantalla
para conectarse a un servidor. Sugerirá conexiones encontradas en tu máquina, sin
enviar nada a ningún lugar:

- los hosts de `~/.ssh/config` y sus archivos `Include` (las entradas con comodín,
  como `Host *.corp`, se omiten), con usuario, puerto, clave y host de salto
  resueltos con `ssh -G <alias>`;
- la configuración de Remote-SSH de VS Code;
- las sesiones `ssh` abiertas ahora, marcadas como "conectado ahora";
- en Windows, las sesiones guardadas de PuTTY.

El historial de la terminal (`~/.zsh_history`, `~/.bash_history`) solo se usa si
marcas una opción para esa búsqueda. `known_hosts` no es una fuente, porque los
nombres suelen estar con hash. Cada sugerencia tiene **Probar** (`ssh -o
BatchMode=yes`, para ver si la clave autentica, si pide contraseña y si agentport ya
está instalado) y **Configurar** (el mismo flujo de `setup`). También puedes escribir
`usuario@ip`.

## Idiomas

agentport tiene cuatro idiomas: inglés (predeterminado), português (Brasil), español
y 繁體中文 (chino tradicional, como se usa en Taiwán).

La línea de comandos, `help`, los instaladores y el Companion eligen el idioma según
el entorno, en este orden:

1. `AGENTPORT_LANG`
2. `LC_ALL`
3. `LC_MESSAGES`
4. `LANG`

Decide la primera variable definida, como en la biblioteca C. `pt`, `pt_BR`, `pt_PT`
y valores parecidos dan português (Brasil); `es` y `es_*` dan español; `zh_TW`,
`zh_HK`, `zh_MO` y `zh-Hant` dan chino tradicional. `C`, `POSIX` y cualquier otro
valor, incluidos `zh_CN` y `zh-Hans`, dan inglés. Así, `LANG=pt_BR.UTF-8 agentport
help` imprime en português, y `AGENTPORT_LANG=en` fuerza el inglés.

La página del Companion usa, en este orden: `?lang=pt-BR` en la dirección, el idioma
preferido del navegador, el idioma que detectó el proceso del Companion como arriba y,
por último, el inglés.

**Hoy** solo português (Brasil) tiene catálogo. El español y el chino tradicional se
eligen cuando el entorno lo pide, pero sus mensajes siguen en inglés hasta que se
escriban sus catálogos (planificado). Los mensajes de error técnicos se quedan en
inglés a propósito, para poder buscarlos. Los nombres de comandos, las rutas y los
nombres de procesos no se traducen.

**Planificado:** el mismo orden en el panel (F5), y los catálogos de `es` y `zh-Hant`.

## Seguridad

- El puente solo escucha en `127.0.0.1`. El servidor lo alcanza únicamente por el
  túnel SSH que abrió el Companion.
- El servidor forma parte de tu base de confianza. Un agente en el servidor solo
  alcanza el puente con el token de sesión que el Companion registra para él.
- El token de emparejamiento está guardado en el archivo de tu perfil. No pongas
  perfiles ni tokens en archivos que vayan al repositorio de un proyecto.
- La página del Companion está en `127.0.0.1` y necesita un token que va en su propia
  URL. No compartas esa URL: es una vista de tu Companion.
- Los releases se generan con un único workflow, con checksum SHA-256, firma cosign,
  atestado de build y un análisis `govulncheck` de los binarios. Consulta
  [Verificar un release](#verificar-un-release).

**Planificado (F4 y F5)**, para las herramientas de puertos y el panel:

- Los forwards solo hacen bind en `127.0.0.1` de tu máquina. No hay opción para
  `0.0.0.0` en la versión 1.
- El destino de un forward es siempre `127.0.0.1:<puerto>` en el servidor, así que el
  túnel no puede usarse para alcanzar otros hosts de la red del servidor.
- Apruebas cada puerto que el agente pide, salvo que elijas "siempre" para él.
- Puertos bloqueados (22, 2375, 2376) y un límite de forwards simultáneos por perfil.
- Detener un proceso en tu máquina solo es posible desde el panel, con confirmación,
  y nunca con privilegios de administrador. No es una herramienta MCP.

## Actualización

`agentport upgrade` descarga el release estable más reciente para esta máquina,
comprueba su SHA-256 y su atestado, y reemplaza este ejecutable. Después actualiza
todos los servidores guardados, en el orden de abajo.

1. Esta máquina descarga y verifica el binario (el paso más lento; la salida lo
   indica).
2. Se actualiza cada servidor. Un fallo en uno no detiene a los demás, y el resumen
   nombra cada perfil que falló.
3. Solo entonces se detienen los Companions en ejecución, se reemplaza el ejecutable
   local y esos Companions se vuelven a iniciar. El reemplazo local va al final
   porque es el único paso difícil de deshacer.

Si esta máquina ya tiene el release más reciente, aquí no se descarga ni reemplaza
nada; solo se actualizan los servidores que están atrasados.

**Con Homebrew**, el binario pertenece al keg de Homebrew, así que `agentport upgrade`
no lo reemplaza. Imprime `agentport was installed with Homebrew. Run: brew upgrade
agentport` y después actualiza todos los servidores guardados a la versión de este
binario, igual que en cualquier instalación. Para llevar esta máquina a un release
más nuevo, ejecuta `brew upgrade agentport`; el siguiente `agentport upgrade` lleva
los servidores a esa versión.

Define `AGENTPORT_SKIP_ATTESTATION=1` solo si aceptas la comprobación de SHA-256 por
sí sola.

El Companion busca un release estable más nuevo al iniciar y, como máximo, una vez
cada 24 horas, compartiendo esa comprobación entre perfiles. Un release más nuevo se
muestra en la página del Companion y en `agentport_status`.

## Desinstalación

1. **Detén el inicio automático y el Companion** de cada perfil:

   ```bash
   agentport companion autostart disable m2
   agentport companion stop m2
   ```

2. **Quita la entrada de agentport de cada harness** en el servidor:

   ```bash
   agentport uninstall m2 --agent claude
   ```

   Repite para cada harness que configuraste. Esto quita solo la entrada o la
   extensión de agentport. No quita el agente en sí.

3. **Quita el binario** del servidor, si ningún otro perfil lo usa:

   ```bash
   ssh bastion-m2 'rm -f ~/.local/bin/agentport'
   ```

4. **Quita los archivos locales**: el perfil en
   `<directorio de configuración del usuario>/agentport/profiles/m2.json`, la clave
   dedicada en `<directorio de configuración del usuario>/agentport/keys/` (si se creó
   una) y los logs en `<directorio de caché del usuario>/agentport/logs/`.

5. **Quita el binario** de tu máquina:
   - Homebrew: `brew uninstall agentport`.
   - Script de instalación o código fuente: borra el archivo `agentport` de su
     directorio (por defecto `~/.local/bin` en macOS y Linux, y
     `%LOCALAPPDATA%\Programs\agentport\bin` en Windows).

**Planificado:** `uninstall` también quitará el bloque de `~/.ssh/config` que añade
`setup`, y un comando borrará un perfil.

## Solución de problemas

**El Companion no está corriendo.** Inícialo con `agentport companion start m2`, o
activa el [inicio automático](#inicio-automático). `agentport companion status m2`
indica si está corriendo y por qué el túnel está caído.

**El túnel se cae todo el tiempo.** Consulta la ruta del log con `agentport logs m2`,
o expórtalo con `agentport logs m2 --export ./agentport-m2.log`. Para registrar los
eventos del túnel mientras reproduces el problema:

```bash
agentport companion stop m2
agentport companion start m2 --verbose
```

El modo verbose registra los eventos y errores del túnel. Nunca registra tokens ni
contenido.

**`setup` sigue pidiendo la contraseña.** El servidor no acepta inicio de sesión con
clave pública para tu usuario, o su `~/.ssh/authorized_keys` no se puede escribir.
Primero comprueba que el inicio de sesión con clave funciona con el `ssh` normal;
`setup` solo puede añadir la clave dedicada cuando la contraseña funciona una vez.

**"could not verify the build attestation".** No se pudo contactar la API de
atestados, o el release no tiene atestado. Revisa tu red o instala `gh` y vuelve a
intentarlo. Define `AGENTPORT_SKIP_ATTESTATION=1` solo si aceptas la comprobación de
SHA-256 por sí sola.

**"SSH destination must not start with a dash".** El destino parece una opción.
Indica un host, un alias o `usuario@host`.

**`upgrade` dice que agentport se instaló con Homebrew.** Ejecuta `brew upgrade
agentport`, como indica el mensaje.

**`agentport doctor` dice que el puente no está disponible.** Inicia el Companion de
un perfil. `doctor` solo comprueba el puente local; `companion status` también
informa del túnel.

## Desarrollo

El build, las pruebas, el proceso de release y los catálogos de mensajes están en
[DEVELOPMENT.md](DEVELOPMENT.md). Las reglas para contribuir están en
[CONTRIBUTING.md](CONTRIBUTING.md), y la política de seguridad en
[SECURITY.md](SECURITY.md). La referencia de comandos es [COMMANDS.md](COMMANDS.md).

**Harness de extremo a extremo.** `test/e2e` levanta un `sshd` desechable en Docker y
comprueba contra él los forwards por un ControlMaster real, los conflictos de puertos y
el escáner de puertos dentro de Linux. Necesita Docker en ejecución y no toca tu
`~/.ssh`:

```bash
test/e2e/run.sh
go test -tags e2e -v -count=1 ./test/e2e/
```

Un `go test ./...` normal no ejecuta el harness.

**Planificado (F7):** el mismo harness ejecutará `setup` y las herramientas de puertos
de extremo a extremo, cuando se integren.

## Créditos

agentport nació como una bifurcación de [AgentClip](https://github.com/wendellrocha/agentclip),
de [Wendell Rocha](https://github.com/wendellrocha), distribuido bajo la licencia MIT.
Reutilizamos la base del Companion, el túnel SSH inverso, el servidor MCP por stdio,
el registro automático en los agentes y el pipeline de releases verificables.
Gracias al proyecto original. 💙

## Licencia

Distribuido bajo la [licencia MIT](LICENSE). La licencia mantiene el copyright del
proyecto original AgentClip, junto con el copyright de agentport.
