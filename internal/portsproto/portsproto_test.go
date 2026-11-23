package portsproto

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestForwardRequestValidation(t *testing.T) {
	ok := ForwardRequest{V: Version, Reason: "dev server", Ports: []PortRequest{{RemotePort: 3000, Label: "web", Path: "/app"}}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	long := strings.Repeat("x", MaxReasonLength+1)
	tooManyPorts := make([]PortRequest, MaxPortsPerBatch+1)
	for i := range tooManyPorts {
		tooManyPorts[i] = PortRequest{RemotePort: i + 1}
	}
	cases := map[string]ForwardRequest{
		"wrong version":  {V: 2, Reason: "r", Ports: ok.Ports},
		"empty reason":   {V: Version, Reason: "  ", Ports: ok.Ports},
		"long reason":    {V: Version, Reason: long, Ports: ok.Ports},
		"no ports":       {V: Version, Reason: "r"},
		"too many ports": {V: Version, Reason: "r", Ports: tooManyPorts},
		"port zero":      {V: Version, Reason: "r", Ports: []PortRequest{{RemotePort: 0}}},
		"port too high":  {V: Version, Reason: "r", Ports: []PortRequest{{RemotePort: 65536}}},
		"duplicate port": {V: Version, Reason: "r", Ports: []PortRequest{{RemotePort: 80}, {RemotePort: 80}}},
		"long label":     {V: Version, Reason: "r", Ports: []PortRequest{{RemotePort: 80, Label: strings.Repeat("l", 65)}}},
		"path no slash":  {V: Version, Reason: "r", Ports: []PortRequest{{RemotePort: 80, Path: "app"}}},
		"path too long":  {V: Version, Reason: "r", Ports: []PortRequest{{RemotePort: 80, Path: "/" + strings.Repeat("a", 256)}}},
		"group too long": {V: Version, Reason: "r", Group: strings.Repeat("g", 65), Ports: ok.Ports},
	}
	for name, req := range cases {
		if err := req.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestUnforwardValidation(t *testing.T) {
	if err := (UnforwardRequest{V: Version, All: true}).Validate(); err != nil {
		t.Fatalf("all: %v", err)
	}
	if err := (UnforwardRequest{V: Version, Ports: []int{3000}}).Validate(); err != nil {
		t.Fatalf("ports: %v", err)
	}
	bad := []UnforwardRequest{
		{V: Version},
		{V: Version, All: true, Ports: []int{1}},
		{V: Version, Ports: []int{0}},
		{V: 9, All: true},
	}
	for i, req := range bad {
		if err := req.Validate(); err == nil {
			t.Errorf("case %d: expected an error", i)
		}
	}
}

func TestRegisterValidation(t *testing.T) {
	if err := (RegisterSessionRequest{V: Version, Agent: "claude-code", PID: 10, Cwd: "/x"}).Validate(); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if err := (RegisterSessionRequest{V: Version, Agent: " "}).Validate(); err == nil {
		t.Fatal("empty agent accepted")
	}
	if err := (RegisterSessionRequest{V: Version, Agent: "a", PID: -1}).Validate(); err == nil {
		t.Fatal("negative pid accepted")
	}
}

// Unknown JSON fields must not change the meaning of a request: decoding a
// request with extra keys still yields the same validated value.
func TestDecodeIgnoresUnknownFields(t *testing.T) {
	var req ForwardRequest
	if err := json.Unmarshal([]byte(`{"v":1,"reason":"r","ports":[{"remote_port":3000}],"extra":true}`), &req); err != nil {
		t.Fatal(err)
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("decoded request rejected: %v", err)
	}
}

func TestURL(t *testing.T) {
	if got := URL(3001, "/app"); got != "http://localhost:3001/app" {
		t.Fatalf("URL = %q", got)
	}
}
