package forward

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type fakeProc struct{ killed int }

func (p *fakeProc) Kill() error { p.killed++; return nil }

type fakeStarter struct {
	started []call
	procs   []*fakeProc
	fail    bool
}

func (s *fakeStarter) Start(name string, args ...string) (Process, error) {
	s.started = append(s.started, call{name: name, args: append([]string(nil), args...)})
	if s.fail {
		return nil, errors.New("no such binary")
	}
	p := &fakeProc{}
	s.procs = append(s.procs, p)
	return p, nil
}

func TestPerProcessAddStartsSSHAndRemoveKills(t *testing.T) {
	s := &fakeStarter{}
	p := NewPerProcess("/usr/bin/ssh", "dev", s)
	ctx := context.Background()

	if err := p.Add(ctx, 3000, 13000); err != nil {
		t.Fatal(err)
	}
	want := []string{"-N", "-L", "127.0.0.1:13000:127.0.0.1:3000", "dev"}
	if len(s.started) != 1 || s.started[0].name != "/usr/bin/ssh" || !slices.Equal(s.started[0].args, want) {
		t.Fatalf("started = %+v, want ssh %v", s.started, want)
	}

	if err := p.Remove(ctx, 3000, 13000); err != nil {
		t.Fatal(err)
	}
	if s.procs[0].killed != 1 {
		t.Errorf("killed = %d, want 1", s.procs[0].killed)
	}
	if got := p.List(); len(got) != 0 {
		t.Errorf("List = %v, want empty", got)
	}
}

func TestPerProcessReapplyRestartsEveryForward(t *testing.T) {
	s := &fakeStarter{}
	p := NewPerProcess("ssh", "dev", s)
	ctx := context.Background()
	_ = p.Add(ctx, 8080, 18080)
	_ = p.Add(ctx, 3000, 13000)

	if err := p.Reapply(ctx); err != nil {
		t.Fatal(err)
	}
	// Two original starts plus two restarts, sorted by Remote (3000 first).
	if len(s.started) != 4 {
		t.Fatalf("starts = %d, want 4", len(s.started))
	}
	if got := s.started[2].args[2]; got != "127.0.0.1:13000:127.0.0.1:3000" {
		t.Errorf("first restart spec = %q, want the 3000 forward", got)
	}
	if s.procs[0].killed != 1 || s.procs[1].killed != 1 {
		t.Errorf("old processes killed = %d,%d, want 1,1", s.procs[0].killed, s.procs[1].killed)
	}
	if got := p.List(); len(got) != 2 {
		t.Errorf("List = %v, want both forwards kept", got)
	}
}

func TestPerProcessStartFailureIsNotListed(t *testing.T) {
	s := &fakeStarter{fail: true}
	p := NewPerProcess("ssh", "dev", s)
	if err := p.Add(context.Background(), 3000, 13000); err == nil {
		t.Fatal("Add succeeded, want start error")
	}
	if got := p.List(); len(got) != 0 {
		t.Fatalf("List = %v, want empty", got)
	}
}

func TestPerProcessRemoveUnknownIsNotFound(t *testing.T) {
	p := NewPerProcess("ssh", "dev", &fakeStarter{})
	if err := p.Remove(context.Background(), 3000, 13000); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
