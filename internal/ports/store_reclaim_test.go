package ports

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/rafaelb13/agentport/internal/portsproto"
)

// forwardedBy opens a forward of remote for a new session of agent and returns the
// store snapshot that a restarted Companion would load from disk.
func forwardedBy(t *testing.T, h *harness, agent string, remote int) []Record {
	t.Helper()
	sess := h.register(agent, "/apps/web")
	out := h.requestAsync(sess, batch("api", remote))
	if _, err := h.store.Approve(context.Background(), h.firstPending().ID, Approval{Duration: DurationSession, Ports: []PortDecision{{RemotePort: remote}}}); err != nil {
		t.Fatal(err)
	}
	<-out
	return h.store.Snapshot()
}

// restartCompanion is a Companion restart: a new store with an empty session
// table, the forwards restored from disk as reserved, and the tunnel back up.
func restartCompanion(t *testing.T, h *harness, snap []Record) *harness {
	t.Helper()
	fresh := newHarness(t, nil)
	fresh.now = h.now
	fresh.store.Restore(snap)
	fresh.store.Resume(context.Background())
	return fresh
}

func TestReclaimAfterRestartTakesRestoredForwardsBack(t *testing.T) {
	h := newHarness(t, nil)
	snap := forwardedBy(t, h, "claude-code", 3000)
	fresh := restartCompanion(t, h, snap)
	if fw := fresh.store.Forwards(""); fw[0].State != "session_ended" || len(fw[0].Owners) != 0 {
		t.Fatalf("restored forward = %+v, want session_ended without owners", fw)
	}

	again, err := fresh.store.RegisterSession(portsproto.RegisterSessionRequest{
		V: 1, Agent: "claude-code", PID: 2, Cwd: "/apps/web", Reclaim: []int{3000, 9999},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.Reclaimed, []int{3000}) || !again.Resumed {
		t.Fatalf("register = %+v, want reclaimed [3000] and resumed", again)
	}
	fw := fresh.store.Forwards(again.ID)
	if len(fw) != 1 || fw[0].State != "active" || !fw[0].Mine {
		t.Fatalf("after reclaim = %+v, want one active forward owned by the session", fw)
	}
	if !reflect.DeepEqual(fw[0].Owners, []string{"claude-code"}) {
		t.Fatalf("owners = %v, want exactly one claude-code owner", fw[0].Owners)
	}
}

func TestReclaimTwiceDoesNotDuplicateOwners(t *testing.T) {
	h := newHarness(t, nil)
	fresh := restartCompanion(t, h, forwardedBy(t, h, "claude-code", 3000))
	req := portsproto.RegisterSessionRequest{V: 1, Agent: "claude-code", PID: 2, Cwd: "/apps/web", Reclaim: []int{3000}}
	first, err := fresh.store.RegisterSession(req)
	if err != nil {
		t.Fatal(err)
	}
	// A re-registration while the first session still lives (the heartbeat failed
	// on the client side) names it as Previous: the client must not count twice.
	req.Previous = first.ID
	second, err := fresh.store.RegisterSession(req)
	if err != nil {
		t.Fatal(err)
	}
	fw := fresh.store.Forwards(second.ID)
	if len(fw) != 1 || len(fw[0].Owners) != 1 || !fw[0].Mine {
		t.Fatalf("forwards = %+v, want one forward with one owner, the new session", fw)
	}
	if first.ID == second.ID {
		t.Fatal("two registrations got the same session ID")
	}
	if err := fresh.store.Heartbeat(first.ID); err == nil {
		t.Fatal("the previous session is still alive")
	}
}

func TestPreviousSessionOfAnotherFolderIsNotEnded(t *testing.T) {
	h := newHarness(t, nil)
	other := h.register("claude-code", "/apps/other")
	_, err := h.store.RegisterSession(portsproto.RegisterSessionRequest{
		V: 1, Agent: "claude-code", PID: 2, Cwd: "/apps/web", Previous: other,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.Heartbeat(other); err != nil {
		t.Fatalf("a session of another folder was ended: %v", err)
	}
}

func TestReclaimIgnoresPortsThatAreNoLongerForwardedOrAreBlocked(t *testing.T) {
	h := newHarness(t, nil)
	fresh := restartCompanion(t, h, forwardedBy(t, h, "claude-code", 3000))
	// The user closed 3000 after the restart; the agent still remembers it.
	if err := fresh.store.CloseByUser(3000); err != nil {
		t.Fatal(err)
	}
	resp, err := fresh.store.RegisterSession(portsproto.RegisterSessionRequest{
		V: 1, Agent: "claude-code", PID: 2, Cwd: "/apps/web", Reclaim: []int{3000, 22},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Reclaimed) != 0 {
		t.Fatalf("reclaimed %v; a closed or blocked port must not come back", resp.Reclaimed)
	}
	if fw := fresh.store.Forwards(""); len(fw) != 0 {
		t.Fatalf("forwards = %+v, want none", fw)
	}
}

func TestReclaimWithinGraceSurvivesTheSweep(t *testing.T) {
	h := newHarness(t, nil)
	fresh := restartCompanion(t, h, forwardedBy(t, h, "claude-code", 3000))
	fresh.advance(19 * time.Minute)
	resp, err := fresh.store.RegisterSession(portsproto.RegisterSessionRequest{
		V: 1, Agent: "claude-code", PID: 2, Cwd: "/apps/web", Reclaim: []int{3000},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The agent keeps its session alive while the grace period runs out.
	for i := 0; i < 10; i++ {
		fresh.advance(30 * time.Second)
		if err := fresh.store.Heartbeat(resp.ID); err != nil {
			t.Fatalf("heartbeat %d: %v", i, err)
		}
	}
	fresh.store.Sweep()
	if !fresh.fwd.isOpen(3000) {
		t.Fatal("reclaimed forward closed after the grace period had passed from its restore")
	}
	fw := fresh.store.Forwards(resp.ID)
	if len(fw) != 1 || fw[0].State != "active" {
		t.Fatalf("forwards = %+v, want the reclaimed forward active", fw)
	}
}

func TestUnclaimedRestoredForwardStillExpiresAfterGrace(t *testing.T) {
	h := newHarness(t, nil)
	fresh := restartCompanion(t, h, forwardedBy(t, h, "claude-code", 3000))
	fresh.advance(21 * time.Minute)
	fresh.store.Sweep()
	if fresh.fwd.isOpen(3000) {
		t.Fatal("a restored forward nobody reclaimed stayed open past the grace period")
	}
}

func TestReclaimJoinsAForwardThatAnotherLiveSessionOwns(t *testing.T) {
	h := newHarness(t, nil)
	other := h.register("codex", "/apps/api")
	out := h.requestAsync(other, batch("api", 3000))
	if _, err := h.store.Approve(context.Background(), h.firstPending().ID, Approval{Duration: DurationSession, Ports: []PortDecision{{RemotePort: 3000}}}); err != nil {
		t.Fatal(err)
	}
	<-out
	resp, err := h.store.RegisterSession(portsproto.RegisterSessionRequest{
		V: 1, Agent: "claude-code", PID: 2, Cwd: "/apps/web", Reclaim: []int{3000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resp.Reclaimed, []int{3000}) {
		t.Fatalf("reclaimed = %v, want [3000]", resp.Reclaimed)
	}
	fw := h.store.Forwards(resp.ID)
	if len(fw) != 1 || !fw[0].Mine || len(fw[0].Owners) != 2 {
		t.Fatalf("forwards = %+v, want both sessions as owners", fw)
	}
}

func TestForwardsOfUnknownSessionIsRejectedSoItRegistersAgain(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.store.ForwardsOf("zzz"); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("list for an unknown session = %v, want ErrUnknownSession", err)
	}
	sess := h.register("claude-code", "/apps/web")
	if _, err := h.store.ForwardsOf(sess); err != nil {
		t.Fatalf("list for a registered session: %v", err)
	}
	h.store.UnregisterSession(sess)
	if _, err := h.store.ForwardsOf(sess); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("list for an ended session = %v, want ErrUnknownSession", err)
	}
}
