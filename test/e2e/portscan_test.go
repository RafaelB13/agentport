//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rafaelb13/agentport/internal/portscan"
)

// buildScan cross-compila cmd/scan para Linux, que é onde o container roda.
func buildScan(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "scan")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/scan")
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build cmd/scan: %v\n%s", err, out)
	}
	return bin
}

// scanAs roda o binário dentro do container como o usuário dado ("" = padrão, root).
func scanAs(t *testing.T, f *fixture, user string) []portscan.Listener {
	t.Helper()
	args := []string{"exec"}
	if user != "" {
		args = append(args, "-u", user)
	}
	args = append(args, f.name, "/usr/local/bin/scan")
	out, err := exec.Command("docker", args...).Output()
	if err != nil {
		t.Fatalf("scan como %q: %v", user, err)
	}
	var ls []portscan.Listener
	if err := json.NewDecoder(bytes.NewReader(out)).Decode(&ls); err != nil {
		t.Fatalf("JSON do scan: %v\n%s", err, out)
	}
	return ls
}

func findPort(ls []portscan.Listener, port int) (portscan.Listener, bool) {
	for _, l := range ls {
		if l.Port == port {
			return l, true
		}
	}
	return portscan.Listener{}, false
}

// TestPortscanInContainer: o portscan real, dentro do Linux, como dev e como root.
func TestPortscanInContainer(t *testing.T) {
	f := fixtureFor(t)
	bin := buildScan(t)
	if out, err := exec.Command("docker", "cp", bin, f.name+":/usr/local/bin/scan").CombinedOutput(); err != nil {
		t.Fatalf("docker cp: %v\n%s", err, out)
	}

	t.Run("dev", func(t *testing.T) {
		ls := scanAs(t, f, "dev")
		want := map[int]struct{ bind, proc string }{
			3000: {"127.0.0.1", "python3"},
			5173: {"127.0.0.1", "python3"},
			8080: {"0.0.0.0", "python3"},
			9229: {"::1", "python3"},
		}
		for port, w := range want {
			l, ok := findPort(ls, port)
			if !ok {
				t.Errorf("porta %d ausente; scan=%+v", port, ls)
				continue
			}
			if l.Bind != w.bind {
				t.Errorf("porta %d bind = %q, quero %q", port, l.Bind, w.bind)
			}
			if l.PID <= 0 || l.Process != w.proc || l.User != "dev" {
				t.Errorf("porta %d: pid=%d process=%q user=%q; quero pid>0, %q, dev", port, l.PID, l.Process, l.User, w.proc)
			}
		}

		// Processo de outro usuário: aparece, mas sem pid nem nome de processo.
		o, ok := findPort(ls, 5432)
		if !ok {
			t.Errorf("5432 (other) ausente como dev")
		} else if o.PID != 0 || o.Process != "" {
			t.Errorf("5432 como dev: pid=%d process=%q; quero 0 e vazio", o.PID, o.Process)
		} else if o.Bind != "127.0.0.1" {
			t.Errorf("5432 bind = %q", o.Bind)
		}

		// sshd (22) é privilegiado: o padrão do Scan filtra portas < 1024.
		if _, ok := findPort(ls, 22); ok {
			t.Errorf("22 apareceu no scan padrão; deveria ser filtrado")
		}
	})

	t.Run("root", func(t *testing.T) {
		ls := scanAs(t, f, "")
		o, ok := findPort(ls, 5432)
		if !ok {
			t.Fatalf("5432 ausente como root; scan=%+v", ls)
		}
		if o.PID <= 0 || o.User != "other" || o.Process != "python3" {
			t.Errorf("5432 como root: pid=%d user=%q process=%q; quero pid>0, other, python3", o.PID, o.User, o.Process)
		}
	})
}

// TestPortscanLinuxUnit roda os testes unitários do pacote no Linux, em um
// container golang:1.27 descartável (--rm), com o repo montado somente leitura.
func TestPortscanLinuxUnit(t *testing.T) {
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("docker", "run", "--rm",
		"-v", repo+":/src:ro", "-w", "/src",
		"-e", "GOCACHE=/tmp/gocache", "-e", "GOPATH=/tmp/gopath", "-e", "GOFLAGS=-mod=mod",
		"golang:1.27", "go", "test", "-count=1", "./internal/portscan/")
	out, err := cmd.CombinedOutput()
	t.Logf("\n%s", out)
	if err != nil {
		t.Fatalf("go test ./internal/portscan/ no Linux falhou: %v", err)
	}
}
