package remote

import (
	"errors"
	"strings"
	"testing"
)

const ssOutput = `State  Recv-Q Send-Q Local Address:Port Peer Address:Port Process
LISTEN 0      128          0.0.0.0:22        0.0.0.0:*
LISTEN 0      128        127.0.0.1:39123     0.0.0.0:*
LISTEN 0      128             [::]:22           [::]:*
LISTEN 0      4096       [::1]:39125        [::]:*
`

const linuxNetstat = `Active Internet connections (servers and established)
Proto Recv-Q Send-Q Local Address           Foreign Address         State
tcp        0      0 0.0.0.0:22              0.0.0.0:*               LISTEN
tcp        0      0 127.0.0.1:39123         0.0.0.0:*               LISTEN
tcp        0      0 10.0.0.5:22             10.0.0.9:50000          ESTABLISHED
tcp6       0      0 :::8080                 :::*                    LISTEN
`

const macNetstat = `Active Internet connections (including servers)
Proto Recv-Q Send-Q  Local Address          Foreign Address        (state)
tcp4       0      0  127.0.0.1.39123        *.*                    LISTEN
tcp6       0      0  *.22                   *.*                    LISTEN
tcp4       0      0  192.168.0.2.50000      1.2.3.4.443            ESTABLISHED
`

const lsofOutput = `COMMAND   PID USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
sshd      812 root    3u  IPv4 0x1234      0t0  TCP *:22 (LISTEN)
sshd     9001 wendell 9u  IPv4 0xabcd      0t0  TCP 127.0.0.1:39123 (LISTEN)
node      100 wendell 5u  IPv6 0x9999      0t0  TCP [::1]:3000 (LISTEN)
`

func TestParseListeningPorts(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []int
		known  bool
	}{
		{"ss", ssOutput, []int{22, 39123, 39125}, true},
		{"linux netstat", linuxNetstat, []int{22, 39123, 8080}, true},
		{"macOS netstat", macNetstat, []int{39123, 22}, true},
		{"lsof", lsofOutput, []int{22, 39123, 3000}, true},
		{"no tool on the server", "agentport-listening=unavailable\n", nil, false},
		{"empty output is unknown, not free", "", nil, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, known := ParseListeningPorts(test.output)
			if known != test.known || len(got) != len(test.want) {
				t.Fatalf("got %v known=%v, want %v known=%v", got, known, test.want, test.known)
			}
			for _, port := range test.want {
				if !got[port] {
					t.Errorf("port %d missing from %v", port, got)
				}
			}
		})
	}
}

func TestPickPort(t *testing.T) {
	busy := func(ports ...int) map[int]bool {
		m := map[int]bool{}
		for _, p := range ports {
			m[p] = true
		}
		return m
	}
	tests := []struct {
		name      string
		listening map[int]bool
		known     bool
		preferred int
		explicit  bool
		avoid     []int
		want      int
		wantErr   string
	}{
		{"free port is kept", busy(22), true, 39123, false, nil, 39123, ""},
		{"busy port moves up to the next free one", busy(39123), true, 39123, false, nil, 39124, ""},
		{"skips every busy port", busy(39123, 39124, 39125), true, 39123, false, nil, 39126, ""},
		{"explicit busy port is refused", busy(39123), true, 39123, true, nil, 0, "remote port 39123 is already in use on the server"},
		{"explicit free port is kept", busy(39123), true, 40000, true, nil, 40000, ""},
		{"unknown server keeps the port", nil, false, 39123, false, nil, 39123, ""},
		{"avoid forces another port even when unknown", nil, false, 39123, false, []int{39123}, 39124, ""},
		{"range exhausted", func() map[int]bool {
			m := map[int]bool{}
			for p := 39123; p < 39123+PortScanSpan; p++ {
				m[p] = true
			}
			return m
		}(), true, 39123, false, nil, 0, "no free remote port"},
		{"scan stops at 65535", busy(65535), true, 65535, false, nil, 0, "no free remote port"},
		{"invalid preferred", nil, false, 0, false, nil, 0, "between 1 and 65535"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := PickPort(test.listening, test.known, test.preferred, test.explicit, test.avoid...)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, test.wantErr)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("got %d, %v, want %d", got, err, test.want)
			}
		})
	}
}

func TestPickPortNoFreePortIsATypedError(t *testing.T) {
	_, err := PickPort(map[int]bool{65535: true}, true, 65535, false)
	if !errors.Is(err, ErrNoFreePort) {
		t.Fatalf("err = %v, want ErrNoFreePort", err)
	}
}

// Our own tunnel from before a re-setup can hold the port for a moment: the
// server is asked again before the profile's port is given up.
func TestChoosePortRechecksAProfilePortBeforeMovingIt(t *testing.T) {
	answers := []map[int]bool{{39123: true}, {39123: true}, {}}
	calls, waits := 0, 0
	query := func() (map[int]bool, bool) {
		answer := answers[min(calls, len(answers)-1)]
		calls++
		return answer, true
	}
	got, err := ChoosePort(query, 39123, false, 3, func() { waits++ })
	if err != nil || got != 39123 || calls != 3 || waits != 2 {
		t.Fatalf("got %d, %v after %d asks and %d waits, want 39123 after 3 asks and 2 waits", got, err, calls, waits)
	}
}

func TestChoosePortMovesWhenStillBusyAfterRechecks(t *testing.T) {
	query := func() (map[int]bool, bool) { return map[int]bool{39123: true}, true }
	got, err := ChoosePort(query, 39123, false, 2, nil)
	if err != nil || got != 39124 {
		t.Fatalf("got %d, %v, want 39124", got, err)
	}
}

func TestChoosePortWithoutRecheckAsksOnce(t *testing.T) {
	calls := 0
	query := func() (map[int]bool, bool) { calls++; return map[int]bool{39123: true}, true }
	if _, err := ChoosePort(query, 39123, true, 0, nil); err == nil || calls != 1 {
		t.Fatalf("err = %v after %d asks, want a refusal after 1", err, calls)
	}
}
