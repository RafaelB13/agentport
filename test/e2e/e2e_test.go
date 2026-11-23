//go:build e2e

// Roda o harness de ponta a ponta (test/e2e/run.sh). Exige Docker e ssh no host.
//
//	go test -tags e2e -v -count=1 ./test/e2e/
//
// Verificações em Go com internal/forward e internal/portscan podem entrar aqui
// depois, no mesmo harness.
package e2e

import (
	"os/exec"
	"testing"
)

func TestE2E(t *testing.T) {
	cmd := exec.Command("bash", "run.sh")
	out, err := cmd.CombinedOutput()
	t.Logf("\n%s", out)
	if err != nil {
		t.Fatalf("run.sh falhou: %v", err)
	}
}
