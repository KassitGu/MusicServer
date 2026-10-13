package main

import (
	"os"
	"strings"
	"testing"
)

// The Help tab and the "Help" section of the READMEs are the same text.
// L'onglet Aide et la section « Aide » des LISEZMOI sont le même texte.
func TestHelpInReadme(t *testing.T) {
	for _, c := range []struct{ readme, title, help string }{
		{"../README.md", "## Help", "help/help.en.md"},
		{"../LISEZMOI.md", "## Aide", "help/help.fr.md"},
	} {
		readme, err := os.ReadFile(c.readme)
		if err != nil {
			t.Fatal(err)
		}
		help, err := helpFiles.ReadFile(c.help)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(readme), c.title+"\n\n"+string(help)) {
			t.Errorf("%s: the %q section differs from %s", c.readme, c.title, c.help)
		}
	}
}

func TestMarkdownHTML(t *testing.T) {
	got := markdownHTML("### Title <x>\n\nOne **bold**\nline with `code`.\n- a\n- **b**\n\nEnd")
	want := "<h3>Title &lt;x&gt;</h3>\n<p>One <b>bold</b> line with <code>code</code>.</p>\n<ul>\n<li>a</li>\n<li><b>b</b></li>\n</ul>\n<p>End</p>\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestHelpPage(t *testing.T) {
	_, h, _, store := testApp(t)
	if body := getPage(h, "/?p=help"); !strings.Contains(body, "<h3>Endless radio</h3>") || !strings.Contains(body, `href="/?p=help" class="on"`) {
		t.Fatalf("help page:\n%s", body)
	}
	store.UpdateConfig(func(c *Config) { c.Lang = langFR })
	body := getPage(h, "/?p=help")
	if !strings.Contains(body, "<h3>Radio infinie</h3>") || !strings.Contains(body, "? Aide</a>") {
		t.Fatalf("French help page:\n%s", body)
	}
	if (donateURL == "") == strings.Contains(body, "Offrez-moi un café") {
		t.Fatal("coffee link shown without a link, or missing")
	}
}
