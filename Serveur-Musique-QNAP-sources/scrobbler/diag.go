package main

// Server state and logs, shown on the settings page.
// État du serveur et journaux, affichés sur la page de réglages.
//
// Why: the NAS may not have SSH enabled and the package folder is hidden in
// File Station. Without this page there is no way to see why the server
// stopped or which network card it announces itself on.
// Pourquoi : le NAS n'a pas forcément SSH activé et le dossier du paquet est
// caché dans File Station. Sans cette page, impossible de savoir pourquoi le
// serveur s'est arrêté ou sur quelle carte réseau il s'annonce.
//
// The program runs unprivileged: it only READS the state written by the
// supervisor (musique.sh, root) and drops "requests" (music folder change,
// server name, restart) that the supervisor checks again before applying.
// Le programme tourne sans privilèges : il ne fait que LIRE l'état écrit par
// le superviseur (musique.sh, root) et déposer des « demandes » (dossier
// musique, nom diffusé, redémarrage) que le superviseur revérifie avant de
// les appliquer.

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// File names shared with the supervisor (musique.sh): requests dropped in the
// data folder, state and logs read from the log folders.
// Noms de fichiers partagés avec le superviseur (musique.sh) : demandes
// déposées dans le dossier de données, état et journaux lus dans les dossiers
// de journaux.
const (
	requestMusicDir = "request-music-dir"
	requestRestart  = "request-restart"
	requestName     = "request-name"
	stateFile       = "state.txt"
	serverLogFile   = "server.log"
	superLogFile    = "supervisor.log"
	maxLogBytes     = 64 * 1024
)

// Diag gives the settings page read access to the supervisor's state and logs.
// Diag donne à la page de réglages l'accès en lecture à l'état et aux
// journaux du superviseur.
type Diag struct {
	LogDir   string // server logs (unprivileged account) / journaux du serveur (compte non privilégié)
	StateDir string // supervisor state and log (root, read-only here) / état et journal du superviseur (root, lecture seule ici)
	Shares   string // root of the shared folders (/share on QTS) / racine des dossiers partagés (/share sur QTS)
}

// ServerState is what the supervisor last reported, plus the requests still
// waiting to be applied.
// ServerState : ce que le superviseur a indiqué en dernier, plus les
// demandes pas encore appliquées.
type ServerState struct {
	Known        bool
	Stale        bool // state not refreshed: supervisor stopped / état non rafraîchi : superviseur arrêté
	Name         string
	MusicDir     string
	MusicDirOK   bool
	Crashes      string
	LastExitTime string
	LastExitCode string // exit status, described by exitText / code de sortie, décrit par exitText
	Updated      string
	Shares       []string
	ServerLog    string
	SuperLog     string
	PendingDir   string
	PendingBoot  bool
	PendingName  string
}

// readState parses state.txt (key=value lines written by the supervisor).
// readState lit state.txt (lignes clé=valeur écrites par le superviseur).
func (d *Diag) readState() map[string]string {
	out := map[string]string{}
	f, err := os.Open(filepath.Join(d.StateDir, stateFile))
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, 16*1024))
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// tail returns at most the last n lines of a file, reading no more than
// maxLogBytes from its end.
// tail renvoie au plus les n dernières lignes d'un fichier, sans lire plus
// de maxLogBytes depuis la fin.
func tail(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	off := max(st.Size()-maxLogBytes, 0)
	buf := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	if off > 0 && len(lines) > 1 {
		lines = lines[1:] // first line is probably cut / première ligne probablement tronquée
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// listShares lists the shared folders a music folder can be chosen from.
// listShares liste les dossiers partagés parmi lesquels choisir le dossier musique.
func (d *Diag) listShares() []string {
	ents, err := os.ReadDir(d.Shares)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		name := e.Name()
		// QTS internal volumes, not shared folders.
		// Volumes internes de QTS, pas des dossiers partagés.
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "CACHEDEV") || strings.HasSuffix(name, "_DATA") ||
			strings.HasPrefix(name, "external") {
			continue
		}
		p := filepath.Join(d.Shares, name)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// State gathers everything the "Music server" tab shows.
// State rassemble tout ce qu'affiche l'onglet « Serveur de musique ».
func (d *Diag) State(dataDir string) ServerState {
	v := d.readState()
	s := ServerState{Known: len(v) > 0, Name: v["NAME"], MusicDir: v["MUSIC_DIR"], MusicDirOK: v["MUSIC_DIR_OK"] == "yes",
		Crashes: v["CRASHES"], LastExitTime: v["LAST_EXIT_TIME"], LastExitCode: v["LAST_EXIT_CODE"], Updated: v["UPDATED"]}
	if st, err := os.Stat(filepath.Join(d.StateDir, stateFile)); err == nil && time.Since(st.ModTime()) > 2*time.Minute {
		s.Stale = true
	}
	s.Shares = d.listShares()
	s.ServerLog = tail(filepath.Join(d.LogDir, serverLogFile), 80)
	s.SuperLog = tail(filepath.Join(d.StateDir, superLogFile), 40)
	if b, err := os.ReadFile(filepath.Join(dataDir, requestMusicDir)); err == nil {
		s.PendingDir = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile(filepath.Join(dataDir, requestName)); err == nil {
		s.PendingName = strings.TrimSpace(string(b))
	}
	_, err := os.Stat(filepath.Join(dataDir, requestRestart))
	s.PendingBoot = err == nil
	return s
}

// exitText describes the program's last exit status.
// exitText décrit le dernier code de sortie du programme.
func exitText(lang, code string) string {
	switch code {
	case "":
		return ""
	case "0":
		return tr(lang, "normal stop (code 0)")
	case "1":
		return tr(lang, "start-up error (code 1, see the server log)")
	case "2":
		return tr(lang, "internal error (code 2, see the server log)")
	case "137":
		return tr(lang, "killed (code 137, out of memory?)")
	case "139":
		return tr(lang, "memory error (code 139)")
	}
	return trf(lang, "code %s", code)
}

var (
	errBadPath     = errors.New("invalid path")
	errDirNotFound = errors.New("folder not found")
	errOutsideRoot = errors.New("the folder must be inside the shared folders")
)

// ValidMusicDir checks a music folder requested from the page. The
// supervisor runs the same checks again before applying it.
// ValidMusicDir vérifie un dossier musique demandé depuis la page. Le
// superviseur refait les mêmes contrôles avant de l'appliquer.
func (d *Diag) ValidMusicDir(p string) (string, error) {
	if p == "" || len(p) > 512 || strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return "", errBadPath
	}
	clean := filepath.Clean(p)
	root := filepath.Clean(d.Shares) + "/"
	if !strings.HasPrefix(clean+"/", root) || clean+"/" == root {
		return "", errOutsideRoot
	}
	st, err := os.Stat(clean)
	if err != nil || !st.IsDir() {
		return "", errDirNotFound
	}
	return clean, nil
}

// writeRequest drops a request for the supervisor (atomic write).
// writeRequest dépose une demande pour le superviseur (écriture atomique).
func writeRequest(dataDir, name string, content []byte) error {
	tmp := filepath.Join(dataDir, "."+name+".tmp")
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dataDir, name))
}
