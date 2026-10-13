package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestFileBirth: when the kernel gives a creation date, it is the one of a
// file just created (checks the statx number and offsets of each processor).
// TestFileBirth : quand le noyau donne une date de création, c'est celle d'un
// fichier tout juste créé (vérifie le numéro statx et les décalages de chaque
// processeur).
func TestFileBirth(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := fileBirth(p)
	if b == 0 {
		t.Skip("no creation date on this system")
	}
	if d := time.Since(time.Unix(b, 0)); d < -time.Minute || d > time.Hour {
		t.Fatalf("creation date %d is not now", b)
	}
}
