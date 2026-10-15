package main

import "testing"

// The MusicBrainz query keeps quotes and backslashes inside the phrase.
// La requête MusicBrainz garde guillemets et barres obliques dans la phrase.
func TestLuceneQuote(t *testing.T) {
	if got := luceneQuote.Replace(`AC\DC "live"`); got != `AC\\DC \"live\"` {
		t.Fatalf("luceneQuote: %q", got)
	}
}
