//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"testing"

	"github.com/rafaelb13/agentport/internal/testenv"
)

// TestMain isolates the cache and config directories unless AGENTPORT_CACHE_DIR
// is already set, and fails the run if it still created the developer's real
// agentport cache directory.
func TestMain(m *testing.M) {
	os.Exit(runE2E(m))
}

func runE2E(m *testing.M) int {
	guard := testenv.NewCacheGuard()
	cleanup, err := guard.Isolate()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer cleanup()

	code := m.Run()
	closeFixture()
	if err := guard.Verify(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if code == 0 {
			code = 1
		}
	}
	return code
}
