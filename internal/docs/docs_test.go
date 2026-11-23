package docs

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const root = "../../"

// readmes are the four languages of the README. English is the default.
var readmes = []string{"README.md", "README.pt-BR.md", "README.es.md", "README.zh-Hant.md"}

// languageLinks are the targets of the language bar, which every README carries.
var languageLinks = []string{"(README.md)", "(README.pt-BR.md)", "(README.es.md)", "(README.zh-Hant.md)"}

func read(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(root + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// shellLines returns the commands of the fenced shell blocks (bash, sh and
// powershell), without blank lines and without comment lines, which are
// translated. Other fenced blocks, such as diagrams and planned configuration,
// are prose and may differ between languages.
func shellLines(markdown string) []string {
	var lines []string
	inside := false
	shell := false
	for _, line := range strings.Split(markdown, "\n") {
		if strings.HasPrefix(line, "```") {
			if inside {
				inside = false
				shell = false
				continue
			}
			inside = true
			language := strings.TrimSpace(strings.TrimPrefix(line, "```"))
			shell = language == "bash" || language == "sh" || language == "powershell"
			continue
		}
		trimmed := strings.TrimSpace(line)
		if inside && shell && trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

// Every README must carry the same commands, so a command changed in one
// language and not in the others cannot leave readers of one language with a
// broken instruction.
func TestReadmesHaveTheSameCommands(t *testing.T) {
	reference := shellLines(read(t, readmes[0]))
	if len(reference) == 0 {
		t.Fatalf("found no commands in %s", readmes[0])
	}
	for _, name := range readmes[1:] {
		other := shellLines(read(t, name))
		longest := len(reference)
		if len(other) > longest {
			longest = len(other)
		}
		for i := 0; i < longest; i++ {
			var want, got string
			if i < len(reference) {
				want = reference[i]
			}
			if i < len(other) {
				got = other[i]
			}
			if want != got {
				t.Fatalf("command %d differs between %s and %s:\n  %s: %q\n  %s: %q", i+1, readmes[0], name, readmes[0], want, name, got)
			}
		}
	}
}

// Every README has the same level-2 sections, so a section added in one
// language must be translated into the others.
func TestReadmesHaveTheSameSections(t *testing.T) {
	headings := regexp.MustCompile(`(?m)^## `)
	want := len(headings.FindAllString(read(t, readmes[0]), -1))
	if want == 0 {
		t.Fatalf("%s has no sections", readmes[0])
	}
	for _, name := range readmes[1:] {
		if got := len(headings.FindAllString(read(t, name), -1)); got != want {
			t.Errorf("%s has %d level-2 sections and %s has %d; translate every section", readmes[0], want, name, got)
		}
	}
}

// The language bar names the four languages in every README. The current
// language is shown in bold, so the other three are links.
func TestReadmesHaveTheLanguageBar(t *testing.T) {
	labels := []string{"English", "Português (Brasil)", "Español", "繁體中文"}
	for _, name := range readmes {
		text := read(t, name)
		for _, label := range labels {
			if !strings.Contains(text, label) {
				t.Errorf("%s does not show the language %q in its language bar", name, label)
			}
		}
		for _, link := range languageLinks {
			if link == "("+name+")" {
				continue
			}
			if !strings.Contains(text, link) {
				t.Errorf("%s does not link to %s in its language bar", name, link)
			}
		}
	}
}

// Both the sentence that introduces the attestation and the one that names
// its fallback mention the first release that has one. Each README must name
// it the same number of times, and the same version, so a drift is caught.
func TestReadmesAgreeOnTheAttestationSettings(t *testing.T) {
	const attested = "v0.7.1-rc.1"
	versions := regexp.MustCompile(`v\d+\.\d+\.\d+-rc\.\d+`)
	for _, name := range append([]string{"SECURITY.md"}, readmes...) {
		text := read(t, name)
		for _, version := range versions.FindAllString(text, -1) {
			if version != attested {
				t.Errorf("%s names %s as a release with an attestation, want %s", name, version, attested)
			}
		}
		if got := strings.Count(text, attested); got != 2 {
			t.Errorf("%s mentions the first release with an attestation %d time(s), want 2", name, got)
		}
	}
	// The variable is documented in every README; SECURITY.md names the release only.
	for _, name := range readmes {
		if !strings.Contains(read(t, name), "AGENTPORT_SKIP_ATTESTATION=1") {
			t.Errorf("%s does not document AGENTPORT_SKIP_ATTESTATION=1", name)
		}
	}
}

func TestCommandReferenceDescribesTheHomebrewUpgrade(t *testing.T) {
	if !strings.Contains(read(t, "COMMANDS.md"), "brew upgrade agentport") {
		t.Error("COMMANDS.md does not describe the Homebrew behaviour of agentport upgrade")
	}
}
