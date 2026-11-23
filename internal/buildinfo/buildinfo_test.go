package buildinfo

import (
	"strings"
	"testing"
)

// A development build must not report the version of another project.
func TestDevelopmentVersionIsOwnAndMarkedAsDev(t *testing.T) {
	if !strings.HasSuffix(Version, "-dev") && !strings.Contains(Version, "dev") {
		t.Skipf("Version %q is a release value set by the linker", Version)
	}
	if strings.HasPrefix(Version, "0.7") {
		t.Fatalf("Version = %q inherits the AgentClip version", Version)
	}
}
