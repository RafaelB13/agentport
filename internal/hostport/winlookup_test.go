package hostport

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// routeAll answers commands by their full "name arg arg..." line, so two netstat
// calls with different protocol arguments can have different answers.
func routeAll(routes map[string]fakeOut) runner {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		key := strings.Join(append([]string{name}, args...), " ")
		r, ok := routes[key]
		if !ok {
			return nil, errors.New("unexpected command: " + key)
		}
		return []byte(r.out), r.err
	}
}

func winFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// winRoutes returns the command answers for a Windows host with the given fixtures.
func winRoutes(t *testing.T) map[string]fakeOut {
	t.Helper()
	netstat := winFixture(t, "netstat_win.txt")
	v6 := winFixture(t, "netstat_v6_win.txt")
	tasks := winFixture(t, "tasklist_v_win.txt")
	svcs := winFixture(t, "tasklist_svc_win.txt")
	return map[string]fakeOut{
		"netstat -ano -p TCP":                                 {out: netstat},
		"netstat -ano -p TCPv6":                               {out: v6},
		"tasklist /V /FO CSV /NH":                             {out: tasks},
		"tasklist /svc /FO CSV /NH":                           {out: svcs},
		"docker ps --filter publish=7000 --format {{.Names}}": {out: "pg-dev\n"},
	}
}

func TestLookupWindowsClassification(t *testing.T) {
	tests := []struct {
		name     string
		port     int
		account  string
		mutate   func(map[string]fakeOut)
		wantNil  bool
		wantErr  bool
		wantKind Kind
		wantPID  int
		wantProc string
		wantUser string
		wantSvc  string
		wantCont string
		wantStop []string
	}{
		{
			name: "own process is proc", port: 3000, account: `DESKTOP-K2\rafael`,
			wantKind: KindProc, wantPID: 12345, wantProc: "node", wantUser: `DESKTOP-K2\rafael`,
		},
		{
			name: "IPv6 listener is found", port: 9000, account: `DESKTOP-K2\rafael`,
			wantKind: KindProc, wantPID: 9001, wantProc: "ssh", wantUser: `DESKTOP-K2\rafael`,
		},
		{
			name: "pid hosting a service is service with no stop command", port: 5432, account: `DESKTOP-K2\rafael`,
			wantKind: KindService, wantPID: 8120, wantProc: "postgres", wantUser: `DESKTOP-K2\rafael`,
			wantSvc: "postgresql-x64-16",
		},
		{
			name: "System pid 4 is system", port: 445, account: `DESKTOP-K2\rafael`,
			wantKind: KindSystem, wantPID: 4, wantProc: "System", wantUser: `NT AUTHORITY\SYSTEM`,
		},
		{
			name: "another account is system", port: 6000, account: `DESKTOP-K2\rafael`,
			wantKind: KindSystem, wantPID: 6000, wantProc: "AdminTool", wantUser: `DESKTOP-K2\admin`,
		},
		{
			name: "unknown own account fails closed to system", port: 3000, account: "",
			wantKind: KindSystem, wantPID: 12345, wantProc: "node", wantUser: `DESKTOP-K2\rafael`,
		},
		{
			name: "docker backend is container with docker stop", port: 7000, account: `DESKTOP-K2\rafael`,
			wantKind: KindContainer, wantPID: 7000, wantProc: "com.docker.backend", wantUser: `DESKTOP-K2\rafael`,
			wantCont: "pg-dev", wantStop: []string{"docker", "stop", "pg-dev"},
		},
		{
			name: "free port is nil", port: 4444, account: `DESKTOP-K2\rafael`, wantNil: true,
		},
		{
			name: "netstat failure is an error", port: 3000, account: `DESKTOP-K2\rafael`,
			mutate:  func(r map[string]fakeOut) { r["netstat -ano -p TCP"] = fakeOut{err: errors.New("boom")} },
			wantErr: true,
		},
		{
			name: "service list failure fails closed", port: 3000, account: `DESKTOP-K2\rafael`,
			mutate:  func(r map[string]fakeOut) { r["tasklist /svc /FO CSV /NH"] = fakeOut{err: errors.New("boom")} },
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routes := winRoutes(t)
			if tt.mutate != nil {
				tt.mutate(routes)
			}
			e := env{goos: "windows", user: tt.account, run: routeAll(routes), grace: 0}
			o, err := lookupWindows(context.Background(), e, tt.port)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantNil {
				if o != nil {
					t.Fatalf("want nil owner, got %+v", o)
				}
				return
			}
			if o.Kind != tt.wantKind || o.PID != tt.wantPID || o.Process != tt.wantProc || o.User != tt.wantUser {
				t.Fatalf("owner = %+v", o)
			}
			if o.Service != tt.wantSvc || o.Container != tt.wantCont || !reflect.DeepEqual(o.StopCmd, tt.wantStop) {
				t.Fatalf("owner = %+v", o)
			}
			if tt.wantKind == KindService && (o.Manager != "windows service" || len(o.StopCmd) != 0) {
				t.Fatalf("service must not carry a stop command: %+v", o)
			}
		})
	}
}

func TestLookupWindowsDispatchFromLookupWith(t *testing.T) {
	e := env{goos: "windows", user: `DESKTOP-K2\rafael`, run: routeAll(winRoutes(t))}
	o, err := lookupWith(context.Background(), e, 3000)
	if err != nil || o == nil || o.Kind != KindProc || o.PID != 12345 {
		t.Fatalf("owner = %+v, err = %v", o, err)
	}
}

func TestStopWindowsProcessSendsTaskkillThenStops(t *testing.T) {
	sig := &fakeSig{alive: true, exitOnTerm: true}
	e := env{goos: "windows", sig: sig, listening: func(int) bool { return false }, grace: 20 * time.Millisecond, poll: time.Millisecond}
	o := &Owner{Port: 3000, PID: 12345, Kind: KindProc}
	if err := stopWith(context.Background(), e, o); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sig.sent, []syscall.Signal{syscall.SIGTERM}) {
		t.Fatalf("signals = %v", sig.sent)
	}
}

func TestStopWindowsProcessEscalatesToForcedKill(t *testing.T) {
	sig := &fakeSig{alive: true, exitOnKill: true}
	e := env{goos: "windows", sig: sig, listening: func(int) bool { return false }, grace: 20 * time.Millisecond, poll: time.Millisecond}
	o := &Owner{Port: 3000, PID: 12345, Kind: KindProc}
	if err := stopWith(context.Background(), e, o); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sig.sent, []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL}) {
		t.Fatalf("signals = %v", sig.sent)
	}
}

func TestStopWindowsServiceAndSystemAreNeverSignalled(t *testing.T) {
	for _, o := range []*Owner{
		{Port: 5432, PID: 8120, Kind: KindService, Service: "postgresql-x64-16"},
		{Port: 445, PID: 4, Kind: KindSystem},
	} {
		sig := &fakeSig{alive: true}
		e := env{goos: "windows", sig: sig, listening: func(int) bool { return false }}
		if err := stopWith(context.Background(), e, o); !errors.Is(err, ErrNotStoppable) {
			t.Fatalf("%s: err = %v, want ErrNotStoppable", o.Kind, err)
		}
		if len(sig.sent) != 0 {
			t.Fatalf("%s: signals sent: %v", o.Kind, sig.sent)
		}
	}
}

func TestTaskkillArgs(t *testing.T) {
	if got := taskkillArgs(12345, syscall.SIGTERM); !reflect.DeepEqual(got, []string{"/PID", "12345"}) {
		t.Fatalf("SIGTERM args = %v", got)
	}
	if got := taskkillArgs(12345, syscall.SIGKILL); !reflect.DeepEqual(got, []string{"/F", "/PID", "12345"}) {
		t.Fatalf("SIGKILL args = %v", got)
	}
}

func TestParseNetstatListenersIncludesV6AndSkipsEstablished(t *testing.T) {
	got := parseNetstatListeners([]byte(winFixture(t, "netstat_win.txt") + winFixture(t, "netstat_v6_win.txt")))
	has := func(port, pid int) bool {
		for _, ep := range got {
			if ep.Port == port && ep.PID == pid {
				return true
			}
		}
		return false
	}
	if !has(3000, 12345) || !has(9000, 9001) || !has(445, 4) {
		t.Fatalf("missing listeners: %+v", got)
	}
	for _, ep := range got {
		if ep.Port == 50000 {
			t.Fatalf("established connection reported as listener: %+v", ep)
		}
	}
}

func TestParseTasklistServicesSkipsNA(t *testing.T) {
	got := parseTasklistServices([]byte(winFixture(t, "tasklist_svc_win.txt")))
	if !reflect.DeepEqual(got[2200], []string{"Dhcp", "Dnscache"}) {
		t.Fatalf("svchost services = %v", got[2200])
	}
	if _, ok := got[12345]; ok {
		t.Fatal("N/A must not produce a service")
	}
}

func TestSameAccountIsStrictAboutUnknowns(t *testing.T) {
	if !sameAccount(`DESKTOP\rafael`, `desktop\RAFAEL`) {
		t.Fatal("account names compare case-insensitively")
	}
	if sameAccount("", "") || sameAccount("x", "") || sameAccount("", "x") {
		t.Fatal("an unknown account must never match")
	}
}

func TestSupportedIncludesWindows(t *testing.T) {
	if !supported("windows") || supported("plan9") {
		t.Fatal("windows must be supported")
	}
}
