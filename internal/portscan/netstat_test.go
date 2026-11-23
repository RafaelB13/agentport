package portscan

import (
	"os"
	"reflect"
	"testing"
)

func readWinFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseNetstatKeepsOnlyListeners(t *testing.T) {
	got := parseNetstat(readWinFixture(t, "netstat_ano_win.txt"))
	want := []netstatEndpoint{
		{Port: 135, Bind: "0.0.0.0", PID: 1040},
		{Port: 445, Bind: "0.0.0.0", PID: 4},
		{Port: 3000, Bind: "127.0.0.1", PID: 12345},
		{Port: 5432, Bind: "127.0.0.1", PID: 8120},
		{Port: 135, Bind: "::", PID: 1040},
		{Port: 9000, Bind: "::1", PID: 555},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseNetstat:\n got %+v\nwant %+v", got, want)
	}
}

func TestParseNetstatIgnoresEstablishedAndTimeWait(t *testing.T) {
	out := []byte("  TCP    127.0.0.1:3000         127.0.0.1:55555        ESTABLISHED     12345\n" +
		"  TCP    192.168.1.20:51234     104.16.0.1:443         TIME_WAIT       0\n")
	if got := parseNetstat(out); len(got) != 0 {
		t.Fatalf("want no listeners, got %+v", got)
	}
}

func TestParseNetstatIsLocaleIndependent(t *testing.T) {
	// German Windows localises the state word; the foreign endpoint 0.0.0.0:0 still marks a listener.
	out := []byte("  TCP    0.0.0.0:8080           0.0.0.0:0              ABHOEREN        2222\n")
	got := parseNetstat(out)
	if len(got) != 1 || got[0].Port != 8080 || got[0].PID != 2222 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseNetstatSkipsMalformedRows(t *testing.T) {
	out := []byte("TCP 0.0.0.0:80 0.0.0.0:0 LISTENING notapid\n" +
		"TCP 0.0.0.0:99999 0.0.0.0:0 LISTENING 5\n" +
		"UDP 0.0.0.0:53 *:* 900\n" +
		"TCP 0.0.0.0:81 0.0.0.0:0 LISTENING 6\n")
	got := parseNetstat(out)
	if len(got) != 1 || got[0].Port != 81 || got[0].PID != 6 {
		t.Fatalf("got %+v", got)
	}
}

func TestSplitEndpoint(t *testing.T) {
	tests := []struct {
		in   string
		host string
		port int
		ok   bool
	}{
		{"127.0.0.1:3000", "127.0.0.1", 3000, true},
		{"[::]:445", "::", 445, true},
		{"[fe80::1%12]:9000", "fe80::1%12", 9000, true},
		{"nocolon", "", 0, false},
		{"0.0.0.0:x", "", 0, false},
		{"0.0.0.0:70000", "", 0, false},
	}
	for _, tt := range tests {
		host, port, ok := splitEndpoint(tt.in)
		if host != tt.host || port != tt.port || ok != tt.ok {
			t.Errorf("splitEndpoint(%q) = %q,%d,%v; want %q,%d,%v", tt.in, host, port, ok, tt.host, tt.port, tt.ok)
		}
	}
}

func TestParseTasklistNames(t *testing.T) {
	got := parseTasklistNames(readWinFixture(t, "tasklist_names_win.txt"))
	want := map[int]string{
		0:     "System Idle Process",
		4:     "System",
		1040:  "svchost",
		12345: "node",
		8120:  "postgres",
		555:   "Code, Helper",
		777:   "explorer",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseTasklistNames:\n got %v\nwant %v", got, want)
	}
}

func TestParseTasklistNamesSkipsInfoLine(t *testing.T) {
	got := parseTasklistNames([]byte("INFO: No tasks are running which match the specified criteria.\n"))
	if len(got) != 0 {
		t.Fatalf("want empty map, got %v", got)
	}
}

func TestTrimExe(t *testing.T) {
	for in, want := range map[string]string{"node.exe": "node", "PostgreSQL.EXE": "PostgreSQL", "sshd": "sshd", ".exe": ".exe"} {
		if got := trimExe(in); got != want {
			t.Errorf("trimExe(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestListenersFromNetstatNamesFromTasklist(t *testing.T) {
	eps := parseNetstat(readWinFixture(t, "netstat_ano_win.txt"))
	names := parseTasklistNames(readWinFixture(t, "tasklist_names_win.txt"))
	ls := listenersFromNetstat(eps, names)
	// Names must reach the listeners and survive the shared normalize step.
	got := normalize(ls, Options{IncludeSystem: true})
	var node Listener
	for _, l := range got {
		if l.Port == 3000 {
			node = l
		}
	}
	if node.PID != 12345 || node.Process != "node" || node.Bind != "127.0.0.1" {
		t.Fatalf("port 3000 = %+v", node)
	}
}
