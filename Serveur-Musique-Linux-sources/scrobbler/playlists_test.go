package main

// Tests of the server playlists, the backup and the translations.
// Tests des playlists du serveur, de la sauvegarde et des traductions.

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestUserPlaylists(t *testing.T) {
	dir := t.TempDir()
	u := NewUserPlaylists(dir)
	if _, err := u.Create("  ", nil); err != errPlaylistName {
		t.Fatalf("empty name accepted: %v", err)
	}
	if _, err := u.Create("a\nb", nil); err != errPlaylistName {
		t.Fatal("control character accepted")
	}
	id, err := u.Create(" Soirée ", []string{"/m/a.flac"})
	if err != nil || len(id) != 12 {
		t.Fatalf("creation: %q %v", id, err)
	}
	u.Append(id, []string{"/m/b.flac", "/m/c.flac"})
	u.Move(id, 2, -1) // a c b
	u.Move(id, 0, -1) // already first / déjà en tête
	u.Remove(id, 0)   // c b
	if err := u.Remove(id, 9); err == nil {
		t.Fatal("out-of-range index accepted")
	}
	u.Rename(id, "Dîner")
	// Saved on disk and read back. / Enregistré sur le disque et relu.
	p, ok := NewUserPlaylists(dir).Get(id)
	if !ok || p.Name != "Dîner" || strings.Join(p.Paths, ",") != "/m/c.flac,/m/b.flac" {
		t.Fatalf("read back: %+v", p)
	}
	if st, _ := os.Stat(filepath.Join(dir, "playlists.json")); st.Mode().Perm() != 0o600 {
		t.Fatalf("permissions: %v", st.Mode())
	}
	if raw := u.RawLists(); len(raw) != 1 || raw[0].Path != userPlaylistPfx+id || raw[0].Name != "Dîner" {
		t.Fatalf("publication: %+v", raw)
	}
	// Replace: invalid or duplicate ids are renewed, names checked.
	// Replace : identifiants invalides ou en double renouvelés, noms vérifiés.
	if err := u.Replace([]UserPlaylist{{ID: "x", Name: ""}}); err != errPlaylistName {
		t.Fatal("empty name restored")
	}
	if err := u.Replace([]UserPlaylist{{ID: "../../etc", Name: "A"}, {ID: id, Name: "B"}, {ID: id, Name: "C"}}); err != nil {
		t.Fatal(err)
	}
	all := u.All()
	if len(all) != 3 || all[0].ID == "../../etc" || all[1].ID != id || all[2].ID == id {
		t.Fatalf("restore: %+v", all)
	}
	if err := u.Delete(id); err != nil || len(u.All()) != 2 {
		t.Fatal("deletion")
	}
	if err := u.Delete(id); err != errPlaylistNotFound {
		t.Fatal("double deletion")
	}
}

// testApp: settings page over the test library.
// testApp : page de réglages sur la bibliothèque de test.
func testApp(t *testing.T) (*App, http.Handler, *LibraryService, *Store) {
	t.Helper()
	tl := buildTestLibrary(t)
	ls, sc, store := scanLibrary(t, tl)
	ls.upl = NewUserPlaylists(tl.data)
	app := &App{store: store, actors: map[string]*actor{}, kick: make(chan struct{}, 1), lib: ls, scanner: sc,
		top: ls.top, photos: ls.photos, ms: NewMediaServer("Musique NAS", "uuid:1", "t", ls)}
	return app, app.httpServer(":0").Handler, ls, store
}

// postForm sends a same-origin form POST from the LAN.
// postForm envoie un formulaire POST de même origine depuis le réseau local.
func postForm(h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://nas:8790")
	r.Host = "nas:8790"
	r.RemoteAddr = "192.168.68.56:5000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// getPage returns the body of a GET from the LAN.
// getPage renvoie le corps d'un GET depuis le réseau local.
func getPage(h http.Handler, target string) string {
	r := httptest.NewRequest("GET", target, nil)
	r.RemoteAddr = "192.168.68.56:5000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Body.String()
}

func TestPlaylistsPage(t *testing.T) {
	_, h, ls, _ := testApp(t)
	w := postForm(h, "/pl", url.Values{"op": {"create"}, "name": {"Jazz <du soir>"}})
	loc, _ := url.Parse(w.Header().Get("Location"))
	id := loc.Query().Get("pl")
	if w.Code != http.StatusSeeOther || id == "" || loc.Query().Get("ok") != "1" {
		t.Fatalf("creation: %d %s", w.Code, loc)
	}
	ix := ls.Index()
	kob := ix.AlbumByName("Miles Davis", "Kind of Blue")
	postForm(h, "/pl", url.Values{"op": {"addalbum"}, "id": {id}, "key": {kob.Key}})
	postForm(h, "/pl", url.Values{"op": {"addtrack"}, "id": {id}, "track": {kob.Tracks[0].ID}})
	postForm(h, "/pl", url.Values{"op": {"down"}, "id": {id}, "i": {"0"}})
	w = postForm(h, "/pl", url.Values{"op": {"addtrack"}, "id": {id}, "track": {"/etc/passwd"}})
	if loc, _ := url.Parse(w.Header().Get("Location")); loc.Query().Get("ok") == "1" {
		t.Fatal("unknown track accepted")
	}
	n := len(kob.Tracks) + 1
	p, _ := ls.upl.Get(id)
	if len(p.Paths) != n || p.Paths[1] != kob.Tracks[0].Path {
		t.Fatalf("contents: %+v", p.Paths)
	}
	// Published at once in the control app. / Publiée tout de suite dans l'appli.
	var found *Playlist
	for _, pl := range ls.Index().Playlists {
		if pl.Name == "Jazz <du soir>" {
			found = pl
		}
	}
	if found == nil || len(found.Tracks) != n {
		t.Fatalf("playlist not published: %+v", ls.Index().Playlists)
	}
	// Page: entries listed, name escaped, search offers albums and tracks.
	// Page : entrées listées, nom échappé, la recherche propose albums et morceaux.
	body := getPage(h, "/?p=playlists&pl="+id+"&qp=kind")
	if !strings.Contains(body, "Jazz &lt;du soir&gt;") || !strings.Contains(body, `name="key" value="`+kob.Key+`"`) ||
		!strings.Contains(body, "So What") || strings.Contains(body, "<du soir>") {
		t.Fatalf("page:\n%s", body)
	}
	postForm(h, "/pl", url.Values{"op": {"rename"}, "id": {id}, "name": {"Jazz"}})
	postForm(h, "/pl", url.Values{"op": {"remove"}, "id": {id}, "i": {"0"}})
	if p, _ := ls.upl.Get(id); p.Name != "Jazz" || len(p.Paths) != n-1 {
		t.Fatalf("rename/removal: %+v", p)
	}
	postForm(h, "/pl", url.Values{"op": {"delete"}, "id": {id}})
	if len(ls.upl.All()) != 0 {
		t.Fatal("deletion")
	}
	for _, pl := range ls.Index().Playlists {
		if pl.Name == "Jazz" {
			t.Fatal("deleted playlist still published")
		}
	}
}

// fakePlaylistService answers IdArray and ReadList like an OpenHome player.
// fakePlaylistService répond à IdArray et ReadList comme un lecteur OpenHome.
func fakePlaylistService(t *testing.T, entries [][3]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.Header.Get("SOAPACTION")
		var inner string
		switch {
		case strings.Contains(action, "#IdArray"):
			buf := make([]byte, 4*len(entries))
			for i := range entries {
				binary.BigEndian.PutUint32(buf[4*i:], uint32(i+1))
			}
			inner = "<Token>1</Token><Array>" + base64.StdEncoding.EncodeToString(buf) + "</Array>"
		case strings.Contains(action, "#ReadList"):
			var b strings.Builder
			b.WriteString("<TrackList>")
			// Reverse order: the server must restore the queue order.
			// Ordre inverse : le serveur doit remettre l'ordre de la file.
			for i := len(entries) - 1; i >= 0; i-- {
				b.WriteString("<Entry><Id>" + string(rune('1'+i)) + "</Id><Uri>" + xmlEsc(entries[i][0]) + "</Uri><Metadata>" +
					xmlEsc(`<DIDL-Lite xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/"><item><dc:title>`+
						xmlEsc(entries[i][1])+`</dc:title><upnp:artist>`+xmlEsc(entries[i][2])+`</upnp:artist></item></DIDL-Lite>`) +
					"</Metadata></Entry>")
			}
			b.WriteString("</TrackList>")
			inner = "<TrackList>" + xmlEsc(b.String()) + "</TrackList>"
		default:
			w.WriteHeader(500)
			return
		}
		name := action[strings.Index(action, "#")+1 : len(action)-1]
		io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:`+name+
			`Response xmlns:u="urn:av-openhome-org:service:Playlist:1">`+inner+`</u:`+name+`Response></s:Body></s:Envelope>`)
	}))
}

func TestQueueToPlaylist(t *testing.T) {
	app, h, ls, _ := testApp(t)
	kob := ls.Index().AlbumByName("Miles Davis", "Kind of Blue")
	srv := fakePlaylistService(t, [][3]string{
		{"http://192.168.68.54:49494/f/" + kob.Tracks[0].ID + ".flac", "", ""}, // this server / ce serveur
		{"http://other/x.flac", kob.Tracks[1].Title, "Miles Davis"},            // found by name / retrouvé par le nom
		{"http://radio.example/stream", "Unknown song", "Nobody"},              // skipped / ignoré
	})
	defer srv.Close()
	app.actors["uuid:salon"] = &actor{r: &Renderer{PlaylistURL: srv.URL, PlaylistType: "urn:av-openhome-org:service:Playlist:1"},
		tracker: &Tracker{Player: "Salon"}}
	if body := getPage(h, "/?p=playlists"); !strings.Contains(body, `<option value="uuid:salon">Salon</option>`) {
		t.Fatalf("player missing:\n%s", body)
	}
	w := postForm(h, "/pl", url.Values{"op": {"queue"}, "player": {"uuid:salon"}, "name": {"File du salon"}})
	loc, _ := url.Parse(w.Header().Get("Location"))
	p, ok := ls.upl.Get(loc.Query().Get("pl"))
	if !ok || p.Name != "File du salon" || len(p.Paths) != 2 || p.Paths[0] != kob.Tracks[0].Path || p.Paths[1] != kob.Tracks[1].Path {
		t.Fatalf("saved queue: %+v (%s)", p, loc)
	}
	if args := loc.Query()["ma"]; len(args) != 2 || args[0] != "2" || args[1] != "1" {
		t.Fatalf("message: %v", args)
	}
	w = postForm(h, "/pl", url.Values{"op": {"queue"}, "player": {"uuid:inconnu"}, "name": {"X"}})
	if loc, _ := url.Parse(w.Header().Get("Location")); loc.Query().Get("ok") == "1" {
		t.Fatal("unknown player accepted")
	}
}

// uploadBackup posts content as a backup file to /restore.
// uploadBackup envoie content comme fichier de sauvegarde à /restore.
func uploadBackup(h http.Handler, content []byte) *httptest.ResponseRecorder {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("back", "backup")
	fw, _ := mw.CreateFormFile("file", "backup.json")
	fw.Write(content)
	mw.Close()
	r := httptest.NewRequest("POST", "/restore", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Origin", "http://nas:8790")
	r.Host = "nas:8790"
	r.RemoteAddr = "192.168.68.56:5000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestBackupRestore(t *testing.T) {
	_, h, ls, store := testApp(t)
	kob := ls.Index().AlbumByName("Miles Davis", "Kind of Blue")
	store.UpdateConfig(func(c *Config) {
		c.APIKey, c.APISecret, c.SessionKey, c.Username = "0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210", "SK1", "melomane"
		c.FavoriteGenres, c.FavoriteAlbums, c.PhotoProvider, c.Lang = []string{"jazz"}, []string{kob.Key}, "deezer", langFR
	})
	ls.upl.Create("Dimanche", []string{kob.Tracks[0].Path})

	// Without the box ticked, no Last.fm secret in the file.
	// Sans la case cochée, aucun secret Last.fm dans le fichier.
	plain := postForm(h, "/backup", url.Values{})
	if plain.Code != 200 || !strings.Contains(plain.Header().Get("Content-Disposition"), "attachment") ||
		strings.Contains(plain.Body.String(), "fedcba98") || strings.Contains(plain.Body.String(), "SK1") {
		t.Fatalf("plain backup: %d %s", plain.Code, plain.Body.String())
	}
	full := postForm(h, "/backup", url.Values{"secrets": {"on"}})
	if !strings.Contains(full.Body.String(), "fedcba9876543210fedcba9876543210") {
		t.Fatal("Last.fm account missing from the full backup")
	}
	// A GET cannot download it (another site could trigger it).
	// Un GET ne peut pas la télécharger (un autre site pourrait le déclencher).
	if body := getPage(h, "/backup"); strings.Contains(body, "fedcba98") {
		t.Fatal("backup reachable with GET")
	}

	// Restore the plain backup over changed settings: the Last.fm account stays.
	// Restaurer la sauvegarde simple sur des réglages changés : le compte Last.fm reste.
	store.UpdateConfig(func(c *Config) { c.FavoriteGenres, c.FavoriteAlbums, c.PhotoProvider, c.Lang = nil, nil, "", langEN })
	ls.upl.Replace(nil)
	w := uploadBackup(h, plain.Body.Bytes())
	if loc, _ := url.Parse(w.Header().Get("Location")); loc.Query().Get("ok") != "1" || loc.Query().Get("p") != "backup" {
		t.Fatalf("restore: %s", w.Header().Get("Location"))
	}
	c := store.Config()
	if strings.Join(c.FavoriteGenres, ",") != "jazz" || len(c.FavoriteAlbums) != 1 || c.PhotoProvider != "deezer" || c.Lang != langFR ||
		c.SessionKey != "SK1" || len(ls.upl.All()) != 1 || ls.upl.All()[0].Name != "Dimanche" {
		t.Fatalf("after restore: %+v %+v", c, ls.upl.All())
	}

	// Invalid files change nothing. / Les fichiers invalides ne changent rien.
	var b backupFile
	json.Unmarshal(full.Body.Bytes(), &b)
	b.Lastfm.APIKey = "pas-une-cle"
	bad, _ := json.Marshal(b)
	for _, content := range [][]byte{[]byte("{}"), []byte("not json"), bad} {
		w := uploadBackup(h, content)
		if loc, _ := url.Parse(w.Header().Get("Location")); loc.Query().Get("ok") == "1" {
			t.Fatalf("invalid file accepted: %s", content)
		}
	}
	if store.Config().APIKey != "0123456789abcdef0123456789abcdef" {
		t.Fatal("key changed by a refused file")
	}
	// Full backup restores the account. / La sauvegarde complète restaure le compte.
	store.UpdateConfig(func(c *Config) { c.APIKey, c.APISecret, c.SessionKey, c.Username = "", "", "", "" })
	uploadBackup(h, full.Body.Bytes())
	if c := store.Config(); c.SessionKey != "SK1" || c.Username != "melomane" {
		t.Fatalf("account not restored: %+v", c)
	}
}

// TestFrenchTranslations checks that every text shown to the user has a
// French translation with the same format verbs.
// TestFrenchTranslations vérifie que chaque texte montré à l'utilisateur a
// une traduction française avec les mêmes verbes de format.
func TestFrenchTranslations(t *testing.T) {
	str := `"((?:[^"\\]|\\.)*)"`
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\btrf?\([a-zA-Z.()]+, ` + str),
		regexp.MustCompile(`redirect(?:Msg|OK)\(w, r, ` + str),
		regexp.MustCompile(`redirectTo\(w, r, [^,]+, [^,]+, [^,]+, ` + str),
		regexp.MustCompile(`redirectPL\(w, r, [^,]+, [^,]+, ` + str),
		regexp.MustCompile(`\bmsg(?:, args)? = ` + str),
		regexp.MustCompile(`\bid, msg = "", ` + str),
		regexp.MustCompile(`\{` + str + `, tag[A-Za-z]+\}`),
	}
	var keys []string
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || strings.HasPrefix(f, "i18n") {
			continue
		}
		b, _ := os.ReadFile(f)
		for i, re := range patterns {
			// Message variables are only looked for in the page handlers.
			// Les variables de message ne sont cherchées que dans les gestionnaires de la page.
			if i >= 4 && i <= 5 && !strings.HasPrefix(f, "web") {
				continue
			}
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				if m[1] != "" {
					keys = append(keys, m[1])
				}
			}
		}
	}
	page, _ := os.ReadFile("page.html")
	for _, m := range regexp.MustCompile(`\.[TF] `+str).FindAllStringSubmatch(string(page), -1) {
		keys = append(keys, m[1])
	}
	for _, v := range genreDisplay {
		keys = append(keys, v)
	}
	for _, e := range []error{errPlaylistNotFound, errPlaylistName, errTooManyPlaylists, errPlaylistFull, errAlbumNotFound,
		errTrackNotFound, errPlayerNotFound, errQueueEmpty, errBadRequest, errBadPath, errDirNotFound, errOutsideRoot, errNoKey} {
		keys = append(keys, e.Error())
	}
	if len(keys) < 200 {
		t.Fatalf("only %d texts found: patterns need reviewing", len(keys))
	}
	verbs := regexp.MustCompile(`%[a-z%]`)
	for _, k := range keys {
		var err error
		if k, err = unquoteGo(k); err != nil {
			t.Fatalf("%q: %v", k, err)
		}
		fr, ok := frText[k]
		if !ok {
			t.Errorf("missing translation: %q", k)
			continue
		}
		if strings.Join(verbs.FindAllString(k, -1), "") != strings.Join(verbs.FindAllString(fr, -1), "") {
			t.Errorf("different verbs: %q / %q", k, fr)
		}
	}
}

// unquoteGo turns the inside of a Go or template string literal into its value.
// unquoteGo transforme l'intérieur d'une chaîne Go ou de modèle en sa valeur.
func unquoteGo(s string) (string, error) {
	var v string
	err := json.Unmarshal([]byte(`"`+s+`"`), &v)
	return v, err
}
