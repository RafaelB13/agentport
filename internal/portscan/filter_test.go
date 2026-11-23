package portscan

import (
	"reflect"
	"testing"
)

func TestNormalizeFilters(t *testing.T) {
	all := []Listener{
		{Port: 22, Bind: "0.0.0.0", Process: "sshd"},
		{Port: 80, Bind: "0.0.0.0", Process: "nginx"},
		{Port: 3000, Bind: "127.0.0.1", Process: "node"},
		{Port: 5353, Bind: "0.0.0.0", Process: "avahi-daemon"},
		{Port: 5355, Bind: "0.0.0.0", Process: "systemd-resolve"}, // comm is truncated to 15 bytes
		{Port: 6000, Bind: "0.0.0.0", Process: "bridge-port"},
		{Port: 8080, Bind: "0.0.0.0", Process: "docker-proxy"},
		{Port: 631, Bind: "127.0.0.1", Process: "cupsd"},
		{Port: 9000, Bind: "0.0.0.0", Process: "systemd-timesyncd-x"}, // not a listed daemon
	}
	tests := []struct {
		name string
		opt  Options
		want []int
	}{
		{
			name: "defaults drop sshd, privileged and system daemons",
			opt:  Options{},
			want: []int{3000, 6000, 8080, 9000},
		},
		{
			name: "IncludePrivileged keeps ports below 1024",
			opt:  Options{IncludePrivileged: true},
			want: []int{80, 3000, 6000, 8080, 9000}, // cupsd (631) is still a system daemon
		},
		{
			name: "IncludeSystem keeps daemons",
			opt:  Options{IncludeSystem: true},
			want: []int{3000, 5353, 5355, 6000, 8080, 9000},
		},
		{
			name: "ExcludePorts drops the bridge port",
			opt:  Options{ExcludePorts: []int{6000, 3000}},
			want: []int{8080, 9000},
		},
		{
			name: "everything included",
			opt:  Options{IncludePrivileged: true, IncludeSystem: true},
			want: []int{80, 631, 3000, 5353, 5355, 6000, 8080, 9000},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalize(all, tt.opt)
			ports := make([]int, 0, len(got))
			for _, l := range got {
				ports = append(ports, l.Port)
			}
			if !reflect.DeepEqual(ports, tt.want) {
				t.Errorf("ports = %v, want %v", ports, tt.want)
			}
		})
	}
}

func TestNormalizeKeepsDockerProxy(t *testing.T) {
	got := normalize([]Listener{{Port: 8080, Bind: "0.0.0.0", Process: "docker-proxy"}}, Options{})
	if len(got) != 1 {
		t.Fatalf("docker-proxy dropped: %+v", got)
	}
}

func TestNormalizeDedupPrefersSpecificBind(t *testing.T) {
	tests := []struct {
		name string
		in   []Listener
		want Listener
	}{
		{
			name: "loopback beats wildcard on the same port",
			in: []Listener{
				{Port: 3000, Bind: "::", PID: 10, Process: "node"},
				{Port: 3000, Bind: "127.0.0.1", PID: 10, Process: "node"},
			},
			want: Listener{Port: 3000, Bind: "127.0.0.1", PID: 10, Process: "node"},
		},
		{
			name: "known pid beats unknown pid on equal bind",
			in: []Listener{
				{Port: 5173, Bind: "0.0.0.0"},
				{Port: 5173, Bind: "0.0.0.0", PID: 7, Process: "vite"},
			},
			want: Listener{Port: 5173, Bind: "0.0.0.0", PID: 7, Process: "vite"},
		},
		{
			name: "ipv4 beats ipv6 when otherwise equal",
			in: []Listener{
				{Port: 5173, Bind: "::", PID: 7, Process: "vite"},
				{Port: 5173, Bind: "0.0.0.0", PID: 7, Process: "vite"},
			},
			want: Listener{Port: 5173, Bind: "0.0.0.0", PID: 7, Process: "vite"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalize(tt.in, Options{})
			if len(got) != 1 {
				t.Fatalf("got %d entries, want 1: %+v", len(got), got)
			}
			if got[0] != tt.want {
				t.Errorf("got %+v, want %+v", got[0], tt.want)
			}
		})
	}
}

func TestNormalizeSortsByPortThenBind(t *testing.T) {
	in := []Listener{
		{Port: 9000, Bind: "::1"},
		{Port: 3000, Bind: "127.0.0.1"},
		{Port: 9000, Bind: "0.0.0.0"},
		{Port: 1500, Bind: "0.0.0.0"},
	}
	got := normalize(in, Options{IncludePrivileged: true})
	want := []Listener{
		{Port: 1500, Bind: "0.0.0.0"},
		{Port: 3000, Bind: "127.0.0.1"},
		{Port: 9000, Bind: "::1"}, // a specific bind beats the 0.0.0.0 wildcard
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if in[0].Port != 9000 || in[0].Bind != "::1" {
		t.Error("normalize modified its input")
	}
}
