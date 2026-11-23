package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rafaelb13/agentport/internal/companion"
	"github.com/rafaelb13/agentport/internal/i18n"
	"github.com/rafaelb13/agentport/internal/mcpserver"
	"github.com/rafaelb13/agentport/internal/ports"
	"github.com/rafaelb13/agentport/internal/portsproto"
)

// valueFlags are the port commands' flags that take a value, so the parser knows
// which word after them is not a positional argument.
var valueFlags = map[string]bool{"--profile": true, "--port": true, "--local": true, "--duration": true}

// splitArgs separates flags from positional arguments, in any order.
func splitArgs(args []string) (flags, positional []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		if valueFlags[arg] && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return flags, positional
}

// portProfile is the profile a port command talks to: --profile, or the only
// profile that exists.
func portProfile(flagged string) (string, error) {
	if flagged != "" {
		return flagged, nil
	}
	profiles, err := companion.ListProfiles()
	if err != nil {
		return "", err
	}
	switch len(profiles) {
	case 0:
		return "", errors.New(i18n.T("no server profile exists; run `agentport setup` first"))
	case 1:
		return profiles[0].Name, nil
	default:
		return "", errors.New(i18n.T("several server profiles exist; pass --profile NAME"))
	}
}

// companionFor returns the running Companion of a profile, or says how to start it.
func companionFor(profile string) (companion.RuntimeState, error) {
	state, err := companion.LoadRuntime(profile)
	if err != nil || !companion.RuntimeHealthy(state) {
		return companion.RuntimeState{}, errors.New(i18n.T("Companion %q is not running; start it with `agentport companion start %s`", profile, profile))
	}
	return state, nil
}

// portsAPI calls the Companion's local port API with its control token.
func portsAPI(state companion.RuntimeState, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, "http://"+state.Address+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+state.ControlToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 150 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		var eb portsproto.ErrorBody
		_ = json.NewDecoder(io.LimitReader(resp.Body, 16*1024)).Decode(&eb)
		if eb.Message == "" {
			return fmt.Errorf("Companion answered HTTP %d", resp.StatusCode)
		}
		return errors.New(eb.Message)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4*1024*1024)).Decode(out)
}

// portState is what the local API returns about the profile's ports.
type portState struct {
	Pending  []ports.PendingRequest   `json:"pending"`
	Forwards []portsproto.ForwardInfo `json:"forwards"`
}

func fetchState(state companion.RuntimeState) (portState, error) {
	var out portState
	err := portsAPI(state, http.MethodGet, "/v1/ports/state", nil, &out)
	return out, err
}

// runPorts lists the listening ports. Without a profile it scans this machine
// (the server side); with one it runs the scan on that server over ssh.
func runPorts(args []string) error {
	flags, positional := splitArgs(args)
	fs := flag.NewFlagSet("ports", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, i18n.T("print JSON"))
	all := fs.Bool("all", false, i18n.T("include system services"))
	if err := fs.Parse(flags); err != nil {
		return err
	}
	if len(positional) > 1 {
		return errors.New("usage: agentport ports [profile] [--json] [--all]")
	}
	var entries []mcpserver.ListEntry
	if len(positional) == 0 {
		local, err := mcpserver.ListPorts(context.Background(), nil, nil, *all, false)
		if err != nil {
			return err
		}
		entries = local
	} else {
		remote, err := scanRemote(positional[0], *all)
		if err != nil {
			return err
		}
		entries = remote
		// Mark what this machine already forwards from that server, when its Companion runs.
		if state, err := companionFor(positional[0]); err == nil {
			if st, err := fetchState(state); err == nil {
				mcpserver.MarkForwarded(entries, st.Forwards)
			}
		}
	}
	if *asJSON {
		return writeJSONTo(os.Stdout, map[string]any{"ports": entries})
	}
	if len(entries) == 0 {
		fmt.Fprintln(os.Stdout, i18n.T("No listening ports found."))
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PORT\tBIND\tPID\tPROCESS\tUSER\tFORWARDED\tCOMMAND")
	for _, e := range entries {
		forwarded := ""
		if e.Forwarded != nil {
			forwarded = e.Forwarded.URL
		}
		fmt.Fprintf(tw, "%d\t%s\t%d\t%s\t%s\t%s\t%s\n", e.Port, e.Bind, e.PID, e.Process, e.User, forwarded, e.Cmdline)
	}
	return tw.Flush()
}

// scanRemote runs `agentport ports --json` on the profile's server over ssh.
func scanRemote(profileName string, all bool) ([]mcpserver.ListEntry, error) {
	profile, err := companion.LoadProfile(profileName)
	if err != nil {
		return nil, err
	}
	remote := `sh -lc 'PATH="$HOME/.local/bin:$PATH" agentport ports --json`
	if all {
		remote += " --all"
	}
	remote += "'"
	arguments := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}
	if profile.SSHIdentityFile != "" {
		arguments = append(arguments, "-i", profile.SSHIdentityFile, "-o", "IdentitiesOnly=yes")
	}
	arguments = append(arguments, profile.Destination, remote)
	out, err := exec.Command("ssh", arguments...).Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("list the ports on the server over SSH"), err)
	}
	var body struct {
		Ports []mcpserver.ListEntry `json:"ports"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("read the server's port list"), err)
	}
	return body.Ports, nil
}

func runRequests(args []string) error {
	flags, positional := splitArgs(args)
	fs := flag.NewFlagSet("requests", flag.ContinueOnError)
	profileFlag := fs.String("profile", "", i18n.T("server profile"))
	if err := fs.Parse(flags); err != nil || len(positional) != 0 {
		return errors.New("usage: agentport requests [--profile NAME]")
	}
	profile, err := portProfile(*profileFlag)
	if err != nil {
		return err
	}
	state, err := companionFor(profile)
	if err != nil {
		return err
	}
	st, err := fetchState(state)
	if err != nil {
		return err
	}
	if len(st.Pending) == 0 {
		fmt.Fprintln(os.Stdout, i18n.T("No pending requests."))
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "REQUEST\tAGENT\tFOLDER\tPORTS\tREASON")
	for _, r := range st.Pending {
		var list []string
		for _, p := range r.Ports {
			list = append(list, strconv.Itoa(p.RemotePort))
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.ID, r.Agent, r.Cwd, strings.Join(list, ","), r.Reason)
	}
	return tw.Flush()
}

// runApprove decides a request: --all approves every port, --port N only that one.
// --local picks the local port of that one port.
func runApprove(args []string) error {
	flags, positional := splitArgs(args)
	fs := flag.NewFlagSet("approve", flag.ContinueOnError)
	profileFlag := fs.String("profile", "", i18n.T("server profile"))
	all := fs.Bool("all", false, i18n.T("approve every port of the request"))
	only := fs.Int("port", 0, i18n.T("approve only this remote port"))
	local := fs.Int("local", 0, i18n.T("local port for the approved port"))
	duration := fs.String("duration", ports.DurationSession, i18n.T("session, 1h or pin"))
	if err := fs.Parse(flags); err != nil || len(positional) != 1 {
		return errors.New("usage: agentport approve <request-id> [--all | --port N] [--local N] [--duration session|1h|pin] [--profile NAME]")
	}
	if *all == (*only != 0) {
		return errors.New(i18n.T("approve needs exactly one of --all or --port N"))
	}
	profile, err := portProfile(*profileFlag)
	if err != nil {
		return err
	}
	state, err := companionFor(profile)
	if err != nil {
		return err
	}
	st, err := fetchState(state)
	if err != nil {
		return err
	}
	var request *ports.PendingRequest
	for i := range st.Pending {
		if st.Pending[i].ID == positional[0] {
			request = &st.Pending[i]
		}
	}
	if request == nil {
		return errors.New(i18n.T("no pending request %s for profile %s", positional[0], profile))
	}
	body := approveBody{Duration: *duration}
	if *local != 0 && *all && len(request.Ports) > 1 {
		return errors.New(i18n.T("--local needs --port N when the request has several ports"))
	}
	found := false
	for _, p := range request.Ports {
		pick := *all || p.RemotePort == *only
		if pick {
			found = true
		}
		item := approvePort{RemotePort: p.RemotePort, Skip: !pick}
		if pick && *local != 0 {
			item.LocalPort = *local
		}
		body.Ports = append(body.Ports, item)
	}
	if !found {
		return errors.New(i18n.T("port %d is not in request %s", *only, positional[0]))
	}
	var out portsproto.ForwardResponse
	if err := portsAPI(state, http.MethodPost, "/v1/ports/requests/"+positional[0]+"/approve", body, &out); err != nil {
		return err
	}
	return printResults(out.Results)
}

type approveBody struct {
	Duration string        `json:"duration"`
	Ports    []approvePort `json:"ports"`
}

type approvePort struct {
	RemotePort int  `json:"remote_port"`
	LocalPort  int  `json:"local_port,omitempty"`
	Skip       bool `json:"skip,omitempty"`
}

func printResults(results []portsproto.PortResult) error {
	for _, r := range results {
		line := fmt.Sprintf("port %d: %s", r.RemotePort, r.State)
		if r.URL != "" {
			line += " " + r.URL
		}
		if r.Message != "" {
			line += " (" + r.Message + ")"
		}
		fmt.Fprintln(os.Stdout, line)
	}
	return nil
}

func runDeny(args []string) error {
	flags, positional := splitArgs(args)
	fs := flag.NewFlagSet("deny", flag.ContinueOnError)
	profileFlag := fs.String("profile", "", i18n.T("server profile"))
	if err := fs.Parse(flags); err != nil || len(positional) != 1 {
		return errors.New("usage: agentport deny <request-id> [--profile NAME]")
	}
	profile, err := portProfile(*profileFlag)
	if err != nil {
		return err
	}
	state, err := companionFor(profile)
	if err != nil {
		return err
	}
	if err := portsAPI(state, http.MethodPost, "/v1/ports/requests/"+positional[0]+"/deny", nil, nil); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, i18n.T("Denied request %s.", positional[0]))
	return nil
}

func runForwards(args []string) error {
	flags, positional := splitArgs(args)
	fs := flag.NewFlagSet("forwards", flag.ContinueOnError)
	profileFlag := fs.String("profile", "", i18n.T("server profile"))
	asJSON := fs.Bool("json", false, i18n.T("print JSON"))
	if err := fs.Parse(flags); err != nil || len(positional) != 0 {
		return errors.New("usage: agentport forwards [--profile NAME] [--json]")
	}
	profile, err := portProfile(*profileFlag)
	if err != nil {
		return err
	}
	state, err := companionFor(profile)
	if err != nil {
		return err
	}
	st, err := fetchState(state)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSONTo(os.Stdout, map[string]any{"forwards": st.Forwards})
	}
	if len(st.Forwards) == 0 {
		fmt.Fprintln(os.Stdout, i18n.T("No forwarded ports."))
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "REMOTE\tLOCAL\tSTATE\tORIGIN\tOWNERS\tURL")
	for _, f := range st.Forwards {
		fmt.Fprintf(tw, "%d\t%d\t%s\t%s\t%s\t%s\n", f.RemotePort, f.LocalPort, f.State, f.Origin, strings.Join(f.Owners, ","), f.URL)
	}
	return tw.Flush()
}

// runUnforward closes forwarded ports from the user's side, whatever their owners.
func runUnforward(args []string) error {
	flags, positional := splitArgs(args)
	fs := flag.NewFlagSet("unforward", flag.ContinueOnError)
	profileFlag := fs.String("profile", "", i18n.T("server profile"))
	all := fs.Bool("all", false, i18n.T("close every forwarded port"))
	if err := fs.Parse(flags); err != nil {
		return err
	}
	if *all == (len(positional) > 0) {
		return errors.New("usage: agentport unforward <remote-port...> | --all [--profile NAME]")
	}
	profile, err := portProfile(*profileFlag)
	if err != nil {
		return err
	}
	state, err := companionFor(profile)
	if err != nil {
		return err
	}
	var remote []int
	if *all {
		st, err := fetchState(state)
		if err != nil {
			return err
		}
		for _, f := range st.Forwards {
			remote = append(remote, f.RemotePort)
		}
	} else {
		for _, p := range positional {
			n, err := strconv.Atoi(p)
			if err != nil || !portsproto.ValidPort(n) {
				return errors.New(i18n.T("%q is not a port number", p))
			}
			remote = append(remote, n)
		}
	}
	if len(remote) == 0 {
		fmt.Fprintln(os.Stdout, i18n.T("No forwarded ports."))
		return nil
	}
	var out struct {
		Closed  []int `json:"closed"`
		NotOpen []int `json:"not_open"`
	}
	if err := portsAPI(state, http.MethodPost, "/v1/ports/forwards/close", map[string][]int{"remote_ports": remote}, &out); err != nil {
		return err
	}
	for _, p := range out.Closed {
		fmt.Fprintln(os.Stdout, i18n.T("Closed port %d.", p))
	}
	for _, p := range out.NotOpen {
		fmt.Fprintln(os.Stdout, i18n.T("Port %d was not forwarded.", p))
	}
	return nil
}

func writeJSONTo(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
