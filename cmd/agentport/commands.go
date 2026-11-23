package main

import (
	"strings"

	"github.com/rafaelb13/agentport/internal/i18n"
	"github.com/rafaelb13/agentport/internal/setup"
)

// command describes one command of the CLI. The table below is the single source
// of what exists: main dispatches from it and the help is written from it, so the
// two cannot drift apart.
type command struct {
	name    string
	aliases []string
	group   string
	// summary is one line for the list of commands.
	summary string
	// usage is the syntax, one line per form. It is not translated.
	usage []string
	// details explains what the command does and its options.
	details string
	run     func(arguments []string) error
}

const (
	groupSetup       = "setup"
	groupCompanion   = "companion"
	groupMaintenance = "maintenance"
	groupAdvanced    = "advanced"
)

// groupOrder is the order help lists the groups in.
var groupOrder = []string{groupSetup, groupCompanion, groupMaintenance, groupAdvanced}

func groupTitle(group string) string {
	switch group {
	case groupSetup:
		return i18n.T("Set up a server")
	case groupCompanion:
		return i18n.T("The local Companion")
	case groupMaintenance:
		return i18n.T("Maintenance")
	default:
		return i18n.T("Advanced and internal")
	}
}

// commandTable builds the commands in the language of the process. It is built
// when help or a dispatch needs it, after i18n.Init has run.
func commandTable() []command {
	return append([]command{
		{
			name: "setup", group: groupSetup, run: runSetup,
			summary: i18n.T("Install and configure a server in one step"),
			usage:   []string{strings.TrimPrefix(setup.Usage, "usage: ")},
			details: i18n.T(`The recommended way to set up a server. It installs the matching release on the
Linux or macOS server over SSH, creates or replaces the local profile, detects
the coding-agent harnesses that are already installed, registers agentport with
each of them and starts the local Companion.

  --profile NAME     local name of the pairing (default: derived from the destination)
  --agent NAME       limit the setup to one harness; "all" is the default
  --version vX.Y.Z   release to install on the server
  --remote-port N    loopback port on the server used by the reverse tunnel
  --skip-agent       do not register the MCP integration on the server
  --skip-install     do not install or update the binary on the server
  --no-start         finish without starting the Companion

If SSH still asks for a password, setup creates a private agentport key and
asks for the password once to authorize it. Running setup again for the same
profile issues a new pairing token and stops the previous Companion.`),
		},
		{
			name: "pair", group: groupSetup, run: runPair,
			summary: i18n.T("Create a local pairing and configure the harnesses on a server"),
			usage:   []string{"agentport pair <profile> <ssh-destination> [--agent NAME] [--remote-port N] [--skip-agent]"},
			details: i18n.T(`Creates only the local pairing and configures the harnesses on the server. Useful
for development, or when agentport was already installed on the server by hand.
Unlike setup, it does not install the remote binary and does not start the
Companion: start it with "agentport companion start <profile>".`),
		},
		{
			name: "connect", group: groupSetup, run: runConnect,
			summary: i18n.T("Register agentport again with the harnesses of a saved profile"),
			usage:   []string{"agentport connect <profile> [--agent NAME]"},
			details: i18n.T(`Reads an existing profile and registers agentport again with the harnesses on
the server. It does not restart the Companion or change the profile's token or
port.`),
		},
		{
			name: "uninstall", aliases: []string{"disconnect"}, group: groupSetup, run: runUninstall,
			summary: i18n.T("Remove agentport's MCP entry from one harness on a server"),
			usage:   []string{"agentport uninstall <profile> --agent NAME"},
			details: i18n.T(`Removes only agentport's MCP entry or extension from the chosen harness on the
server. It does not remove the harness, the remote binary, the local profile or
a running Companion. "disconnect" is an alias.`),
		},
		{
			name: "companion", group: groupCompanion, run: runCompanion,
			summary: i18n.T("Run and manage the local Companion"),
			usage: []string{
				"agentport companion <start|stop|status|open|view|run> <profile> [--verbose]",
				"agentport companion autostart <enable|disable|status> <profile>",
			},
			details: i18n.T(`The Companion keeps the local bridge running and holds the reverse SSH tunnel
to the server.

  start      start the Companion in the background
  stop       stop it cleanly and close the tunnel
  status     print its state as JSON: tunnel and update notice
  open       open its local web page in your browser ("view" is the same)
  run        run in the foreground, for diagnosis
  autostart  start it when you log in: "enable", "disable" or "status"

Add --verbose to start or run to log tunnel events (never tokens). The autostart uses a LaunchAgent on macOS, a systemd user unit on
Linux and a Task Scheduler task on Windows.`),
		},
		{
			name: "logs", group: groupCompanion, run: runLogs,
			summary: i18n.T("Show the path of a Companion's log, or export it"),
			usage:   []string{"agentport logs <profile> [--export PATH]"},
			details: i18n.T(`Prints the path of the Companion's private log. With --export it copies the
whole log to PATH instead. To capture more detail while reproducing a problem,
restart the profile with "agentport companion start <profile> --verbose".`),
		},
		{
			name: "upgrade", group: groupMaintenance, run: runUpgrade,
			summary: i18n.T("Update this machine and every saved server to the latest release"),
			usage:   []string{"agentport upgrade"},
			details: i18n.T(`Downloads the latest stable release, checks its SHA-256 and its build
attestation, replaces this executable and updates every saved server: this
machine downloads and verifies the binary for the server's platform and sends
it over SSH. A server that is already up to date is left alone, and only the
Companions that were running are restarted.

Set AGENTPORT_SKIP_ATTESTATION=1 to skip the attestation check.

If agentport was installed with Homebrew, this command does not replace the
executable: it prints "brew upgrade agentport" and exits. Homebrew owns that
install.`),
		},
		{
			name: "doctor", group: groupMaintenance, run: func([]string) error { return runDoctor() },
			summary: i18n.T("Check that a local bridge is healthy"),
			usage:   []string{"agentport doctor"},
			details: i18n.T(`Checks for a healthy local bridge and prints its address and process ID. For the
Companion workflow prefer "agentport companion status <profile>", which also
reports the tunnel.`),
		},
		{
			name: "help", aliases: []string{"-h", "--help"}, group: groupMaintenance, run: runHelp,
			summary: i18n.T("Show the list of commands, or the help of one command"),
			usage:   []string{"agentport help [command]", "agentport <command> --help"},
			details: i18n.T(`Without arguments, lists every command with a one-line summary. With a command,
explains it: its syntax, what it does and its options. Asking for help never
runs the command.`),
		},
		{
			name: "version", group: groupMaintenance, run: func([]string) error { printVersion(); return nil },
			aliases: []string{"--version", "-v"},
			summary: i18n.T("Print the version"),
			usage:   []string{"agentport version"},
			details: i18n.T(`Prints the version of this binary. The installers use it to decide whether an
update is needed.`),
		},
		{
			name: "mcp", group: groupAdvanced, run: func([]string) error { return runMCP() },
			summary: i18n.T("Serve the MCP tools over stdio (run by the harness)"),
			usage:   []string{"agentport mcp"},
			details: i18n.T(`Runs the MCP server on standard input and output. A harness starts it; you do
not run it yourself.`),
		},
		{
			name: "harness", group: groupAdvanced, run: runHarness,
			summary: i18n.T("Install or remove the integration of a harness (used by setup)"),
			usage:   []string{"agentport harness <install|remove> <agy|opencode|pi> --name NAME [--port PORT --token TOKEN]"},
			details: i18n.T(`Writes or removes agentport's integration for the harnesses that have no MCP
command of their own (AGY, OpenCode and Pi). Setup, pair and connect call it on
the server; you rarely need to.`),
		},
		{
			name: "bridge", group: groupAdvanced, run: runBridgeCommand,
			summary: i18n.T("Run the local bridge on its own, or stop it (internal)"),
			usage:   []string{"agentport bridge", "agentport bridge stop [--force]"},
			details: i18n.T(`Runs the local bridge without a Companion. It is used internally; the Companion
starts the bridge for you and stops it when no Companion uses it any more.
"bridge stop" stops a bridge that is still running. It refuses while a Companion
uses the bridge, unless --force is given.`),
		},
	}, portCommands()...)
}

// portCommands are the commands for the ports of a server: list them, decide
// the agent's requests and close forwards. They talk to the local Companion.
func portCommands() []command {
	return []command{
		{
			name: "ports", group: groupCompanion, run: runPorts,
			summary: i18n.T("List the listening ports of a server"),
			usage:   []string{"agentport ports [profile] [--json] [--all]"},
			details: i18n.T(`Lists the TCP ports the server listens on, with the process, user and whether
the port is already forwarded to this machine. Without a profile it scans the
machine it runs on; with a profile it runs the scan on that server over SSH.

  --json   print JSON
  --all    include system services`),
		},
		{
			name: "requests", group: groupCompanion, run: runRequests,
			summary: i18n.T("List the port requests waiting for your decision"),
			usage:   []string{"agentport requests [--profile NAME]"},
			details: i18n.T(`Lists the requests an agent sent with ports_forward that you have not decided yet.`),
		},
		{
			name: "approve", group: groupCompanion, run: runApprove,
			summary: i18n.T("Approve a port request, all of it or one port"),
			usage:   []string{"agentport approve <request-id> (--all | --port N) [--local N] [--duration session|1h|pin] [--profile NAME]"},
			details: i18n.T(`Approves a request. --all approves every port; --port N approves only that port and
skips the others. --local sets the local port of that one port. If the local port is
busy, another free one is used and the result says so. The duration is session (the
default), 1h, or pin (kept until you close it).`),
		},
		{
			name: "deny", group: groupCompanion, run: runDeny,
			summary: i18n.T("Refuse a port request"),
			usage:   []string{"agentport deny <request-id> [--profile NAME]"},
			details: i18n.T(`Refuses every port of a request. The agent is told the ports were denied.`),
		},
		{
			name: "forwards", group: groupCompanion, run: runForwards,
			summary: i18n.T("List the ports forwarded to this machine"),
			usage:   []string{"agentport forwards [--profile NAME] [--json]"},
			details: i18n.T(`Lists the forwarded ports of a server, with the local port, state, origin and owners.`),
		},
		{
			name: "unforward", group: groupCompanion, run: runUnforward,
			summary: i18n.T("Close forwarded ports, whoever opened them"),
			usage:   []string{"agentport unforward <remote-port...> [--profile NAME]", "agentport unforward --all [--profile NAME]"},
			details: i18n.T(`Closes forwarded ports from this machine, whatever their owners are. Agents only
release their own use of a port (ports_unforward); this command is the user's way to close one.`),
		},
	}
}
