//go:build e2e

package e2e

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"

	"github.com/rafaelb13/agentport/internal/forward"
	"github.com/rafaelb13/agentport/internal/hostport"
)

// TestConflictLocalPort: uma porta local ocupada por este processo é pulada pelo
// AllocLocal, o forward cai na próxima livre e hostport.Lookup aponta este pid.
func TestConflictLocalPort(t *testing.T) {
	f := fixtureFor(t)
	fw := f.forwarder()
	ctx := context.Background()

	busy, err := forward.AllocLocal(3000, nil)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(busy)))
	if err != nil {
		t.Fatalf("ocupar %d: %v", busy, err)
	}
	defer ln.Close()

	local, err := forward.AllocLocal(busy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if local == busy {
		t.Fatalf("AllocLocal devolveu a porta ocupada %d", busy)
	}
	if err := fw.Add(ctx, 3000, local); err != nil {
		t.Fatalf("Add 3000 -> %d: %v", local, err)
	}
	t.Cleanup(func() { _ = fw.Remove(ctx, 3000, local) })
	expectBody(t, local, "api-3000")

	owner, err := hostport.Lookup(ctx, busy)
	if err != nil {
		t.Fatalf("Lookup %d: %v", busy, err)
	}
	if owner == nil {
		t.Fatalf("Lookup %d = nil; a porta está ocupada por este teste", busy)
	}
	if owner.Kind != hostport.KindProc {
		t.Errorf("Kind = %q; quero %q", owner.Kind, hostport.KindProc)
	}
	if owner.PID != os.Getpid() {
		t.Errorf("PID = %d; quero o pid deste processo %d", owner.PID, os.Getpid())
	}
}
