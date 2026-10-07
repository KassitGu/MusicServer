package main

// Display language (British English or French).
// Langue d'affichage (anglais britannique ou français).
//
// Every text shown to the user (settings page, messages, rows and folders
// in the control app) is written in British English in the code and
// translated through the frText table below. A missing entry falls back
// to English; TestFrenchTranslations checks that none is missing.
//
// Tout texte montré à l'utilisateur (page de réglages, messages, rangées et
// dossiers de l'application de contrôle) est écrit en anglais britannique
// dans le code et traduit par la table frText ci-dessous. Une entrée
// absente retombe sur l'anglais ; TestFrenchTranslations vérifie qu'il n'en
// manque aucune.

import (
	"fmt"
	"net/http"
	"strings"
)

const (
	langEN = "en"
	langFR = "fr"
)

// validLang reports whether l is a supported language code.
// validLang indique si l est un code de langue pris en charge.
func validLang(l string) bool { return l == langEN || l == langFR }

// configLang returns the language chosen on the settings page; English
// until a choice has been made.
// configLang renvoie la langue choisie sur la page de réglages ; anglais
// tant qu'aucun choix n'a été fait.
func configLang(c Config) string {
	if validLang(c.Lang) {
		return c.Lang
	}
	return langEN
}

// browserLang picks French when the browser prefers it, English otherwise.
// It is only used until a language has been saved.
// browserLang choisit le français si le navigateur le préfère, l'anglais
// sinon. Sert seulement tant qu'aucune langue n'est enregistrée.
func browserLang(r *http.Request) string {
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
		switch {
		case tag == "fr" || strings.HasPrefix(tag, "fr-"):
			return langFR
		case tag == "en" || strings.HasPrefix(tag, "en-"):
			return langEN
		}
	}
	return langEN
}

// tr translates an English text into lang.
// tr traduit un texte anglais dans la langue lang.
func tr(lang, s string) string {
	if lang == langFR {
		if t, ok := frText[s]; ok {
			return t
		}
	}
	return s
}

// trf translates an English format string, then formats it.
// trf traduit une chaîne de format anglaise, puis la met en forme.
func trf(lang, format string, args ...any) string {
	return fmt.Sprintf(tr(lang, format), args...)
}
