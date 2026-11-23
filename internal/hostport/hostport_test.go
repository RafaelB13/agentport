package hostport

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeCmd answers commands by "name first-arg" and records every call.
type fakeCmd struct {
	routes map[string]fakeOut
	calls  [][]string
}

type fakeOut struct {
	out string
	err error
}

func (f *fakeCmd) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	key := name
	if len(args) > 0 {
		key += " " + args[0]
	}
	r, ok := f.routes[key]
	if !ok {
		return nil, &exec.ExitError{} // unknown command: behave like a non-zero exit with no output
	}
	return []byte(r.out), r.err
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakeSig records signals. exitOnTerm/exitOnKill decide when the pid disappears.
type fakeSig struct {
	sent       []syscall.Signal
	alive      bool
	exitOnTerm bool
	exitOnKill bool
}

func (s *fakeSig) Signal(_ int, sig syscall.Signal) error {
	s.sent = append(s.sent, sig)
	if sig == syscall.SIGTERM && s.exitOnTerm {
		s.alive = false
	}
	if sig == syscall.SIGKILL && s.exitOnKill {
		s.alive = false
	}
	return nil
}

func (s *fakeSig) Alive(int) bool { return s.alive }

func testEnv(uid int, goos string, cmd *fakeCmd) env {
	return env{
		goos:      goos,
		uid:       uid,
		run:       cmd.run,
		sig:       &fakeSig{},
		listening: func(int) bool { return false },
		grace:     20 * time.Millisecond,
		poll:      time.Millisecond,
	}
}

func TestClassification(t *testing.T) {
	proc := fixture(t, "lsof_proc.txt")
	root := fixture(t, "lsof_root.txt")
	dockerProxy := "p4242\ncdocker-proxy\nu501\nLrafael\n"
	sysdOut := fixture(t, "systemctl_service.txt")
	sysdNone := fixture(t, "systemctl_none.txt")
	launchPrint := fixture(t, "launchctl_print.txt")
	launchPrintOther := strings.Replace(launchPrint, "pid = 4242", "pid = 9999", 1)
	postgres := "p4242\ncpostgres\nu501\nLrafael\n"

	tests := []struct {
		name     string
		goos     string
		routes   map[string]fakeOut
		wantKind Kind
		wantStop []string
		wantSvc  string
		wantCont string
		wantErr  bool
		wantNil  bool
	}{
		{
			name:     "same user plain process is proc with no stop command",
			goos:     "linux",
			routes:   map[string]fakeOut{"lsof -nP": {out: proc}, "systemctl --user": {out: sysdNone}},
			wantKind: KindProc,
		},
		{
			name:     "root listener is system and not stoppable",
			goos:     "linux",
			routes:   map[string]fakeOut{"lsof -nP": {out: root}},
			wantKind: KindSystem,
		},
		{
			name: "other uid wins over docker name",
			goos: "darwin",
			routes: map[string]fakeOut{
				"lsof -nP":  {out: "p4242\ncdocker-proxy\nu0\nLroot\n"},
				"docker ps": {out: "web\n"},
			},
			wantKind: KindSystem,
		},
		{
			name: "docker-proxy with container name is container",
			goos: "darwin",
			routes: map[string]fakeOut{
				"lsof -nP":  {out: dockerProxy},
				"docker ps": {out: "web\n"},
			},
			wantKind: KindContainer,
			wantStop: []string{"docker", "stop", "web"},
			wantCont: "web",
		},
		{
			name:     "docker-proxy without a container name has no stop command",
			goos:     "darwin",
			routes:   map[string]fakeOut{"lsof -nP": {out: dockerProxy}, "docker ps": {out: ""}},
			wantKind: KindContainer,
		},
		{
			name: "homebrew service on darwin",
			goos: "darwin",
			routes: map[string]fakeOut{
				"lsof -nP":        {out: postgres},
				"launchctl blame": {out: "homebrew.mxcl.postgresql@16\n"},
				"launchctl print": {out: launchPrint},
			},
			wantKind: KindService,
			wantStop: []string{"brew", "services", "stop", "postgresql@16"},
			wantSvc:  "postgresql@16",
		},
		{
			name: "launchd agent on darwin is bootout",
			goos: "darwin",
			routes: map[string]fakeOut{
				"lsof -nP":        {out: postgres},
				"launchctl blame": {out: "com.example.agent\n"},
				"launchctl print": {out: strings.Replace(launchPrint, "homebrew.mxcl.postgresql@16", "com.example.agent", 1)},
			},
			wantKind: KindService,
			wantStop: []string{"launchctl", "bootout", "gui/501/com.example.agent"},
			wantSvc:  "com.example.agent",
		},
		{
			name: "blame without matching job pid stays proc",
			goos: "darwin",
			routes: map[string]fakeOut{
				"lsof -nP":        {out: postgres},
				"launchctl blame": {out: "com.apple.Terminal\n"},
				"launchctl print": {out: launchPrintOther},
			},
			wantKind: KindProc,
		},
		{
			name: "failed blame stays proc",
			goos: "darwin",
			routes: map[string]fakeOut{
				"lsof -nP":        {out: postgres},
				"launchctl blame": {err: &exec.ExitError{}},
			},
			wantKind: KindProc,
		},
		{
			name:     "systemd --user unit on linux is service",
			goos:     "linux",
			routes:   map[string]fakeOut{"lsof -nP": {out: proc}, "systemctl --user": {out: sysdOut}},
			wantKind: KindService,
			wantStop: []string{"systemctl", "--user", "stop", "app.service"},
			wantSvc:  "app.service",
		},
		{
			name:     "pid outside any unit on linux stays proc",
			goos:     "linux",
			routes:   map[string]fakeOut{"lsof -nP": {out: proc}, "systemctl --user": {out: sysdNone}},
			wantKind: KindProc,
		},
		{
			name:    "free port returns nil without error",
			goos:    "darwin",
			routes:  map[string]fakeOut{"lsof -nP": {out: "", err: &exec.ExitError{}}},
			wantNil: true,
		},
		{
			name:    "lsof missing is an error",
			goos:    "darwin",
			routes:  map[string]fakeOut{"lsof -nP": {err: errors.New("exec: lsof: not found")}},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &fakeCmd{routes: tc.routes}
			got, err := lookupWith(context.Background(), testEnv(501, tc.goos, cmd), 4242)
			if tc.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("want nil owner for free port, got %+v", got)
				}
				return
			}
			if got.Kind != tc.wantKind {
				t.Errorf("Kind = %q, want %q", got.Kind, tc.wantKind)
			}
			if !slices.Equal(got.StopCmd, tc.wantStop) {
				t.Errorf("StopCmd = %v, want %v", got.StopCmd, tc.wantStop)
			}
			if got.Service != tc.wantSvc {
				t.Errorf("Service = %q, want %q", got.Service, tc.wantSvc)
			}
			if got.Container != tc.wantCont {
				t.Errorf("Container = %q, want %q", got.Container, tc.wantCont)
			}
		})
	}
}

func TestLookupRejectsInvalidPort(t *testing.T) {
	cmd := &fakeCmd{}
	if _, err := lookupWith(context.Background(), testEnv(501, "linux", cmd), 0); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("err = %v, want ErrInvalidPort", err)
	}
	if len(cmd.calls) != 0 {
		t.Fatal("lsof must not run for an invalid port")
	}
}

func TestSystemOwnerRootUIDFromFixture(t *testing.T) {
	cmd := &fakeCmd{routes: map[string]fakeOut{"lsof -nP": {out: fixture(t, "lsof_root.txt")}}}
	got, err := lookupWith(context.Background(), testEnv(501, "linux", cmd), 5432)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindSystem || got.StopCmd != nil || got.User != "root" {
		t.Fatalf("got %+v, want system owner with nil StopCmd and user root", got)
	}
}

func TestStopProcSIGTermPath(t *testing.T) {
	sig := &fakeSig{alive: true, exitOnTerm: true}
	e := testEnv(501, "linux", &fakeCmd{})
	e.sig = sig
	o := &Owner{Port: 4242, PID: 4242, Kind: KindProc}
	if err := stopWith(context.Background(), e, o); err != nil {
		t.Fatalf("Stop = %v, want nil", err)
	}
	if !slices.Equal(sig.sent, []syscall.Signal{syscall.SIGTERM}) {
		t.Fatalf("signals = %v, want only SIGTERM", sig.sent)
	}
}

func TestStopProcEscalatesToSIGKILL(t *testing.T) {
	sig := &fakeSig{alive: true, exitOnKill: true}
	e := testEnv(501, "linux", &fakeCmd{})
	e.sig = sig
	o := &Owner{Port: 4242, PID: 4242, Kind: KindProc}
	if err := stopWith(context.Background(), e, o); err != nil {
		t.Fatalf("Stop = %v, want nil", err)
	}
	if !slices.Equal(sig.sent, []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL}) {
		t.Fatalf("signals = %v, want SIGTERM then SIGKILL", sig.sent)
	}
}

func TestStopProcFailsWhenPidSurvivesKill(t *testing.T) {
	sig := &fakeSig{alive: true}
	e := testEnv(501, "linux", &fakeCmd{})
	e.sig = sig
	if err := stopWith(context.Background(), e, &Owner{Port: 4242, PID: 4242, Kind: KindProc}); err == nil {
		t.Fatal("Stop = nil, want error when the process survives SIGKILL")
	}
}

func TestStopReportsStillBusy(t *testing.T) {
	sig := &fakeSig{alive: true, exitOnTerm: true}
	e := testEnv(501, "linux", &fakeCmd{})
	e.sig = sig
	e.listening = func(int) bool { return true }
	err := stopWith(context.Background(), e, &Owner{Port: 4242, PID: 4242, Kind: KindProc})
	if !errors.Is(err, ErrStillBusy) {
		t.Fatalf("err = %v, want ErrStillBusy", err)
	}
}

func TestStopSystemIsNotStoppable(t *testing.T) {
	sig := &fakeSig{alive: true, exitOnTerm: true}
	e := testEnv(501, "linux", &fakeCmd{})
	e.sig = sig
	err := stopWith(context.Background(), e, &Owner{Port: 5432, PID: 311, Kind: KindSystem})
	if !errors.Is(err, ErrNotStoppable) {
		t.Fatalf("err = %v, want ErrNotStoppable", err)
	}
	if len(sig.sent) != 0 {
		t.Fatalf("signals sent to a system owner: %v", sig.sent)
	}
}

func TestStopServiceRunsStopCmd(t *testing.T) {
	cmd := &fakeCmd{routes: map[string]fakeOut{"brew services": {out: ""}}}
	e := testEnv(501, "darwin", cmd)
	o := &Owner{Port: 5432, Kind: KindService, StopCmd: []string{"brew", "services", "stop", "postgresql@16"}}
	if err := stopWith(context.Background(), e, o); err != nil {
		t.Fatalf("Stop = %v, want nil", err)
	}
	want := []string{"brew", "services", "stop", "postgresql@16"}
	if len(cmd.calls) != 1 || !slices.Equal(cmd.calls[0], want) {
		t.Fatalf("calls = %v, want [%v]", cmd.calls, want)
	}
}

func TestStopContainerWithoutStopCmdIsNotStoppable(t *testing.T) {
	err := stopWith(context.Background(), testEnv(501, "darwin", &fakeCmd{}), &Owner{Port: 8080, Kind: KindContainer})
	if !errors.Is(err, ErrNotStoppable) {
		t.Fatalf("err = %v, want ErrNotStoppable", err)
	}
}

func TestStopNilOwner(t *testing.T) {
	if err := stopWith(context.Background(), testEnv(501, "linux", &fakeCmd{}), nil); err == nil {
		t.Fatal("want error for nil owner")
	}
}

func TestOSSignalerAliveForOwnProcess(t *testing.T) {
	// The test process itself is alive; the platform signaler must report it.
	if !newSignaler(nil).Alive(os.Getpid()) {
		t.Fatal("Alive(self) = false")
	}
}

func TestLookupFreePortReal(t *testing.T) {
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof not installed")
	}
	port := freePort(t)
	o, err := Lookup(context.Background(), port)
	if err != nil {
		t.Skipf("Lookup unavailable here: %v", err)
	}
	if o != nil {
		t.Fatalf("Lookup(%d) = %+v on a port nothing listens on", port, o)
	}
}

// freePort returns a loopback port that was free a moment ago.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}
