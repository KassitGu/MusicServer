package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestExtraFolders: tracks of an extra folder are indexed with the main one,
// and kept while that folder is unreachable.
// TestExtraFolders : les morceaux d'un dossier supplémentaire sont indexés
// avec le principal, et gardés tant que ce dossier est inaccessible.
func TestExtraFolders(t *testing.T) {
	base := t.TempDir()
	main, usb := filepath.Join(base, "share", "Music"), filepath.Join(base, "usb", "Music")
	for i, dir := range []string{main, usb} {
		album := filepath.Join(dir, "Artist", "Album")
		if err := os.MkdirAll(album, 0o755); err != nil {
			t.Fatal(err)
		}
		makeFLAC(t, filepath.Join(album, "01 Track.flac"), [][2]string{{"ARTIST", "Artist " + itoa(i)}, {"ALBUM", "Album " + itoa(i)},
			{"TITLE", "Track"}, {"TRACKNUMBER", "1"}}, nil, "", 44100, 16, 2, 44100, nil)
	}
	extra := []string{usb, main + "/Artist"} // the nested one is ignored / le dossier inclus est ignoré
	sc := NewScanner(main, filepath.Join(base, "index.json"))
	sc.Extra = func() []string { return extra }
	if got := sc.Roots(); !slices.Equal(got, []string{main, usb}) {
		t.Fatalf("roots %v", got)
	}
	tracks, _, err := sc.Scan(context.Background())
	if err != nil || len(tracks) != 2 {
		t.Fatalf("first scan: %d tracks, %v", len(tracks), err)
	}

	// Disk unplugged: its track stays, the folder is reported offline.
	// Disque débranché : son morceau reste, le dossier est signalé hors ligne.
	if err := os.Rename(filepath.Join(base, "usb"), filepath.Join(base, "usb-away")); err != nil {
		t.Fatal(err)
	}
	tracks, _, err = sc.Scan(context.Background())
	if err != nil || len(tracks) != 2 {
		t.Fatalf("offline scan: %d tracks, %v", len(tracks), err)
	}
	if st := sc.Status(); !slices.Equal(st.Offline, []string{usb}) {
		t.Fatalf("offline %v", st.Offline)
	}

	// Folder removed from the settings: its tracks leave the library.
	// Dossier retiré des réglages : ses morceaux quittent la bibliothèque.
	extra = nil
	tracks, _, err = sc.Scan(context.Background())
	if err != nil || len(tracks) != 1 || len(sc.Status().Offline) != 0 {
		t.Fatalf("after removal: %d tracks, %v, offline %v", len(tracks), err, sc.Status().Offline)
	}

	// The main folder stays mandatory.
	// Le dossier principal reste obligatoire.
	os.RemoveAll(main)
	if _, _, err := sc.Scan(context.Background()); err == nil {
		t.Fatal("missing main folder: no error")
	}
}

// TestExternalRoot: places accepted for an extra folder outside the shared folders.
// TestExternalRoot : emplacements acceptés pour un dossier supplémentaire hors des dossiers partagés.
func TestExternalRoot(t *testing.T) {
	for p, want := range map[string]bool{
		"/volumeUSB1/usbshare": true, "/volumeUSB1": false, "/media/pi/DISK": true, "/mnt/nas": true, "/mnt": false,
		"/share/external/DEV3301_1": true, "/etc/ssl": false, "/run/media/user/Disk": true, "/home/user": false,
	} {
		if got := externalRoot(p); got != want {
			t.Errorf("externalRoot(%q) = %v", p, got)
		}
	}
	d := &Diag{Shares: t.TempDir()}
	for _, p := range []string{"", "relative/path", "/etc", "/mnt/.hidden/x"} {
		if _, err := d.ValidExtraDir(p); err == nil {
			t.Errorf("ValidExtraDir(%q) accepted", p)
		}
	}
	ok := filepath.Join(d.Shares, "Music")
	os.Mkdir(ok, 0o755)
	if got, err := d.ValidExtraDir(ok + "/"); err != nil || got != ok {
		t.Errorf("ValidExtraDir(share) = %q, %v", got, err)
	}
}
