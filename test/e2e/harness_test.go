//go:build e2e

package e2e

// Fixture compartilhado dos testes Go: um container sshd descartável por
// execução de `go test`, criado sob demanda e removido no TestMain. Mesmas
// garantias do run.sh: chave e ssh/known_hosts isolados, só a porta 22 publicada
// em 127.0.0.1, nada fora do próprio container/imagem/diretório é tocado.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rafaelb13/agentport/internal/forward"
)

const hostAlias = "agentport-e2e"

type fixture struct {
	id        string
	image     string
	name      string
	dir       string // diretório temporário próprio (chave, config, socket)
	sshPath   string
	sshConfig string
	ctl       string
	sshPort   int
}

var (
	fxOnce sync.Once
	fx     *fixture
	fxErr  error
)

// fixtureFor devolve o fixture compartilhado, criando-o na primeira chamada.
// Falha o teste atual se o ambiente não puder subir o container.
func fixtureFor(t *testing.T) *fixture {
	t.Helper()
	fxOnce.Do(func() { fx, fxErr = startFixture() })
	if fxErr != nil {
		t.Fatalf("fixture e2e: %v", fxErr)
	}
	return fx
}

// closeFixture é chamado pelo TestMain. Remove só o que o fixture criou.
func closeFixture() {
	if fx != nil {
		fx.close()
	}
}

func run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

func startFixture() (*fixture, error) {
	id := strconv.FormatInt(time.Now().UnixNano()%1e9, 36) + "-" + strconv.Itoa(os.Getpid())
	f := &fixture{id: id, image: "agentport-e2e-server:gotest-" + id, name: "agentport-e2e-gotest-" + id}

	dir, err := os.MkdirTemp("", "agp-e2e-")
	if err != nil {
		return nil, err
	}
	f.dir = dir
	f.ctl = filepath.Join(dir, "ctl")
	f.sshConfig = filepath.Join(dir, "ssh_config")
	key := filepath.Join(dir, "id_ed25519")
	if out, err := run("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "agentport-e2e", "-f", key); err != nil {
		return nil, fmt.Errorf("ssh-keygen: %v: %s", err, out)
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		return nil, err
	}
	if f.sshPath, err = exec.LookPath("ssh"); err != nil {
		return nil, err
	}

	if out, err := run("docker", "build", "-t", f.image, "server"); err != nil {
		return nil, fmt.Errorf("docker build: %v: %s", err, tail(out))
	}
	// SYS_PTRACE: como no run.sh, um root de servidor real tem essa capability.
	if out, err := run("docker", "run", "-d", "--name", f.name, "--cap-add", "SYS_PTRACE", "-p", "127.0.0.1::22",
		"-e", "AUTHORIZED_KEY="+strings.TrimSpace(string(pub)), f.image); err != nil {
		return nil, fmt.Errorf("docker run: %v: %s", err, out)
	}
	port, err := run("docker", "port", f.name, "22/tcp")
	if err != nil {
		return nil, fmt.Errorf("docker port: %v", err)
	}
	pub22 := strings.TrimSpace(string(port))
	f.sshPort, err = strconv.Atoi(pub22[strings.LastIndex(pub22, ":")+1:])
	if err != nil {
		return nil, fmt.Errorf("porta publicada inesperada: %q", pub22)
	}

	cfg := fmt.Sprintf(`Host %s
  HostName 127.0.0.1
  Port %d
  User dev
  IdentityFile %s
  IdentitiesOnly yes
  IdentityAgent none
  UserKnownHostsFile %s
  GlobalKnownHostsFile /dev/null
  StrictHostKeyChecking accept-new
  BatchMode yes
  ControlPath %s
  ControlMaster no
  LogLevel ERROR
`, hostAlias, f.sshPort, key, filepath.Join(dir, "known_hosts"), f.ctl)
	if err := os.WriteFile(f.sshConfig, []byte(cfg), 0o600); err != nil {
		return nil, err
	}

	if err := f.waitSSH(); err != nil {
		return nil, fmt.Errorf("sshd não aceitou a chave: %v: %s", err, f.logs())
	}
	if err := f.waitListeners(); err != nil {
		return nil, err
	}
	if err := f.startMaster(); err != nil {
		return nil, err
	}
	return f, nil
}

// runner executa o ssh do fixture com o config isolado (-F). Outros comandos passam direto.
func (f *fixture) runner(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == f.sshPath {
		args = append([]string{"-F", f.sshConfig}, args...)
	}
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (f *fixture) ssh(args ...string) ([]byte, error) {
	return f.runner(context.Background(), f.sshPath, append([]string{"-F", f.sshConfig}, args...)...)
}

func (f *fixture) waitSSH() error {
	var err error
	for i := 0; i < 60; i++ {
		if _, err = f.ssh("-o", "ConnectTimeout=2", hostAlias, "true"); err == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return err
}

// waitListeners espera os serviços do container aparecerem em `ss -ltn`.
func (f *fixture) waitListeners() error {
	want := []string{"127.0.0.1:3000", "127.0.0.1:5173", "0.0.0.0:8080", "127.0.0.1:5432"}
	for _, addr := range want {
		ok := false
		for i := 0; i < 40 && !ok; i++ {
			out, _ := run("docker", "exec", f.name, "ss", "-ltn")
			ok = strings.Contains(string(out), addr+" ")
			if !ok {
				time.Sleep(250 * time.Millisecond)
			}
		}
		if !ok {
			return fmt.Errorf("serviço %s não escuta no container", addr)
		}
	}
	return nil
}

// startMaster abre o ControlMaster (ou o reabre, se o anterior morreu).
func (f *fixture) startMaster() error {
	out, err := f.ssh("-M", "-N", "-f", "-o", "ControlPersist=yes", "-S", f.ctl, hostAlias)
	if err != nil {
		return fmt.Errorf("ControlMaster: %v: %s", err, out)
	}
	return nil
}

// stopMaster encerra o ControlMaster. Os forwards dele caem junto.
func (f *fixture) stopMaster() {
	_, _ = f.ssh("-S", f.ctl, "-O", "exit", hostAlias)
}

// forwarder devolve um forward.Forwarder real apontando para o ControlMaster do fixture.
func (f *fixture) forwarder() forward.Forwarder {
	return forward.NewControlMaster(f.sshPath, f.ctl, hostAlias, f.runner)
}

func (f *fixture) logs() string {
	out, _ := run("docker", "logs", "--tail", "30", f.name)
	return string(out)
}

func (f *fixture) close() {
	f.stopMaster()
	if f.name != "" {
		_, _ = run("docker", "rm", "-f", f.name)
	}
	if f.image != "" {
		_, _ = run("docker", "rmi", f.image)
	}
	if f.dir != "" {
		_ = os.RemoveAll(f.dir)
	}
}

// httpGet devolve o corpo de GET http://127.0.0.1:port/ com timeout curto.
func httpGet(port int) (string, error) {
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(b)), err
}

// listenerNames devolve os nomes lsof dos listeners TCP em port.
func listenerNames(port int) ([]string, error) {
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-F", "n").Output()
	if err != nil {
		// lsof sai com 1 quando não há correspondência.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n") {
			names = append(names, line[1:])
		}
	}
	return names, nil
}

func tail(b []byte) string {
	lines := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	return string(bytes.Join(lines, []byte("\n")))
}
