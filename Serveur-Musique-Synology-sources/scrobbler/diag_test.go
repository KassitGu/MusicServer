package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidMusicDir(t *testing.T) {
	root := t.TempDir()
	share := filepath.Join(root, "share")
	os.MkdirAll(filepath.Join(share, "Musique", "Jazz"), 0o755)
	os.MkdirAll(filepath.Join(root, "etc"), 0o755)
	os.WriteFile(filepath.Join(share, "fichier"), []byte("x"), 0o644)
	d := &Diag{Shares: share}
	ok := []string{share + "/Musique", share + "/Musique/Jazz/", share + "/Musique/./Jazz"}
	for _, p := range ok {
		if _, err := d.ValidMusicDir(p); err != nil {
			t.Errorf("%q refusé : %v", p, err)
		}
	}
	bad := []string{"", share, share + "/", share + "/../etc", root + "/etc", share + "/absent",
		share + "/fichier", share + "/Musique\n[x]", "relatif/Musique", share + "x/Musique"}
	for _, p := range bad {
		if _, err := d.ValidMusicDir(p); err == nil {
			t.Errorf("%q accepté", p)
		}
	}
}

func TestTail(t *testing.T) {
	f := filepath.Join(t.TempDir(), "g.log")
	var b strings.Builder
	for i := 0; i < 5000; i++ {
		b.WriteString("ligne de remplissage\n")
	}
	b.WriteString("info: IPv4: Server bound to: 192.168.68.54:49494\nderniere\n")
	os.WriteFile(f, []byte(b.String()), 0o644)
	got := tail(f, 3)
	if !strings.HasSuffix(got, "derniere") || strings.Count(got, "\n") != 2 {
		t.Fatalf("tail = %q", got)
	}
	if tail(filepath.Join(t.TempDir(), "absent"), 3) != "" {
		t.Fatal("fichier absent")
	}
}

func TestStatePageAndRequests(t *testing.T) {
	root := t.TempDir()
	share := filepath.Join(root, "share")
	logs := filepath.Join(root, "logs")
	os.MkdirAll(filepath.Join(share, "Music"), 0o755)
	os.MkdirAll(filepath.Join(share, "CACHEDEV1_DATA"), 0o755)
	os.MkdirAll(logs, 0o755)
	os.WriteFile(filepath.Join(logs, "etat.txt"), []byte("SERVEUR=actif\nMUSIC_DIR=/share/Music\nMUSIC_DIR_OK=non\nPLANTAGES=3\nNOM=Musique <NAS>\n"), 0o644)
	os.WriteFile(filepath.Join(logs, "serveur.log"), []byte("panic: <script>\n"), 0o644)
	store, err := OpenStore(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, actors: map[string]*actor{}, kick: make(chan struct{}, 1),
		diag: &Diag{LogDir: logs, StateDir: logs, Shares: share}}
	h := app.httpServer(":0").Handler

	// 0.0.4 : l'état du serveur est dans l'écran des réglages.
	req := httptest.NewRequest("GET", "/?p=reglages", nil)
	req.RemoteAddr = "192.168.68.56:5000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"introuvable ou illisible", "Musique &lt;NAS&gt;", "panic: &lt;script&gt;", share + "/Music"} {
		if !strings.Contains(body, want) {
			t.Errorf("page sans %q", want)
		}
	}
	if strings.Contains(body, "ne tourne plus") {
		t.Error("état récent signalé comme périmé")
	}
	old := time.Now().Add(-5 * time.Minute)
	os.Chtimes(filepath.Join(logs, "etat.txt"), old, old)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "ne tourne plus") {
		t.Error("état périmé non signalé")
	}
	if strings.Contains(body, "CACHEDEV1_DATA") {
		t.Error("volume interne listé")
	}

	post := func(path string, form url.Values, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Host = "nas:8790"
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		r.RemoteAddr = "192.168.68.56:5000"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	// Sans origine : refusé (CSRF).
	if w := post("/musicdir", url.Values{"music_dir": {share + "/Music"}}, ""); w.Code != http.StatusForbidden {
		t.Fatalf("CSRF : code %d", w.Code)
	}
	post("/musicdir", url.Values{"music_dir": {share + "/../etc"}}, "http://nas:8790")
	if _, err := os.Stat(filepath.Join(root, "data", requestMusicDir)); err == nil {
		t.Fatal("demande créée pour un chemin refusé")
	}
	post("/musicdir", url.Values{"music_dir": {share + "/Music/"}}, "http://nas:8790")
	b, err := os.ReadFile(filepath.Join(root, "data", requestMusicDir))
	if err != nil || string(b) != share+"/Music\n" {
		t.Fatalf("demande dossier = %q, %v", b, err)
	}
	post("/restart", nil, "http://nas:8790")
	if _, err := os.Stat(filepath.Join(root, "data", requestRestart)); err != nil {
		t.Fatal("demande de redémarrage absente")
	}
}
