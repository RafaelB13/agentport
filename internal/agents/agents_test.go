package agents

import (
	"strings"
	"testing"

	"github.com/rafaelb13/agentport/internal/companion"
)

func TestAgentAdaptersBuildUserScopedMCPCommands(t *testing.T) {
	profile := companion.Profile{Name: "m2", RemotePort: 39123, Token: "pair-token"}
	tests := []struct {
		id             string
		executable     string
		addContains    []string
		removeContains []string
	}{
		{
			id: "codex", executable: "codex",
			addContains:    []string{"codex", "mcp", "add", "agentport-m2", "--", "agentport", "mcp", "AGENTPORT_BRIDGE_PORT=39123", "AGENTPORT_SESSION_TOKEN=pair-token"},
			removeContains: []string{"codex", "mcp", "remove", "agentport-m2"},
		},
		{
			id: "claude", executable: "claude",
			addContains:    []string{"claude", "mcp", "add", "agentport-m2", "--scope", "user", "--", "agentport", "mcp", "AGENTPORT_BRIDGE_PORT=39123", "AGENTPORT_SESSION_TOKEN=pair-token"},
			removeContains: []string{"claude", "mcp", "remove", "--scope", "user", "agentport-m2"},
		},
		{
			id: "gemini", executable: "gemini",
			addContains:    []string{"gemini", "mcp", "add", "agentport-m2", "agentport", "mcp", "--scope", "user", "AGENTPORT_BRIDGE_PORT=39123", "AGENTPORT_SESSION_TOKEN=pair-token"},
			removeContains: []string{"gemini", "mcp", "remove", "--scope", "user", "agentport-m2"},
		},
		{
			id: "agy", executable: "agy",
			addContains:    []string{"agentport", "harness", "install", "agy", "--name", "agentport-m2", "--port", "39123", "--token", "pair-token"},
			removeContains: []string{"agentport", "harness", "remove", "agy", "--name", "agentport-m2"},
		},
		{
			id: "opencode", executable: "opencode",
			addContains:    []string{"agentport", "harness", "install", "opencode", "--name", "agentport-m2", "--port", "39123", "--token", "pair-token"},
			removeContains: []string{"agentport", "harness", "remove", "opencode", "--name", "agentport-m2"},
		},
		{
			id: "pi", executable: "pi",
			addContains:    []string{"agentport", "harness", "install", "pi", "--name", "agentport-m2", "--port", "39123", "--token", "pair-token"},
			removeContains: []string{"agentport", "harness", "remove", "pi", "--name", "agentport-m2"},
		},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			adapter, err := Resolve(test.id)
			if err != nil {
				t.Fatal(err)
			}
			if adapter.Executable != test.executable {
				t.Fatalf("executable = %q, want %q", adapter.Executable, test.executable)
			}
			assertArgumentsContain(t, adapter.AddArguments(profile, "agentport-m2"), test.addContains)
			assertArgumentsContain(t, adapter.RemoveArguments("agentport-m2"), test.removeContains)
		})
	}
}

func TestResolveAgentAdapterRejectsUnknownAgent(t *testing.T) {
	if _, err := Resolve("aider"); err == nil {
		t.Fatal("expected unsupported agent error")
	}
}

func TestDisplayAgents(t *testing.T) {
	codex, err := Resolve("codex")
	if err != nil {
		t.Fatal(err)
	}
	gemini, err := Resolve("gemini")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Display([]Adapter{codex, gemini}), "Codex, Gemini CLI"; got != want {
		t.Fatalf("Display() = %q, want %q", got, want)
	}
}

func TestAgentAdaptersPropagateSessionToken(t *testing.T) {
	profile := companion.Profile{Name: "m2", Destination: "host", RemotePort: 39123, Token: "session-token"}
	arguments := agentEnvironmentArguments([]string{"codex", "mcp", "add"}, profile)
	if !strings.Contains(strings.Join(arguments, " "), "AGENTPORT_SESSION_TOKEN=session-token") {
		t.Fatalf("adapter arguments omit session token: %#v", arguments)
	}
	harness := harnessInstallArguments("agy", profile, "agentport-m2")
	if strings.Contains(strings.Join(harness, " "), "upload") {
		t.Fatalf("harness arguments still carry an upload token: %#v", harness)
	}
}

func assertArgumentsContain(t *testing.T, arguments, required []string) {
	t.Helper()
	for _, value := range required {
		found := false
		for _, argument := range arguments {
			if argument == value {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("arguments %q do not contain %q", arguments, value)
		}
	}
}
