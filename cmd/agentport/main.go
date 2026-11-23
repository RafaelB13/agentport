package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rafaelb13/agentport/internal/agents"
	"github.com/rafaelb13/agentport/internal/buildinfo"
	"github.com/rafaelb13/agentport/internal/companion"
	"github.com/rafaelb13/agentport/internal/control"
	"github.com/rafaelb13/agentport/internal/daemon"
	"github.com/rafaelb13/agentport/internal/harness"
	"github.com/rafaelb13/agentport/internal/i18n"
	"github.com/rafaelb13/agentport/internal/mcpserver"
	"github.com/rafaelb13/agentport/internal/paths"
	"github.com/rafaelb13/agentport/internal/release"
	"github.com/rafaelb13/agentport/internal/remote"
	"github.com/rafaelb13/agentport/internal/selfupgrade"
	"github.com/rafaelb13/agentport/internal/setup"
	"github.com/rafaelb13/agentport/internal/upgrader"
)

const (
	startupTimeout = 3 * time.Second
	remotePortMin  = 32000
	remotePortMax  = 44999
)

type bridgeBootstrap struct {
	ControlToken string `json:"control_token"`
}

func main() {
	i18n.Init()
	arguments := os.Args[1:]
	table := commandTable()
	if len(arguments) == 0 {
		printHelp(os.Stdout, table)
		return
	}
	selected, found := findCommand(table, arguments[0])
	if !found {
		fmt.Fprintln(os.Stderr, unknownCommand(arguments[0]))
		os.Exit(2)
	}
	// Asking a command for help never runs it.
	if selected.name != "help" && wantsHelp(arguments[1:]) {
		printCommandHelp(os.Stdout, selected)
		return
	}
	if err := selected.run(arguments[1:]); err != nil {
		var mistake usageError
		if errors.As(err, &mistake) {
			fmt.Fprintln(os.Stderr, mistake)
			os.Exit(2)
		}
		log.Fatal(err)
	}
}

func acquireBridgeLock() (func(), error) {
	dir, err := paths.Dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create agentport cache directory: %w", err)
	}
	path := filepath.Join(dir, "bridge.lock")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			_ = os.Remove(path)
			file, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		}
	}
	if err != nil {
		if os.IsExist(err) {
			return nil, errors.New("another agentport bridge operation is already in progress")
		}
		return nil, fmt.Errorf("lock agentport arm operation: %w", err)
	}
	return func() {
		_ = file.Close()
		_ = os.Remove(path)
	}, nil
}

func runMCP() error {
	port, err := strconv.Atoi(os.Getenv("AGENTPORT_BRIDGE_PORT"))
	if err != nil {
		return fmt.Errorf("read AGENTPORT_BRIDGE_PORT: %w", err)
	}
	token := os.Getenv("AGENTPORT_SESSION_TOKEN")
	provider, err := mcpserver.NewHTTPProvider(port, token)
	if err != nil {
		return err
	}
	client, err := mcpserver.NewBridgeClient(port, token, mcpserver.AgentFromEnv())
	if err != nil {
		return err
	}
	// Closing the client when stdio ends unregisters this agent session.
	defer client.Close()
	tools := &mcpserver.PortTools{Bridge: client, ExcludePorts: []int{port}}
	return mcpserver.New(provider, tools).Run(context.Background(), &mcp.StdioTransport{})
}

func runHarness(arguments []string) error {
	if len(arguments) < 2 {
		return errors.New("usage: agentport harness <install|remove> <agy|opencode|pi> --name NAME [--port PORT --token TOKEN]")
	}
	settings := flag.NewFlagSet("harness", flag.ContinueOnError)
	settings.SetOutput(io.Discard)
	name := settings.String("name", "", "agentport MCP entry name")
	port := settings.Int("port", 0, "bridge loopback port")
	token := settings.String("token", "", "bridge session token")
	if err := settings.Parse(arguments[2:]); err != nil {
		return fmt.Errorf("parse harness options: %w", err)
	}
	if *name == "" || settings.NArg() != 0 {
		return errors.New("usage: agentport harness <install|remove> <agy|opencode|pi> --name NAME [--port PORT --token TOKEN]")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find home directory: %w", err)
	}
	switch arguments[0] {
	case "install":
		if *port < 1 || *port > 65535 || strings.TrimSpace(*token) == "" {
			return errors.New("harness install requires --port and --token")
		}
		return harness.Install(home, arguments[1], *name, *port, *token)
	case "remove":
		return harness.Remove(home, arguments[1], *name)
	default:
		return fmt.Errorf("unsupported harness action %q; expected install or remove", arguments[0])
	}
}

func runPair(arguments []string) error {
	if len(arguments) < 2 {
		return errors.New("usage: agentport pair <profile> <ssh-destination> [--agent all|codex|claude|gemini|agy|opencode|pi] [--remote-port 39123] [--skip-agent]")
	}
	settings := flag.NewFlagSet("pair", flag.ContinueOnError)
	settings.SetOutput(io.Discard)
	remotePort := settings.Int("remote-port", 39123, "remote loopback port")
	agent := settings.String("agent", "all", "remote agent: all, codex, claude, gemini, agy, opencode, or pi")
	skipAgent := settings.Bool("skip-agent", false, "do not configure an agent on the server")
	skipCodex := settings.Bool("skip-codex", false, "deprecated alias for --skip-agent")
	if err := settings.Parse(arguments[2:]); err != nil {
		return fmt.Errorf("parse pair options: %w", err)
	}
	profile, err := pairProfile(arguments[0], arguments[1], *remotePort, *agent, *skipAgent || *skipCodex)
	if err != nil {
		return err
	}
	fmt.Println(i18n.T("Paired profile %q. Start it with: agentport companion start %s", profile.Name, profile.Name))
	return nil
}

func runSetup(arguments []string) error {
	options, err := setup.ParseArgs(arguments)
	if err != nil {
		return err
	}
	return setup.Runner{
		Stdout:         os.Stdout,
		LoadProfile:    companion.LoadProfile,
		EnsureIdentity: remote.EnsureSetupSSHIdentity,
		ReleaseTag:     setup.ReleaseTag,
		Install: func(destination, identityFile, tag string) error {
			_, err := remote.InstallVerified(context.Background(), destination, identityFile, tag, os.Stderr)
			return err
		},
		CompanionRunning: func(name string) bool {
			state, err := companion.LoadRuntime(name)
			return err == nil && companion.RuntimeHealthy(state)
		},
		StopCompanion:  stopCompanion,
		Pair:           pairProfileWithIdentity,
		StartCompanion: func(name string) error { return startCompanion(name, false) },
	}.Run(options)
}

func runConnect(arguments []string) error {
	if len(arguments) < 1 {
		return errors.New("usage: agentport connect <profile> [--agent all|codex|claude|gemini|agy|opencode|pi]")
	}
	settings := flag.NewFlagSet("connect", flag.ContinueOnError)
	settings.SetOutput(io.Discard)
	agent := settings.String("agent", "all", "remote agent: all, codex, claude, gemini, agy, opencode, or pi")
	if err := settings.Parse(arguments[1:]); err != nil {
		return fmt.Errorf("parse connect options: %w", err)
	}
	if settings.NArg() != 0 {
		return errors.New("usage: agentport connect <profile> [--agent all|codex|claude|gemini|agy|opencode|pi]")
	}
	profile, err := companion.LoadProfile(arguments[0])
	if err != nil {
		return err
	}
	adapters, err := agents.Configure(profile, *agent)
	if err != nil {
		return err
	}
	fmt.Println(i18n.T("Connected %s to profile %q.", agents.Display(adapters), profile.Name))
	return nil
}

func runUninstall(arguments []string) error {
	if len(arguments) < 1 {
		return errors.New("usage: agentport uninstall <profile> --agent <codex|claude|gemini|agy|opencode|pi>")
	}
	settings := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	settings.SetOutput(io.Discard)
	agent := settings.String("agent", "", "remote agent: codex, claude, gemini, agy, opencode, or pi")
	if err := settings.Parse(arguments[1:]); err != nil {
		return fmt.Errorf("parse uninstall options: %w", err)
	}
	if *agent == "" || settings.NArg() != 0 {
		return errors.New("usage: agentport uninstall <profile> --agent <codex|claude|gemini|agy|opencode|pi>")
	}
	profile, err := companion.LoadProfile(arguments[0])
	if err != nil {
		return err
	}
	adapter, err := agents.Resolve(*agent)
	if err != nil {
		return err
	}
	if err := remote.LoginForProfile(profile, adapter.RemoveArguments("agentport-"+profile.Name)...).Run(); err != nil {
		return fmt.Errorf("remove agentport MCP from %s on %s: %w", adapter.DisplayName, profile.Destination, err)
	}
	fmt.Println(i18n.T("Removed the agentport MCP entry from %s for profile %q. The harness remains installed.", adapter.DisplayName, profile.Name))
	return nil
}

func pairProfile(name, destination string, remotePort int, agent string, skipAgent bool) (companion.Profile, error) {
	return pairProfileWithIdentity(name, destination, remotePort, agent, skipAgent, "")
}

func pairProfileWithIdentity(name, destination string, remotePort int, agent string, skipAgent bool, identityFile string) (companion.Profile, error) {
	token, err := randomToken(32)
	if err != nil {
		return companion.Profile{}, err
	}
	profile := companion.Profile{
		Name: name, Destination: destination, RemotePort: remotePort,
		Token: token, SSHIdentityFile: identityFile, CreatedAt: time.Now().UTC(),
	}
	if err := profile.Validate(); err != nil {
		return companion.Profile{}, err
	}
	if !skipAgent {
		adapters, err := agents.Configure(profile, agent)
		if err != nil {
			return companion.Profile{}, err
		}
		fmt.Println(i18n.T("Configured %s on %s.", agents.Display(adapters), profile.Destination))
	}
	if err := companion.SaveProfile(profile); err != nil {
		return companion.Profile{}, err
	}
	return profile, nil
}

func runCompanion(arguments []string) error {
	if len(arguments) > 0 && arguments[0] == "autostart" {
		return runAutostart(arguments[1:], os.Stdout)
	}
	if len(arguments) == 2 && arguments[0] == "ensure" {
		return ensureCompanion(arguments[1], os.Stdout, startCompanion)
	}
	if len(arguments) < 2 || len(arguments) > 4 {
		return errors.New("usage: agentport companion <start|stop|status|open|view|run|ensure> <profile> [--verbose] | agentport companion autostart <enable|disable|status> <profile>")
	}
	verbose := false
	if len(arguments) == 3 && arguments[2] == "--verbose" && (arguments[0] == "start" || arguments[0] == "run" || arguments[0] == "serve") {
		verbose = true
	}
	if len(arguments) == 4 {
		return errors.New("unknown Companion option; expected --verbose")
	}
	switch arguments[0] {
	case "start":
		return startCompanion(arguments[1], verbose)
	case "stop":
		return stopCompanion(arguments[1])
	case "status":
		return printCompanionStatus(arguments[1])
	case "open", "view":
		return openCompanionView(arguments[1])
	case "run", "serve":
		if verbose {
			os.Setenv("AGENTPORT_LOG_LEVEL", "debug")
		}
		return runCompanionService(arguments[1], arguments[0] == "run")
	default:
		return errors.New("usage: agentport companion <start|stop|status|open|view|run|ensure> <profile>")
	}
}

func startCompanion(name string, verbose bool) error {
	if _, err := companion.LoadProfile(name); err != nil {
		return err
	}
	if state, err := companion.LoadRuntime(name); err == nil && companion.RuntimeHealthy(state) {
		return fmt.Errorf("Companion %q is already running; use `agentport companion open %s`", name, name)
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate agentport executable: %w", err)
	}
	command := exec.Command(executable, "companion", "serve", name)
	if verbose {
		command.Env = append(os.Environ(), "AGENTPORT_LOG_LEVEL=debug")
	}
	if err := startDetached(command); err != nil {
		return fmt.Errorf("start Companion: %w", err)
	}
	pid := command.Process.Pid
	deadline := time.Now().Add(startupTimeout)
	for time.Now().Before(deadline) {
		state, err := companion.LoadRuntime(name)
		if err == nil && state.PID == pid && companion.RuntimeHealthy(state) {
			_ = command.Process.Release()
			fmt.Println(i18n.T("Companion %q started. Open: agentport companion open %s", name, name))
			fmt.Println(i18n.T("Logs: %s", companionLogPath(name)))
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = command.Process.Kill()
	_, _ = command.Process.Wait()
	return errors.New("Companion did not start within the expected time")
}

func companionLogPath(name string) string {
	path, err := paths.Dir("logs", name+".log")
	if err != nil {
		return filepath.Join("~", ".cache", "agentport", "logs", name+".log")
	}
	return path
}

func runLogs(arguments []string) error {
	if len(arguments) < 1 {
		return errors.New("usage: agentport logs <profile> [--export PATH]")
	}
	settings := flag.NewFlagSet("logs", flag.ContinueOnError)
	settings.SetOutput(io.Discard)
	exportPath := settings.String("export", "", "copy the complete log to this file")
	if err := settings.Parse(arguments[1:]); err != nil {
		return fmt.Errorf("parse logs options: %w", err)
	}
	if settings.NArg() != 0 {
		return errors.New("usage: agentport logs <profile> [--export PATH]")
	}
	if err := companion.RequireLog(arguments[0]); err != nil {
		return err
	}
	logger, err := companion.NewLogger(arguments[0])
	if err != nil {
		return err
	}
	defer logger.Close()
	if *exportPath != "" {
		if err := logger.Export(*exportPath); err != nil {
			return err
		}
		fmt.Println(i18n.T("Log exported to %s", *exportPath))
		return nil
	}
	fmt.Println(logger.Path())
	return nil
}

// runUpgrade updates every configured server, then atomically replaces the
// executable that launched this command. A remote failure is reported after
// every profile has been attempted and does not discard a verified local
// upgrade.
func runUpgrade(arguments []string) error {
	if len(arguments) != 0 {
		return errors.New("usage: agentport upgrade")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	runner := upgradeRunner()
	// Homebrew owns the files of its keg: this process must not replace them. The
	// saved servers are still updated to this binary's version. If the check
	// itself fails, the normal upgrade runs.
	if executable, err := os.Executable(); err == nil {
		if brew, err := selfupgrade.InstalledWithHomebrew(executable, os.Getenv); err == nil && brew {
			return runner.RunHomebrew(ctx)
		}
	}
	return runner.Run(ctx)
}

// upgradeRunner wires the upgrade to the real release, SSH and process steps.
func upgradeRunner() selfupgrade.Runner {
	checker := release.NewChecker()
	return selfupgrade.Runner{
		GOOS:           runtime.GOOS,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
		CurrentVersion: buildinfo.Version,
		LatestTag:      checker.FetchLatest,
		Executable:     os.Executable,
		Prepare: func(ctx context.Context, tag, executable string) (upgrader.StagedBinary, error) {
			return upgrader.Prepare(ctx, upgrader.Options{Version: tag, Executable: executable, SkipAttestation: os.Getenv(upgrader.SkipAttestationEnv) == "1"})
		},
		Profiles:         companion.ListProfiles,
		ActiveCompanions: activeCompanions,
		InstallRemote: func(profile companion.Profile, tag string) (bool, error) {
			return remote.InstallVerified(context.Background(), profile.Destination, profile.SSHIdentityFile, tag, os.Stderr)
		},
		StopCompanions:           stopCompanions,
		RestartCompanions:        restartCompanions,
		Replace:                  os.Rename,
		LaunchWindowsReplacement: launchWindowsReplacement,
	}
}

func activeCompanions(profiles []companion.Profile) ([]string, error) {
	active := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		state, err := companion.LoadRuntime(profile.Name)
		if err == nil && companion.RuntimeHealthy(state) {
			active = append(active, profile.Name)
		}
	}
	return active, nil
}

func stopCompanions(names []string) error {
	for _, name := range names {
		state, err := companion.LoadRuntime(name)
		if err != nil || !companion.RuntimeHealthy(state) {
			continue
		}
		if err := companion.StopRuntime(state); err != nil {
			return fmt.Errorf("stop active Companion %q: %w", name, err)
		}
	}
	deadline := time.Now().Add(startupTimeout)
	for time.Now().Before(deadline) {
		stopped := true
		for _, name := range names {
			state, err := companion.LoadRuntime(name)
			if err == nil && companion.RuntimeHealthy(state) {
				stopped = false
				break
			}
		}
		if stopped {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("timed out stopping active Companions")
}

func restartCompanions(names []string) error {
	var failures []string
	for _, name := range names {
		if err := startCompanion(name, false); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(failures) != 0 {
		return fmt.Errorf("restart Companion(s): %s", strings.Join(failures, "; "))
	}
	return nil
}

// launchWindowsReplacement delegates the locked .exe replacement to a fresh
// PowerShell process. It waits for this upgrade process to exit, swaps the
// verified staged executable, starts only the previously active Companions,
// then removes its own script.
func launchWindowsReplacement(staged upgrader.StagedBinary, companions []string) error {
	script, err := os.CreateTemp(filepath.Dir(staged.Target), ".agentport-upgrade-*.ps1")
	if err != nil {
		return fmt.Errorf("create Windows upgrade helper: %w", err)
	}
	scriptPath := script.Name()
	profiles, err := json.Marshal(companions)
	if err == nil {
		_, err = script.WriteString(windowsReplacementScript())
	}
	if closeErr := script.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(scriptPath)
		return fmt.Errorf("write Windows upgrade helper: %w", err)
	}
	command := windowsReplacementCommand(scriptPath, staged, string(profiles), os.Getpid())
	if err := command.Start(); err != nil {
		_ = os.Remove(scriptPath)
		return fmt.Errorf("start Windows upgrade helper: %w", err)
	}
	return command.Process.Release()
}

func windowsReplacementCommand(scriptPath string, staged upgrader.StagedBinary, profiles string, processID int) *exec.Cmd {
	return exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath, "-ProcessId", strconv.Itoa(processID), "-Source", staged.Path, "-Target", staged.Target, "-Profiles", profiles)
}

func windowsReplacementScript() string {
	return "param([int]$ProcessId,[string]$Source,[string]$Target,[string]$Profiles)\n" +
		"$ErrorActionPreference = 'Stop'\n" +
		"Wait-Process -Id $ProcessId\n" +
		"Move-Item -Force -LiteralPath $Source -Destination $Target\n" +
		"$profiles = ConvertFrom-Json $Profiles\n" +
		"foreach ($profile in @($profiles)) { Start-Process -WindowStyle Hidden -FilePath $Target -ArgumentList @('companion','serve',$profile) }\n" +
		"Remove-Item -Force -LiteralPath $PSCommandPath\n"
}

func stopCompanion(name string) error {
	state, err := companion.LoadRuntime(name)
	if err != nil || !companion.RuntimeHealthy(state) {
		return fmt.Errorf("Companion %q is not running", name)
	}
	if err := companion.StopRuntime(state); err != nil {
		return fmt.Errorf("stop Companion %q: %w", name, err)
	}
	fmt.Println(i18n.T("Companion %q is stopping.", name))
	return nil
}

func printCompanionStatus(name string) error {
	state, err := companion.LoadRuntime(name)
	if err != nil || !companion.RuntimeHealthy(state) {
		return fmt.Errorf("Companion %q is not running", name)
	}
	payload, err := companion.FetchRuntimeStatus(state)
	if err != nil {
		return fmt.Errorf("get Companion status: %w", err)
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, payload, "", "  "); err != nil {
		return err
	}
	fmt.Println(formatted.String())
	return nil
}

func openCompanionView(name string) error {
	state, err := companion.LoadRuntime(name)
	if err != nil || !companion.RuntimeHealthy(state) {
		return fmt.Errorf("Companion %q is not running", name)
	}
	if err := companion.OpenURL(companion.ViewURL(state)); err != nil {
		return fmt.Errorf("open Companion view: %w", err)
	}
	return nil
}

// ensureCompanion starts the Companion of profile in the background when it is
// not running. It is a no-op otherwise, so a login item or a shell hook can call it.
func ensureCompanion(name string, stdout io.Writer, start func(name string, verbose bool) error) error {
	if _, err := companion.LoadProfile(name); err != nil {
		return err
	}
	if state, err := companion.LoadRuntime(name); err == nil && companion.RuntimeHealthy(state) {
		fmt.Fprintln(stdout, i18n.T("Companion %q is already running.", name))
		return nil
	}
	// start prints where the Companion runs and where its log is.
	return start(name, false)
}

type companionDashboardStatus struct {
	Profile     string                 `json:"profile"`
	Destination string                 `json:"destination"`
	Version     string                 `json:"version"`
	StartedAt   time.Time              `json:"started_at"`
	Tunnel      companion.TunnelStatus `json:"tunnel"`
	Release     release.Status         `json:"release"`
}

func runCompanionService(name string, announce bool) error {
	profile, err := companion.LoadProfile(name)
	if err != nil {
		return err
	}
	// A login service must not start a second Companion for a profile that is
	// already running: they would fight over the state file and the tunnel. The
	// check and the claim (the runtime state written by StartControl) happen under
	// a start lock, so two starts at the same moment cannot both pass the check.
	releaseStart, err := acquireProfileStartLock(name, profileStartWait, profileStartStale)
	if err != nil {
		return err
	}
	startReleased := false
	defer func() {
		if !startReleased {
			releaseStart()
		}
	}()
	if state, err := companion.LoadRuntime(name); err == nil && companion.RuntimeHealthy(state) {
		if announce {
			return fmt.Errorf("Companion %q is already running; use `agentport companion open %s`", name, name)
		}
		return nil
	}
	logger, err := companion.NewLogger(name)
	if err != nil {
		return err
	}
	defer logger.Close()
	logger.Info("Companion starting: profile=%s destination=%s", profile.Name, profile.Destination)
	unlock, err := acquireBridgeLock()
	if err != nil {
		logger.Error("bridge unavailable: %v", err)
		return err
	}
	state, started, err := ensureBridge()
	unlock()
	if err != nil {
		return err
	}
	if started {
		defer func() { _ = control.Shutdown(state) }()
	}
	payload, err := json.Marshal(struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}{ID: "companion:" + profile.Name, Token: profile.Token})
	if err != nil {
		return err
	}
	if err := control.Post(state, "/v1/control/persistent-session", payload, nil); err != nil {
		return fmt.Errorf("register companion session: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serviceStartedAt := time.Now().UTC()
	var releaseMu sync.RWMutex
	releaseState := release.Status{CurrentVersion: buildinfo.Version}
	if err := control.PublishReleaseStatus(state, releaseState); err != nil {
		logger.Error("publish initial release status: %v", err)
	}
	var tunnelMu sync.RWMutex
	tunnel := companion.TunnelStatus{UpdatedAt: time.Now().UTC()}
	stop := make(chan struct{}, 1)
	portErrors := func(err error) { logger.Error("ports: %v", err) }
	portManager, err := companion.NewPortManager(profile, portErrors)
	if err != nil {
		logger.Error("start port manager: %v", err)
		return fmt.Errorf("start port manager: %w", err)
	}
	controlServer, err := companion.StartControl(profile.Name, func() any {
		tunnelMu.RLock()
		currentTunnel := tunnel
		tunnelMu.RUnlock()
		return companionDashboardStatus{
			Profile: profile.Name, Destination: profile.Destination, Version: buildinfo.Version, StartedAt: serviceStartedAt,
			Tunnel: currentTunnel, Release: func() release.Status {
				releaseMu.RLock()
				defer releaseMu.RUnlock()
				return releaseState
			}(),
		}
	}, func() {
		select {
		case stop <- struct{}{}:
		default:
		}
	}, portManager.Handler)
	if err != nil {
		logger.Error("start Companion control server: %v", err)
		return fmt.Errorf("start Companion control server: %w", err)
	}
	// The bridge relays /v1/sessions and /v1/forwards to this Companion's port API.
	upstreamPayload, err := json.Marshal(struct {
		ID      string `json:"id"`
		Address string `json:"address"`
		Token   string `json:"token"`
	}{ID: "companion:" + profile.Name, Address: controlServer.State.Address, Token: controlServer.State.ControlToken})
	if err != nil {
		return err
	}
	if err := control.Post(state, "/v1/control/upstream", upstreamPayload, nil); err != nil {
		logger.Error("register port API with the bridge: %v", err)
	}
	// A new request opens the panel when no tab is connected, as the profile's settings say.
	autoOpen := &companion.AutoOpen{
		URL: func() string { return companion.ViewURL(controlServer.State) },
		Settings: func() companion.PanelSettings {
			settings, err := companion.LoadPanelSettings(profile.Name)
			if err != nil {
				portErrors(err)
				return companion.DefaultPanelSettings()
			}
			return settings
		},
		Clients: controlServer.PanelClients,
		Open:    companion.OpenURL,
		Notify:  companion.NotifyDesktop,
		OnError: portErrors,
	}
	portManager.Store.SetOnPending(autoOpen.OnPending)
	portManager.Start(ctx)
	// The runtime state is written: a later start sees this Companion as running.
	startReleased = true
	releaseStart()
	defer controlServer.Close()
	go watchReleaseStatus(ctx, state, logger, func(next release.Status) {
		releaseMu.Lock()
		releaseState = next
		releaseMu.Unlock()
	})
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	errors := make(chan error, 2)
	go func() {
		errors <- companion.RunTunnelWithHooks(ctx, profile, control.Port(state), func(status companion.TunnelStatus) {
			tunnelMu.Lock()
			tunnel = status
			tunnelMu.Unlock()
		}, logger, func() { portManager.OnConnected(ctx, portErrors) })
	}()
	if announce {
		fmt.Println(i18n.T("Companion %q is running.", profile.Name))
	}
	var runErr error
	received := 0
	select {
	case <-signals:
	case <-stop:
	case runErr = <-errors:
		received = 1
	}
	cancel()
	for received < 2 {
		if err := <-errors; runErr == nil && err != nil {
			runErr = err
		}
		received++
	}
	if runErr != nil {
		logger.Error("Companion stopped with error: %v", runErr)
	} else {
		logger.Info("Companion stopped")
	}
	return runErr
}

func runBridge() error {
	var bootstrap bridgeBootstrap
	if err := json.NewDecoder(io.LimitReader(os.Stdin, 64*1024)).Decode(&bootstrap); err != nil {
		return fmt.Errorf("read bridge bootstrap: %w", err)
	}
	if bootstrap.ControlToken == "" {
		return errors.New("bridge bootstrap did not include a control token")
	}
	d, err := daemon.Start(bootstrap.ControlToken)
	if err != nil {
		return fmt.Errorf("start local bridge: %w", err)
	}
	defer d.Close()
	if logger, logErr := companion.NewLogger("bridge"); logErr == nil {
		defer logger.Close()
		d.Bridge.SetLogger(logger)
		logger.Info("bridge started: pid=%d", os.Getpid())
		defer logger.Info("bridge stopped")
	} else {
		fmt.Fprintf(os.Stderr, "bridge logging disabled: %v\n", logErr)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	<-signals
	return nil
}

func runDoctor() error {
	state, err := daemon.LoadState()
	if err != nil || !control.Healthy(state) {
		return errors.New("bridge: unavailable (start the Companion with `agentport companion start <profile>`)")
	}
	fmt.Println(i18n.T("bridge: healthy at %s (PID %d)", state.Address, state.PID))
	return nil
}

func ensureBridge() (daemon.State, bool, error) {
	state, err := daemon.LoadState()
	if err == nil && control.Healthy(state) {
		return state, false, nil
	}
	state, err = startBridge()
	return state, true, err
}

func startBridge() (daemon.State, error) {
	token, err := randomToken(32)
	if err != nil {
		return daemon.State{}, err
	}
	payload, err := json.Marshal(bridgeBootstrap{ControlToken: token})
	if err != nil {
		return daemon.State{}, fmt.Errorf("encode bridge bootstrap: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return daemon.State{}, fmt.Errorf("locate agentport executable: %w", err)
	}
	command := exec.Command(executable, "bridge")
	command.Stdin = bytes.NewReader(payload)
	if err := startDetached(command); err != nil {
		return daemon.State{}, fmt.Errorf("start local bridge process: %w", err)
	}
	pid := command.Process.Pid
	deadline := time.Now().Add(startupTimeout)
	for time.Now().Before(deadline) {
		state, err := daemon.LoadState()
		if err == nil && state.PID == pid && control.Healthy(state) {
			_ = command.Process.Release()
			return state, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = command.Process.Kill()
	_, _ = command.Process.Wait()
	return daemon.State{}, errors.New("local bridge did not start within the expected time")
}

// watchReleaseStatus checks once after Companion startup and then relies on
// the shared disk cache to avoid repeated GitHub calls from other Companions.
func watchReleaseStatus(ctx context.Context, state daemon.State, logger *companion.Logger, set func(release.Status)) {
	checker := release.NewChecker()
	refresh := func() {
		status, err := checker.Current(ctx)
		if err != nil {
			logger.Error("release check failed: %v", err)
		}
		set(status)
		if err := control.PublishReleaseStatus(state, status); err != nil {
			logger.Error("publish release status: %v", err)
		}
	}
	refresh()
	ticker := time.NewTicker(release.CacheTTL)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate secure random value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func randomPort() (int, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(remotePortMax-remotePortMin+1))
	if err != nil {
		return 0, fmt.Errorf("choose remote SSH port: %w", err)
	}
	return remotePortMin + int(value.Int64()), nil
}

// How long a start waits for another start of the same profile, and how old a
// start lock must be before it is taken for a leftover of a crashed start.
const (
	profileStartWait  = 25 * time.Second
	profileStartStale = 20 * time.Second
)

// acquireProfileStartLock serialises the starts of one profile. It is held only
// while a Companion decides whether to start and writes its runtime state, so a
// waiting start gets the lock as soon as the first one has either claimed the
// profile or given up.
func acquireProfileStartLock(name string, wait, stale time.Duration) (func(), error) {
	directory, err := paths.Dir("companions")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, fmt.Errorf("create agentport cache directory: %w", err)
	}
	path := filepath.Join(directory, name+".start.lock")
	deadline := time.Now().Add(wait)
	for {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			return func() {
				_ = file.Close()
				_ = os.Remove(path)
			}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("lock the start of Companion %q: %w", name, err)
		}
		// A leftover of a crashed start is taken over. Where the file cannot be
		// removed (Windows keeps a file that a live process still has open), fall
		// through to the deadline instead of retrying without end.
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > stale {
			if os.Remove(path) == nil {
				continue
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("another start of Companion %q is in progress", name)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
