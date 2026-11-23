package portsproto

import "testing"

func TestRegisterValidatesReclaimList(t *testing.T) {
	base := RegisterSessionRequest{V: Version, Agent: "claude-code"}

	ok := base
	ok.Reclaim = []int{3000, 5173}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid reclaim rejected: %v", err)
	}

	badPort := base
	badPort.Reclaim = []int{3000, 70000}
	if err := badPort.Validate(); err == nil {
		t.Fatal("reclaim with port 70000 accepted")
	}

	tooMany := base
	for i := 0; i <= MaxReclaimPorts; i++ {
		tooMany.Reclaim = append(tooMany.Reclaim, 1000+i%100)
	}
	if err := tooMany.Validate(); err == nil {
		t.Fatalf("reclaim of %d ports accepted", len(tooMany.Reclaim))
	}
}
