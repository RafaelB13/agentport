package companion

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The dashboard builds rows from JavaScript strings, so a CSS rule can silently
// stop matching anything: a rule for a class that no code applies styles nothing,
// and nothing reports it.
// Every "*-row" class the stylesheet declares must therefore be used by the page.
func TestDashboardStylesheetRowClassesAreUsedByThePage(t *testing.T) {
	page, err := dashboardFiles.ReadFile("ui/index.html")
	if err != nil {
		t.Fatal(err)
	}
	styles, err := dashboardFiles.ReadFile("ui/assets/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	// The rows are built by the page's script, which lives in its own file.
	script, err := dashboardFiles.ReadFile("ui/assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	page = append(page, script...)

	declared := map[string]bool{}
	for _, match := range regexp.MustCompile(`\.([a-z][a-z0-9]*(?:-[a-z0-9]+)*-row)\b`).FindAllStringSubmatch(string(styles), -1) {
		declared[match[1]] = true
	}
	if len(declared) == 0 {
		t.Fatal("expected the stylesheet to declare row classes")
	}
	for class := range declared {
		if !strings.Contains(string(page), class) {
			t.Errorf("stylesheet styles .%s but neither index.html nor app.js applies it", class)
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
