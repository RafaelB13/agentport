package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestSupportedNormalizesLanguageTags(t *testing.T) {
	for tag, want := range map[string]string{
		"pt": Portuguese, "pt-BR": Portuguese, "pt_BR": Portuguese, "PT_br.UTF-8": Portuguese, "pt-PT": Portuguese, " pt-br ": Portuguese,
		"en": English, "en-US": English, "en_GB.UTF-8": English, "EN": English,
	} {
		if got, ok := Supported(tag); !ok || got != want {
			t.Errorf("Supported(%q) = %q, %v; want %q", tag, got, ok, want)
		}
	}
	for _, tag := range []string{"", "fr", "de-DE", "C", "POSIX", "ptx", "english"} {
		if got, ok := Supported(tag); ok {
			t.Errorf("Supported(%q) = %q, want no language", tag, got)
		}
	}
}

func TestFromAcceptLanguageHonoursQualityThenOrder(t *testing.T) {
	for header, want := range map[string]string{
		"pt-BR,pt;q=0.9,en;q=0.8": Portuguese,
		"en-US,en;q=0.9,pt;q=0.8": English,
		"fr-FR,pt;q=0.5,en;q=0.4": Portuguese,
		"en;q=0.3, pt-BR;q=0.7":   Portuguese,
		"pt;q=0, en":              English,
		"fr, de;q=0.5":            "",
		"":                        "",
		"*":                       "",
		"pt-BR;q=bad, en;q=0.5":   Portuguese,
		"en-GB, pt-BR":            English,
	} {
		if got := FromAcceptLanguage(header); got != want {
			t.Errorf("FromAcceptLanguage(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestEnvSelectsTheLanguageAndAnythingElseIsEnglish(t *testing.T) {
	defer SetLanguage(English)
	for value, want := range map[string]string{"pt-BR": Portuguese, "pt_BR.UTF-8": Portuguese, "en-US": English, "": English, "fr": English, "garbage": English} {
		t.Setenv(Env, value)
		Init()
		if Language() != want {
			t.Errorf("%s=%q gave %q, want %q", Env, value, Language(), want)
		}
	}
}

func TestTranslationFallsBackToEnglishAndFormats(t *testing.T) {
	previous := catalogs[Portuguese]
	defer func() { catalogs[Portuguese] = previous }()
	catalogs[Portuguese] = map[string]string{"Hello %s, you have %d files": "Olá %s, você tem %d arquivos", "Empty": ""}

	if got := Tr(Portuguese, "Hello %s, you have %d files", "Ana", 3); got != "Olá Ana, você tem 3 arquivos" {
		t.Errorf("translated = %q", got)
	}
	if got := Tr(English, "Hello %s, you have %d files", "Ana", 3); got != "Hello Ana, you have 3 files" {
		t.Errorf("English = %q", got)
	}
	// Anything the catalog lacks, or leaves empty, is shown in English.
	if got := Tr(Portuguese, "Not translated %s", "yet"); got != "Not translated yet" {
		t.Errorf("fallback = %q", got)
	}
	if got := Tr(Portuguese, "Empty"); got != "Empty" {
		t.Errorf("empty translation = %q, want the English message", got)
	}
	// No arguments: the message is not a format, so a literal percent survives.
	if got := Tr(English, "100% done"); got != "100% done" {
		t.Errorf("a message without arguments was formatted: %q", got)
	}
	if got := Tr("fr-FR", "Anything"); got != "Anything" {
		t.Errorf("an unknown language = %q", got)
	}
}

func TestCatalogReturnsACopy(t *testing.T) {
	if len(Catalog(English)) != 0 {
		t.Error("English needs no catalog")
	}
	first := Catalog(Portuguese)
	for key := range first {
		first[key] = "changed"
	}
	for key, value := range Catalog(Portuguese) {
		if value == "changed" {
			t.Fatalf("changing a returned catalog changed %q", key)
		}
	}
}

// --- the catalog against the source ---------------------------------------

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// usedMessages finds every message the program can show: literals passed to
// i18n.T (or Tr) in the Go source and
// {{t:...}} in its page.
func usedMessages(t *testing.T) map[string][]string {
	t.Helper()
	root := repoRoot(t)
	used := map[string][]string{}
	add := func(message, where string) { used[message] = append(used[message], where) }

	fileSet := token.NewFileSet()
	for _, base := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, base), func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, err := parser.ParseFile(fileSet, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if pkg, ok := selector.X.(*ast.Ident); !ok || pkg.Name != "i18n" {
					return true
				}
				index := map[string]int{"T": 0, "Tr": 1}[selector.Sel.Name]
				if selector.Sel.Name != "T" && selector.Sel.Name != "Tr" {
					return true
				}
				if len(call.Args) <= index {
					return true
				}
				literal, ok := call.Args[index].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					// A message that is not a literal cannot be checked here; the call
					// says so with i18n:dynamic and the messages are checked where they
					// are written (the page's {{t:...}} markers).
					if lineOf(t, path, fileSet.Position(call.Pos()).Line) {
						return true
					}
					t.Errorf("%s: i18n.%s needs a string literal so the message can be checked against the catalog", fileSet.Position(call.Pos()), selector.Sel.Name)
					return true
				}
				message, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				add(message, fileSet.Position(call.Pos()).String())
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// The Companion panel has its own catalogs (internal/companion/ui/assets/i18n),
	// checked against its script and page by the companion package's tests, so its
	// strings are not part of this catalog.
	return used
}

func TestEveryMessageTheProgramShowsHasATranslation(t *testing.T) {
	var missing []string
	for message, places := range usedMessages(t) {
		if catalogs[Portuguese][message] == "" {
			missing = append(missing, strconv.Quote(message)+"  ("+places[0]+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d message(s) have no pt-BR translation:\n%s", len(missing), strings.Join(missing, "\n"))
	}
}

func TestNoTranslationIsLeftBehindForAMessageThatNoLongerExists(t *testing.T) {
	used := usedMessages(t)
	var stale []string
	for message := range catalogs[Portuguese] {
		if _, ok := used[message]; !ok {
			stale = append(stale, strconv.Quote(message))
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("%d translation(s) for messages that are not used:\n%s", len(stale), strings.Join(stale, "\n"))
	}
}

var verbPattern = regexp.MustCompile(`%[-+# 0-9.*]*[a-zA-Z]`)

// A translation that drops or reorders a verb would print the wrong value or
// panic at run time, so every one keeps the verbs of its English message, in order.
func TestTranslationsKeepTheFormatVerbsOfTheirMessage(t *testing.T) {
	for _, language := range catalogLanguages {
		for message, translated := range catalogs[language] {
			if strings.TrimSpace(translated) == "" {
				t.Errorf("%s: empty translation for %q", language, message)
				continue
			}
			want := verbPattern.FindAllString(strings.ReplaceAll(message, "%%", ""), -1)
			got := verbPattern.FindAllString(strings.ReplaceAll(translated, "%%", ""), -1)
			if strings.Join(want, " ") != strings.Join(got, " ") {
				t.Errorf("%s: %q has verbs %v but its translation %q has %v", language, message, want, translated, got)
			}
			if strings.HasSuffix(message, " ") != strings.HasSuffix(translated, " ") {
				t.Errorf("%s: %q and its translation differ in a trailing space, which the dashboard relies on", language, message)
			}
		}
	}
}

// lineOf reports whether the given line of a file carries the i18n:dynamic marker.
func lineOf(t *testing.T, path string, line int) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	return line >= 1 && line <= len(lines) && strings.Contains(lines[line-1], "i18n:dynamic")
}

// Detect follows the order AGENTPORT_LANG, LC_ALL, LC_MESSAGES, LANG. The first
// variable that is set decides; an unsupported value means English.
func TestDetectFollowsTheVariableOrder(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"nothing set", nil, English},
		{"AGENTPORT_LANG wins over everything", map[string]string{"AGENTPORT_LANG": "pt-BR", "LC_ALL": "es_ES.UTF-8", "LANG": "zh_TW.UTF-8"}, Portuguese},
		{"LC_ALL wins over LC_MESSAGES and LANG", map[string]string{"LC_ALL": "es_ES.UTF-8", "LC_MESSAGES": "pt_BR", "LANG": "zh_TW"}, Spanish},
		{"LC_MESSAGES wins over LANG", map[string]string{"LC_MESSAGES": "zh_HK.UTF-8", "LANG": "pt_BR"}, ChineseTraditional},
		{"LANG is the last", map[string]string{"LANG": "pt_PT.UTF-8"}, Portuguese},
		{"first set value decides even if unsupported", map[string]string{"AGENTPORT_LANG": "fr", "LANG": "pt_BR"}, English},
		{"empty values are unset", map[string]string{"AGENTPORT_LANG": "  ", "LC_ALL": "", "LANG": "es_MX"}, Spanish},
		{"C locale is English", map[string]string{"LC_ALL": "C", "LANG": "pt_BR.UTF-8"}, English},
		{"POSIX locale is English", map[string]string{"LANG": "POSIX"}, English},
		{"C.UTF-8 is English", map[string]string{"LANG": "C.UTF-8"}, English},
		{"Spanish from Mexico", map[string]string{"LANG": "es_MX.UTF-8"}, Spanish},
		{"Spanish from Spain", map[string]string{"LC_ALL": "es_ES"}, Spanish},
		{"Traditional Chinese from Taiwan", map[string]string{"LANG": "zh_TW.UTF-8"}, ChineseTraditional},
		{"Traditional Chinese from Hong Kong", map[string]string{"LANG": "zh_HK"}, ChineseTraditional},
		{"Simplified Chinese is English", map[string]string{"LANG": "zh_CN.UTF-8"}, English},
		{"AGENTPORT_LANG accepts a Spanish tag", map[string]string{"AGENTPORT_LANG": "es"}, Spanish},
	}
	for _, c := range cases {
		getenv := func(name string) string { return c.env[name] }
		if got := Detect(getenv); got != c.want {
			t.Errorf("%s: Detect = %q, want %q", c.name, got, c.want)
		}
	}
}

// The mapping of locale tags to languages. zh-CN and zh-Hans have no catalog and
// are not supported, so they are English.
func TestSupportedMapsLocalesToLanguages(t *testing.T) {
	for tag, want := range map[string]string{
		"pt": Portuguese, "pt_BR": Portuguese, "pt_BR.UTF-8": Portuguese, "pt_PT": Portuguese, "pt-BR": Portuguese,
		"es": Spanish, "es_ES.UTF-8": Spanish, "es_MX": Spanish, "es-419": Spanish,
		"zh_TW": ChineseTraditional, "zh_TW.UTF-8": ChineseTraditional, "zh_HK": ChineseTraditional, "zh_MO": ChineseTraditional,
		"zh-Hant": ChineseTraditional, "zh-Hant-TW": ChineseTraditional, "zh_TW@stroke": ChineseTraditional,
		"zh-TW": ChineseTraditional, "zh-HK": ChineseTraditional, "zh_Hant_TW.UTF-8": ChineseTraditional,
	} {
		if got, ok := Supported(tag); !ok || got != want {
			t.Errorf("Supported(%q) = %q, %v; want %q", tag, got, ok, want)
		}
	}
	for _, tag := range []string{"zh_CN", "zh-CN", "zh-Hans", "zh", "zh_CN.UTF-8", "esperanto", "espanol", "fr_FR", "C", "POSIX", ""} {
		if got, ok := Supported(tag); ok {
			t.Errorf("Supported(%q) = %q, want English (no language)", tag, got)
		}
	}
}

// Init reads the environment, so the Companion and the CLI agree on a language.
func TestInitUsesTheDetectionOrder(t *testing.T) {
	defer SetLanguage(English)
	t.Setenv(Env, "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "pt_BR.UTF-8")
	Init()
	if Language() != Portuguese {
		t.Fatalf("Init with LANG=pt_BR.UTF-8 chose %q", Language())
	}
	t.Setenv("LC_ALL", "C")
	Init()
	if Language() != English {
		t.Fatalf("Init with LC_ALL=C chose %q", Language())
	}
}

// Every catalog has exactly the keys of the pt-BR catalog, so no language shows
// an English line that the others translate.
func TestEveryCatalogHasExactlyThePortugueseKeys(t *testing.T) {
	for _, language := range catalogLanguages {
		if language == Portuguese {
			continue
		}
		var missing, extra []string
		for key := range catalogs[Portuguese] {
			if _, ok := catalogs[language][key]; !ok {
				missing = append(missing, strconv.Quote(key))
			}
		}
		for key := range catalogs[language] {
			if _, ok := catalogs[Portuguese][key]; !ok {
				extra = append(extra, strconv.Quote(key))
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)
		if len(missing) > 0 || len(extra) > 0 {
			t.Errorf("%s: %d key(s) missing %v, %d extra %v", language, len(missing), missing, len(extra), extra)
		}
	}
}

// The Spanish and Traditional Chinese catalogs translate, and do not fall back.
func TestSpanishAndTraditionalChineseTranslate(t *testing.T) {
	for _, language := range []string{Spanish, ChineseTraditional} {
		if len(Catalog(language)) != len(catalogs[Portuguese]) {
			t.Errorf("%s has %d translations, want %d", language, len(Catalog(language)), len(catalogs[Portuguese]))
		}
	}
	if got := Tr(Spanish, "Also known as:"); got != "También conocido como:" {
		t.Errorf("Tr(es) = %q", got)
	}
	if got := Tr(ChineseTraditional, "Also known as:"); got != "別名：" {
		t.Errorf("Tr(zh-Hant) = %q", got)
	}
}

// A message with no translation is still shown in English.
func TestAMessageWithoutATranslationIsEnglish(t *testing.T) {
	if got := Tr(Spanish, "no such message %d", 1); got != "no such message 1" {
		t.Errorf("Tr(es) = %q", got)
	}
}
