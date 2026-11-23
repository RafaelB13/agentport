package forward

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestControlMasterAddRemoveArgv(t *testing.T) {
	f := &fakeSSH{}
	c := NewControlMaster("/usr/bin/ssh", "/tmp/cm-dev.sock", "dev", f.run)
	ctx := context.Background()

	if err := c.Add(ctx, 3000, 13000); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(ctx, 3000, 13000); err != nil {
		t.Fatal(err)
	}

	wantAdd := call{"/usr/bin/ssh", []string{"-S", "/tmp/cm-dev.sock", "-O", "forward", "-L", "127.0.0.1:13000:127.0.0.1:3000", "dev"}}
	wantCancel := call{"/usr/bin/ssh", []string{"-S", "/tmp/cm-dev.sock", "-O", "cancel", "-L", "127.0.0.1:13000:127.0.0.1:3000", "dev"}}
	if len(f.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(f.calls))
	}
	if f.calls[0].name != wantAdd.name || !slices.Equal(f.calls[0].args, wantAdd.args) {
		t.Errorf("add call = %v %v, want %v %v", f.calls[0].name, f.calls[0].args, wantAdd.name, wantAdd.args)
	}
	if f.calls[1].name != wantCancel.name || !slices.Equal(f.calls[1].args, wantCancel.args) {
		t.Errorf("cancel call = %v %v, want %v %v", f.calls[1].name, f.calls[1].args, wantCancel.name, wantCancel.args)
	}
}

func TestControlMasterListSortedAndReapply(t *testing.T) {
	f := &fakeSSH{}
	c := NewControlMaster("ssh", "/cp", "dev", f.run)
	ctx := context.Background()
	if err := c.Add(ctx, 8080, 18080); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(ctx, 3000, 13000); err != nil {
		t.Fatal(err)
	}

	want := []Forward{{Remote: 3000, Local: 13000}, {Remote: 8080, Local: 18080}}
	if got := c.List(); !slices.Equal(got, want) {
		t.Fatalf("List = %v, want %v", got, want)
	}

	f.calls = nil
	if err := c.Reapply(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 {
		t.Fatalf("reapply calls = %d, want 2", len(f.calls))
	}
	if got := f.calls[0].args[5]; got != "127.0.0.1:13000:127.0.0.1:3000" {
		t.Errorf("first reapply spec = %q, want the 3000 forward first", got)
	}
	if got := f.calls[1].args[3]; got != "forward" {
		t.Errorf("reapply op = %q, want forward", got)
	}
}

func TestControlMasterFailedAddIsNotListed(t *testing.T) {
	f := &fakeSSH{failOps: map[string]bool{"forward": true}}
	c := NewControlMaster("ssh", "/cp", "dev", f.run)
	if err := c.Add(context.Background(), 3000, 13000); err == nil {
		t.Fatal("Add succeeded, want error from ssh")
	}
	if got := c.List(); len(got) != 0 {
		t.Fatalf("List = %v, want empty after failed add", got)
	}
}

func TestControlMasterFailedCancelKeepsForward(t *testing.T) {
	f := &fakeSSH{}
	c := NewControlMaster("ssh", "/cp", "dev", f.run)
	ctx := context.Background()
	if err := c.Add(ctx, 3000, 13000); err != nil {
		t.Fatal(err)
	}
	f.failOps = map[string]bool{"cancel": true}
	err := c.Remove(ctx, 3000, 13000)
	if err == nil {
		t.Fatal("Remove succeeded, want error from ssh")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, must not be ErrNotFound", err)
	}
	if got := c.List(); len(got) != 1 {
		t.Fatalf("List = %v, want the forward kept so Remove can be retried", got)
	}
}

func TestControlMasterReapplyKeepsGoingAfterError(t *testing.T) {
	f := &fakeSSH{}
	c := NewControlMaster("ssh", "/cp", "dev", f.run)
	ctx := context.Background()
	_ = c.Add(ctx, 3000, 13000)
	_ = c.Add(ctx, 8080, 18080)
	f.failOps = map[string]bool{"forward": true}
	if err := c.Reapply(ctx); err == nil {
		t.Fatal("Reapply returned nil, want joined error")
	}
	if len(f.calls) != 4 { // 2 adds + 2 reapplies, the failing one did not stop the loop
		t.Fatalf("calls = %d, want 4", len(f.calls))
	}
	if got := c.List(); len(got) != 2 {
		t.Fatalf("List = %v, want both forwards kept", got)
	}
}
