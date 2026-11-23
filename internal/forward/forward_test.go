package forward

import (
	"context"
	"errors"
	"net"
	"slices"
	"testing"
)

type call struct {
	name string
	args []string
}

// fakeSSH records every command and fails the ops listed in failOps.
type fakeSSH struct {
	calls   []call
	failOps map[string]bool
}

func (f *fakeSSH) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, call{name: name, args: append([]string(nil), args...)})
	// args: -S <cp> -O <op> -L <spec> <host>
	if len(args) > 3 && f.failOps[args[3]] {
		return []byte("mux_client: boom\n"), errors.New("exit status 255")
	}
	return nil, nil
}

func TestValidPort(t *testing.T) {
	for _, tc := range []struct {
		port int
		ok   bool
	}{{0, false}, {1, true}, {65535, true}, {65536, false}, {-1, false}} {
		err := validPort(tc.port)
		if (err == nil) != tc.ok {
			t.Errorf("validPort(%d) = %v, want ok=%v", tc.port, err, tc.ok)
		}
		if err != nil && !errors.Is(err, ErrInvalidPort) {
			t.Errorf("validPort(%d) error %v is not ErrInvalidPort", tc.port, err)
		}
	}
}

func TestSpecPinsLoopback(t *testing.T) {
	if got, want := spec(13000, 3000), "127.0.0.1:13000:127.0.0.1:3000"; got != want {
		t.Fatalf("spec = %q, want %q", got, want)
	}
}

func TestAddRejectsInvalidPortsWithoutSSH(t *testing.T) {
	f := &fakeSSH{}
	c := NewControlMaster("ssh", "/cp", "dev", f.run)
	ctx := context.Background()
	if err := c.Add(ctx, 0, 13000); !errors.Is(err, ErrInvalidPort) {
		t.Errorf("remote 0: err = %v, want ErrInvalidPort", err)
	}
	if err := c.Add(ctx, 3000, 65536); !errors.Is(err, ErrInvalidPort) {
		t.Errorf("local 65536: err = %v, want ErrInvalidPort", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("ssh called %d times for invalid ports", len(f.calls))
	}
}

func TestAddRejectsDuplicateLocalPort(t *testing.T) {
	f := &fakeSSH{}
	c := NewControlMaster("ssh", "/cp", "dev", f.run)
	ctx := context.Background()
	if err := c.Add(ctx, 3000, 13000); err != nil {
		t.Fatal(err)
	}
	err := c.Add(ctx, 4000, 13000) // different remote, same local
	if !errors.Is(err, ErrLocalInUse) {
		t.Fatalf("err = %v, want ErrLocalInUse", err)
	}
	if len(f.calls) != 1 {
		t.Errorf("ssh called %d times, want 1 (duplicate must not reach ssh)", len(f.calls))
	}
}

func TestRemoveUnknownIsNotFound(t *testing.T) {
	f := &fakeSSH{}
	c := NewControlMaster("ssh", "/cp", "dev", f.run)
	if err := c.Remove(context.Background(), 3000, 13000); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAllocLocalSkipsBusy(t *testing.T) {
	busy := func(p int) bool { return p < 5002 }
	got, err := AllocLocal(5000, busy)
	if err != nil || got != 5002 {
		t.Fatalf("AllocLocal = %d, %v; want 5002", got, err)
	}
}

func TestAllocLocalInvalidWant(t *testing.T) {
	if _, err := AllocLocal(0, func(int) bool { return false }); !errors.Is(err, ErrInvalidPort) {
		t.Fatalf("err = %v, want ErrInvalidPort", err)
	}
}

func TestAllocLocalNoneFree(t *testing.T) {
	_, err := AllocLocal(65535, func(int) bool { return true })
	if !errors.Is(err, ErrNoFreePort) {
		t.Fatalf("err = %v, want ErrNoFreePort", err)
	}
}

func TestAllocLocalRealListenerIsSkipped(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	if CanListen(port) {
		t.Fatalf("CanListen(%d) = true while another listener holds it", port)
	}
	got, err := AllocLocal(port, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got == port || got < port {
		t.Fatalf("AllocLocal(%d) = %d; must skip the occupied port", port, got)
	}
}

func TestSuggest(t *testing.T) {
	busy := func(p int) bool { return p == 5000 || p == 5001 }
	got := Suggest(5000, 3, busy)
	want := []int{5002, 5003, 5004, 15000}
	if !slices.Equal(got, want) {
		t.Fatalf("Suggest = %v, want %v", got, want)
	}
}

func TestSuggestSkipsBusyOffsetAndOverflow(t *testing.T) {
	never := func(int) bool { return false }
	if got := Suggest(5000, 1, func(p int) bool { return p == 15000 }); !slices.Equal(got, []int{5000}) {
		t.Errorf("busy offset: Suggest = %v, want [5000]", got)
	}
	// want+10000 overflows the port range, so it is not offered.
	if got := Suggest(56000, 1, never); !slices.Equal(got, []int{56000}) {
		t.Errorf("overflow: Suggest = %v, want [56000]", got)
	}
	if got := Suggest(0, 2, never); got != nil {
		t.Errorf("invalid want: Suggest = %v, want nil", got)
	}
}
