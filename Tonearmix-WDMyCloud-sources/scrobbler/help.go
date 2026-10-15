package main

// Help tab of the settings page. The text lives in help/help.en.md and
// help/help.fr.md, which are also the "Help" section of README.md and
// LISEZMOI.md (TestHelpInReadme checks that they stay identical). Only the
// Markdown used there is understood: "###" headings, paragraphs, "- " lists,
// **bold** and `code`.
//
// Onglet Aide de la page de réglages. Le texte est dans help/help.en.md et
// help/help.fr.md, qui forment aussi la section « Aide » de README.md et de
// LISEZMOI.md (TestHelpInReadme vérifie qu'ils restent identiques). Seul le
// Markdown utilisé là est compris : titres « ### », paragraphes, listes
// « - », **gras** et `code`.

import (
	"embed"
	"html"
	"html/template"
	"regexp"
	"strings"
)

// donateURL is the "Buy me a coffee" link, shown in the Help tab and at the
// bottom of every page; empty: hidden.
// donateURL est le lien « Offrez-moi un café », affiché dans l'onglet Aide
// et en bas de chaque page ; vide : masqué.
const donateURL = "https://buymeacoffee.com/guzkassit"

//go:embed help/help.en.md help/help.fr.md
var helpFiles embed.FS

var (
	boldRe = regexp.MustCompile(`\*\*(.+?)\*\*`)
	codeRe = regexp.MustCompile("`([^`]+)`")
)

// helpHTML returns the help text in lang, as HTML.
// helpHTML renvoie le texte d'aide dans la langue lang, en HTML.
func helpHTML(lang string) template.HTML {
	name := "help/help.en.md"
	if lang == langFR {
		name = "help/help.fr.md"
	}
	b, err := helpFiles.ReadFile(name)
	if err != nil {
		return ""
	}
	return template.HTML(markdownHTML(string(b)))
}

// markdownHTML converts the small Markdown subset of the help files. The
// text is escaped first, so it can never inject HTML.
// markdownHTML convertit le petit sous-ensemble Markdown des fichiers
// d'aide. Le texte est d'abord échappé : il ne peut jamais injecter de HTML.
func markdownHTML(md string) string {
	inline := func(s string) string {
		s = html.EscapeString(s)
		s = codeRe.ReplaceAllString(s, "<code>$1</code>")
		return boldRe.ReplaceAllString(s, "<b>$1</b>")
	}
	var out, para strings.Builder
	inList := false
	flush := func() {
		if para.Len() > 0 {
			out.WriteString("<p>" + inline(para.String()) + "</p>\n")
			para.Reset()
		}
		if inList {
			out.WriteString("</ul>\n")
			inList = false
		}
	}
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimRight(line, " \r")
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "### "):
			flush()
			out.WriteString("<h3>" + inline(line[4:]) + "</h3>\n")
		case strings.HasPrefix(line, "- "):
			if para.Len() > 0 {
				flush()
			}
			if !inList {
				out.WriteString("<ul>\n")
				inList = true
			}
			out.WriteString("<li>" + inline(line[2:]) + "</li>\n")
		default:
			if para.Len() > 0 {
				para.WriteByte(' ')
			}
			para.WriteString(line)
		}
	}
	flush()
	return out.String()
}
