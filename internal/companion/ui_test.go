package companion

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A CSS rule can silently stop matching anything: a rule for a class that no
// code applies styles nothing, and nothing reports it. Tailwind writes only the
// utilities the source uses, so the built app.css cannot carry such a rule; the
// hand-written stylesheet can, so every class it declares must be used in web/src.
func TestPanelStylesheetClassesAreUsedByTheSource(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "index.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(string(styles), "")
	var source strings.Builder
	panelSource(t, func(name, text string) {
		if !strings.HasPrefix(path.Base(name), "index.css") {
			source.WriteString(text)
		}
	})
	declared := map[string]bool{}
	for _, match := range regexp.MustCompile(`\.([a-z][a-z0-9-]*)\b`).FindAllStringSubmatch(css, -1) {
		declared[match[1]] = true
	}
	for class := range declared {
		if !regexp.MustCompile(`['"\s]` + regexp.QuoteMeta(class) + `['"\s]`).MatchString(source.String()) {
			t.Errorf("web/src/index.css styles .%s but no source file applies it", class)
		}
	}
}

const designSystemAssets = "../../design-systems/agentport-companion/assets/"

func TestDesignSystemBrandAssetsMatchTheDashboard(t *testing.T) {
	for _, name := range []string{"agentport-mark.svg", "favicon.svg"} {
		design, err := os.ReadFile(designSystemAssets + name)
		if err != nil {
			t.Fatal(err)
		}
		dashboard, err := dashboardFiles.ReadFile("ui/assets/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(design, dashboard) {
			t.Errorf("%s differs between the design system and the dashboard", name)
		}
	}
}
