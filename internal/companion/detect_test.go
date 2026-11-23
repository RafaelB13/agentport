package companion

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

const scanOutput = `{"ports":[
 {"port":8080,"bind":"0.0.0.0","pid":42,"process":"python3","cmdline":"python3 -m http.server","user":"dev","cwd":"/srv/site"},
 {"port":39123,"bind":"127.0.0.1","pid":9,"process":"sshd","user":"root"},
 {"port":3000,"bind":"127.0.0.1","pid":41,"process":"node","cmdline":"node server.js","user":"dev","cwd":"/apps/web"}
]}`

// scripted is a Runner that answers with out and counts the calls.
type scripted struct {
	mu    sync.Mutex
	out   string
	err   error
	calls int
	args  []string
}

func (s *scripted) run(_ context.Context, name string, args ...string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.args = append([]string{name}, args...)
	return []byte(s.out), s.err
}

func TestRemoteDetectorParsesAndExcludesTheTunnelPort(t *testing.T) {
	run := &scripted{out: scanOutput}
	detector := &RemoteDetector{Destination: "bastion", ControlPath: "/tmp/cm/abc.sock", ExcludePorts: []int{39123}, Run: run.run}
	listed, err := detector.Detect(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].Port != 3000 || listed[1].Port != 8080 {
		t.Fatalf("listed = %+v, want 3000 and 8080 sorted, without the tunnel port", listed)
	}
	if listed[0].Cwd != "/apps/web" || listed[0].PID != 41 || listed[0].User != "dev" || listed[0].Cmdline != "node server.js" || listed[0].Bind != "127.0.0.1" {
		t.Fatalf("process info lost: %+v", listed[0])
	}
}

func TestRemoteDetectorUsesTheControlMasterWithoutANewConnection(t *testing.T) {
	run := &scripted{out: scanOutput}
	detector := &RemoteDetector{Destination: "bastion", ControlPath: "/tmp/cm/abc.sock", Run: run.run}
	if _, err := detector.Detect(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(run.args, " ")
	for _, want := range []string{"ssh", "-S /tmp/cm/abc.sock", "ControlMaster=no", "bastion", "agentport ports --json"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("ssh arguments %q do not contain %q", joined, want)
		}
	}
}

func TestRemoteDetectorCachesForThreeSecondsAndRefreshes(t *testing.T) {
	run := &scripted{out: scanOutput}
	clock := time.Unix(1000, 0)
	detector := &RemoteDetector{Destination: "bastion", Run: run.run, Now: func() time.Time { return clock }}
	ctx := context.Background()

	if _, err := detector.Detect(ctx, false); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Second)
	if _, err := detector.Detect(ctx, false); err != nil {
		t.Fatal(err)
	}
	if run.calls != 1 {
		t.Fatalf("calls within the TTL = %d, want 1", run.calls)
	}
	if _, err := detector.Detect(ctx, true); err != nil {
		t.Fatal(err)
	}
	if run.calls != 2 {
		t.Fatalf("refresh did not scan: calls = %d", run.calls)
	}
	clock = clock.Add(4 * time.Second)
	if _, err := detector.Detect(ctx, false); err != nil {
		t.Fatal(err)
	}
	if run.calls != 3 {
		t.Fatalf("calls after the TTL = %d, want 3", run.calls)
	}
}

func TestRemoteDetectorDoesNotCacheAFailure(t *testing.T) {
	run := &scripted{err: errors.New("exit status 255")}
	detector := &RemoteDetector{Destination: "bastion", Run: run.run}
	if _, err := detector.Detect(context.Background(), false); err == nil || !strings.Contains(err.Error(), "scan the server") {
		t.Fatalf("err = %v, want the scan error", err)
	}
	run.err, run.out = nil, scanOutput
	listed, err := detector.Detect(context.Background(), false)
	if err != nil || len(listed) != 3 {
		t.Fatalf("after the failure: listed=%d err=%v", len(listed), err)
	}
}

func TestRemoteDetectorRejectsAnUnreadableAnswer(t *testing.T) {
	for _, out := range []string{"not json", `{"other":1}`} {
		if _, err := parseRemotePorts([]byte(out), nil); err == nil {
			t.Fatalf("parseRemotePorts(%q) succeeded", out)
		}
	}
	listed, err := parseRemotePorts([]byte(`{"ports":[]}`), nil)
	if err != nil || len(listed) != 0 {
		t.Fatalf("empty list = %v, %v", listed, err)
	}
}
