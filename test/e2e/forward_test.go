//go:build e2e

package e2e

import (
	"context"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/rafaelb13/agentport/internal/forward"
)

// TestForwardsRealSSH: Add/List/Remove e Reapply contra um sshd real, pelo
// ControlMaster do fixture.
func TestForwardsRealSSH(t *testing.T) {
	f := fixtureFor(t)
	fw := f.forwarder()
	ctx := context.Background()
	t.Cleanup(func() {
		for _, x := range fw.List() {
			_ = fw.Remove(ctx, x.Remote, x.Local)
		}
	})

	api, err := forward.AllocLocal(3000, nil)
	if err != nil {
		t.Fatal(err)
	}
	web, err := forward.AllocLocal(5173, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := fw.Add(ctx, 3000, api); err != nil {
		t.Fatalf("Add 3000: %v", err)
	}
	if err := fw.Add(ctx, 5173, web); err != nil {
		t.Fatalf("Add 5173: %v", err)
	}

	expectBody(t, api, "api-3000")
	expectBody(t, web, "web-5173")

	// Com o forward já listado, reabrir a mesma porta local é recusado sem tocar no ssh.
	if err := fw.Add(ctx, 3000, api); err == nil {
		t.Errorf("Add duplicado em %d deveria falhar", api)
	}

	want := []forward.Forward{{Remote: 3000, Local: api}, {Remote: 5173, Local: web}}
	if got := fw.List(); !reflect.DeepEqual(got, sortedByRemote(want)) {
		t.Errorf("List = %+v; quero %+v", got, sortedByRemote(want))
	}

	t.Run("segurança: listener só em 127.0.0.1", func(t *testing.T) {
		for _, x := range fw.List() {
			names, err := listenerNames(x.Local)
			if err != nil {
				t.Fatalf("lsof %d: %v", x.Local, err)
			}
			if len(names) == 0 {
				t.Errorf("forward %d->%d sem listener", x.Remote, x.Local)
			}
			for _, n := range names {
				if n != "127.0.0.1:"+strconv.Itoa(x.Local) {
					t.Errorf("forward %d->%d escuta em %q; quero só 127.0.0.1", x.Remote, x.Local, n)
				}
			}
		}
	})

	if err := fw.Remove(ctx, 3000, api); err != nil {
		t.Fatalf("Remove 3000: %v", err)
	}
	waitClosed(t, api)
	if got := fw.List(); len(got) != 1 || got[0].Local != web {
		t.Errorf("após Remove, List = %+v; quero só o forward de 5173", got)
	}

	// Reapply: derruba o ControlMaster (os forwards caem), sobe outro e reaplica.
	f.stopMaster()
	waitClosed(t, web)
	if err := f.startMaster(); err != nil {
		t.Fatalf("recriar ControlMaster: %v", err)
	}
	if err := fw.Reapply(ctx); err != nil {
		t.Fatalf("Reapply: %v", err)
	}
	expectBody(t, web, "web-5173")
}

func expectBody(t *testing.T, port int, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var body string
	var err error
	for time.Now().Before(deadline) {
		if body, err = httpGet(port); err == nil && body == want {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("GET 127.0.0.1:%d = %q (%v); quero %q", port, body, err, want)
}

// waitClosed espera a porta local parar de aceitar conexões.
func waitClosed(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := httpGet(port); err != nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("porta %d continua respondendo", port)
}

func sortedByRemote(xs []forward.Forward) []forward.Forward {
	out := append([]forward.Forward(nil), xs...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Remote != out[j].Remote {
			return out[i].Remote < out[j].Remote
		}
		return out[i].Local < out[j].Local
	})
	return out
}
