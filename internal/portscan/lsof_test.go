package portscan

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParseLsofF(t *testing.T) {
	f, err := os.Open("testdata/lsof_listen.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := parseLsofF(f)
	if err != nil {
		t.Fatalf("parseLsofF: %v", err)
	}
	want := []Listener{
		{Port: 3000, Bind: "127.0.0.1", PID: 4321, Process: "node", User: "rafael"},
		{Port: 5173, Bind: "0.0.0.0", PID: 4321, Process: "node", User: "rafael"},
		{Port: 5173, Bind: "::", PID: 4321, Process: "node", User: "rafael"},
		{Port: 9000, Bind: "::1", PID: 4321, Process: "node", User: "rafael"},
		{Port: 8080, Bind: "0.0.0.0", PID: 4321, Process: "node", User: "rafael"},
		{Port: 5432, Bind: "::1", PID: 612, Process: "postgres", User: "postgres"},
		{Port: 5432, Bind: "127.0.0.1", PID: 612, Process: "postgres", User: "postgres"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got:\n%+v\nwant:\n%+v", got, want)
	}
}

func TestParseLsofFFallsBackToUID(t *testing.T) {
	input := "p77\ncnginx\nu33\nf6\ntIPv4\nn*:80\n"
	got, err := parseLsofF(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	want := []Listener{{Port: 80, Bind: "0.0.0.0", PID: 77, Process: "nginx", User: "33"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestSplitLsofName(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantHost string
		wantPort int
		wantOK   bool
	}{
		{name: "ipv4", in: "127.0.0.1:3000", wantHost: "127.0.0.1", wantPort: 3000, wantOK: true},
		{name: "wildcard", in: "*:5173", wantHost: "*", wantPort: 5173, wantOK: true},
		{name: "bracketed ipv6", in: "[::1]:9000", wantHost: "::1", wantPort: 9000, wantOK: true},
		{name: "listen suffix", in: "*:8080 (LISTEN)", wantHost: "*", wantPort: 8080, wantOK: true},
		{name: "no port", in: "127.0.0.1", wantOK: false},
		{name: "bad port", in: "*:http", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port, ok := splitLsofName(tt.in)
			if ok != tt.wantOK || host != tt.wantHost || port != tt.wantPort {
				t.Errorf("splitLsofName(%q) = (%q, %d, %v), want (%q, %d, %v)",
					tt.in, host, port, ok, tt.wantHost, tt.wantPort, tt.wantOK)
			}
		})
	}
}

func TestParseLsofCwd(t *testing.T) {
	f, err := os.Open("testdata/lsof_cwd.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := parseLsofCwd(f)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]string{4321: "/Users/rafael/app", 612: "/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
