//go:build e2e

package e2e

// Harness da fase C: binários reais do agentport (host e servidor) contra o
// container sshd do fixture. O host roda com HOME e AGENTPORT_CONFIG_DIR
// temporários e um shim de `ssh` no PATH, que aponta para o config isolado.
// Assim o produto nunca toca ~/.config, ~/.ssh, launchd nem autostart.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	profileName = "e2e"
	bridgePort  = 39123 // RemotePort do perfil: porta do bridge no servidor
)

// product guarda o estado de uma execução da fase C.
type product struct {
	t        *testing.T
	f        *fixture
	bin      string // agentport do host (darwin)
	binDir   string
	home     string
	env      []string
	token    string
	shimDir  string
	profile  string
	sessions []*mcpSession
	mu       sync.Mutex
}

// startProduct compila os binários, instala o servidor no container, faz o pair
// e inicia o Companion. Tudo fica dentro de um HOME temporário.
func startProduct(t *testing.T) *product {
	t.Helper()
	f := fixtureFor(t)
	p := &product{t: t, f: f, profile: profileName}

	p.binDir = t.TempDir()
	p.bin = filepath.Join(p.binDir, "agentport")
	buildBinary(t, p.bin, "", "")
	server := filepath.Join(p.binDir, "agentport-linux")
	buildBinary(t, server, "linux", runtime.GOARCH)
	if out, err := exec.Command("docker", "cp", server, f.name+":/usr/local/bin/agentport").CombinedOutput(); err != nil {
		t.Fatalf("instalar agentport no container: %v\n%s", err, out)
	}
	if out, err := exec.Command("docker", "exec", f.name, "chmod", "755", "/usr/local/bin/agentport").CombinedOutput(); err != nil {
		t.Fatalf("chmod agentport: %v\n%s", err, out)
	}

	// /tmp curto: o socket do ControlMaster precisa caber no limite do unix socket.
	home, err := os.MkdirTemp("/tmp", "agpc-")
	if err != nil {
		t.Fatal(err)
	}
	p.home = home
	t.Cleanup(func() {
		// Em falha, o HOME fica para inspecionar o log do Companion e do bridge.
		if t.Failed() || os.Getenv("AGENTPORT_E2E_KEEP_HOME") == "1" {
			t.Logf("HOME preservado para depuração: %s", home)
			return
		}
		_ = os.RemoveAll(home)
	})
	p.shimDir = filepath.Join(home, "shim")
	if err := os.MkdirAll(p.shimDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal(err)
	}
	shim := fmt.Sprintf("#!/bin/sh\nexec %s -F %s \"$@\"\n", sshPath, f.sshConfig)
	if err := os.WriteFile(filepath.Join(p.shimDir, "ssh"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	p.env = append(os.Environ(),
		"HOME="+filepath.Join(home, "home"),
		"AGENTPORT_CONFIG_DIR="+filepath.Join(home, "cfg"),
		"PATH="+p.shimDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AGENTPORT_LOG_LEVEL=debug",
	)
	t.Cleanup(p.cleanup)

	if out, err := p.ag("pair", p.profile, hostAlias, "--skip-agent", "--remote-port", strconv.Itoa(bridgePort)); err != nil {
		t.Fatalf("pair: %v\n%s", err, out)
	}
	p.token = p.profileToken()
	if out, err := p.ag("companion", "start", p.profile); err != nil {
		t.Fatalf("companion start: %v\n%s", err, out)
	}
	return p
}

// buildBinary compila cmd/agentport. goos vazio = o host; senão cross-compila.
func buildBinary(t *testing.T, out, goos, goarch string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", out, "./cmd/agentport")
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = os.Environ()
	if goos != "" {
		cmd.Env = append(cmd.Env, "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
	}
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", out, err, b)
	}
}

// ag roda o agentport do host com o ambiente isolado e devolve a saída.
func (p *product) ag(args ...string) (string, error) {
	return p.agWith(context.Background(), args...)
}

func (p *product) agWith(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, p.bin, args...)
	cmd.Env = p.env
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (p *product) profileToken() string {
	p.t.Helper()
	b, err := os.ReadFile(filepath.Join(p.home, "cfg", "agentport", "profiles", p.profile+".json"))
	if err != nil {
		p.t.Fatalf("ler perfil: %v", err)
	}
	var prof struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(b, &prof); err != nil || prof.Token == "" {
		p.t.Fatalf("perfil sem token: %v", err)
	}
	return prof.Token
}

// companionStatus lê o `companion status` e devolve o tunnel.connected.
func (p *product) tunnelConnected() bool {
	out, err := p.ag("companion", "status", p.profile)
	if err != nil {
		return false
	}
	var st struct {
		Tunnel struct {
			Connected bool `json:"connected"`
		} `json:"tunnel"`
	}
	if json.Unmarshal([]byte(out), &st) != nil {
		return false
	}
	return st.Tunnel.Connected
}

// waitFor repete check até devolver true ou estourar timeout.
func waitFor(t *testing.T, timeout time.Duration, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("timeout esperando: %s", what)
}

// pendingRequestID devolve o ID do primeiro pedido pendente, quando houver.
func (p *product) pendingRequestID() string {
	out, err := p.ag("requests")
	if err != nil {
		return ""
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 2 || strings.HasPrefix(lines[0], "No pending") {
		return ""
	}
	return strings.Fields(lines[1])[0]
}

// cleanup encerra o Companion e todo processo que usa o binário temporário
// desta execução (bridge, companion serve). O caminho do binário é único por
// teste, então nada fora daqui é atingido.
func (p *product) cleanup() {
	_, _ = p.ag("companion", "stop", p.profile)
	for _, s := range p.sessions {
		s.close()
	}
	out, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasPrefix(strings.TrimSpace(line[len(fields[0]):]), p.bin) {
			continue
		}
		if pid, err := strconv.Atoi(fields[0]); err == nil {
			_ = exec.Command("kill", "-TERM", strconv.Itoa(pid)).Run()
		}
	}
}

// mcpSession é um `agentport mcp` dentro do container, falado por stdio pelo go-sdk.
type mcpSession struct {
	t  *testing.T
	cs *mcp.ClientSession
}

func (p *product) openMCP() *mcpSession {
	p.t.Helper()
	cmd := exec.Command("docker", "exec", "-i", "-u", "dev",
		"-e", "AGENTPORT_BRIDGE_PORT="+strconv.Itoa(bridgePort),
		"-e", "AGENTPORT_SESSION_TOKEN="+p.token,
		p.f.name, "agentport", "mcp")
	client := mcp.NewClient(&mcp.Implementation{Name: "agentport-e2e", Version: "0"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		p.t.Fatalf("conectar MCP: %v", err)
	}
	s := &mcpSession{t: p.t, cs: cs}
	p.mu.Lock()
	p.sessions = append(p.sessions, s)
	p.mu.Unlock()
	return s
}

func (s *mcpSession) close() {
	if s.cs != nil {
		_ = s.cs.Close()
		s.cs = nil
	}
}

// call chama uma ferramenta e devolve o texto JSON do resultado.
func (s *mcpSession) call(name string, args map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	res, err := s.cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", err
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if res.IsError {
		return text, fmt.Errorf("ferramenta %s devolveu erro: %s", name, text)
	}
	return text, nil
}

// callAsync roda call em goroutine; o resultado chega pelo canal.
func (s *mcpSession) callAsync(name string, args map[string]any) <-chan callResult {
	out := make(chan callResult, 1)
	go func() {
		text, err := s.call(name, args)
		out <- callResult{text: text, err: err}
	}()
	return out
}

type callResult struct {
	text string
	err  error
}

func (s *mcpSession) tools() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := s.cs.ListTools(ctx, nil)
	if err != nil {
		s.t.Fatalf("tools/list: %v", err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// forwardResult é a forma de cada resultado de ports_forward / unforward.
type forwardResult struct {
	RemotePort int    `json:"remote_port"`
	State      string `json:"state"`
	LocalPort  int    `json:"local_port"`
	URL        string `json:"url"`
	RequestID  string `json:"request_id"`
	Message    string `json:"message"`
}

func parseForwardResults(t *testing.T, text string) []forwardResult {
	t.Helper()
	var body struct {
		Results []forwardResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		t.Fatalf("resposta de ports_forward inválida: %v\n%s", err, text)
	}
	return body.Results
}

// getURL faz GET em uma URL absoluta e devolve o corpo sem espaços nas bordas.
func getURL(url string) (string, error) {
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(b)), err
}

// waitBridgeOnline espera o bridge alcançar o Companion pelo túnel reverso.
// O status "connected" vira true quando o ssh começa, antes da autenticação e
// do forward reverso: só ports_list sem forwarded_status prova que está de pé.
func (s *mcpSession) waitBridgeOnline(timeout time.Duration) {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		text, err := s.call("ports_list", map[string]any{})
		last = text
		if err == nil && !strings.Contains(text, "forwarded_status") {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	s.t.Fatalf("bridge não alcançou o Companion em %s: %s", timeout, last)
}
