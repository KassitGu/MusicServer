package main

// État du serveur et journaux, affichés sur la page de configuration.
//
// Pourquoi : le NAS n'a pas forcément SSH activé et le dossier du paquet est
// caché dans File Station. Sans cette page, impossible de savoir pourquoi
// le serveur s'est arrêté ou sur quelle carte réseau il s'annonce.
//
// Le programme tourne sans privilèges : il ne fait que LIRE l'état écrit par
// le superviseur (musique.sh, root) et déposer des « demandes » (changement de
// dossier musique, redémarrage) que le superviseur vérifie puis applique.
//
// 0.0.3 : Gerbera est retiré ; le journal affiché est celui du serveur
// Musique NAS (ce programme), écrit dans LogDir/serveur.log.

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

const (
	requestMusicDir = "demande-dossier"
	requestRestart  = "demande-redemarrage"
	requestName     = "demande-nom" // 0.0.4 : nom diffusé
	maxLogBytes     = 64 * 1024
)

type Diag struct {
	LogDir   string // journaux du serveur (compte non privilégié)
	StateDir string // état et journal du superviseur (root, lecture seule ici)
	Shares   string // racine des dossiers partagés (/share sur QTS)
}

type ServerState struct {
	Known       bool
	Stale       bool // état non rafraîchi : superviseur arrêté
	Values      map[string]string
	Shares      []string
	ServerLog   string
	SuperLog    string
	PendingDir  string
	PendingBoot bool
	PendingName string // 0.0.4
}

// readState lit etat.txt (clé=valeur, écrit par le superviseur).
func (d *Diag) readState() map[string]string {
	out := map[string]string{}
	f, err := os.Open(filepath.Join(d.StateDir, "etat.txt"))
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

// tail renvoie au plus n dernières lignes d'un fichier (lecture bornée).
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
	off := st.Size() - maxLogBytes
	if off < 0 {
		off = 0
	}
	buf := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	if off > 0 && len(lines) > 1 {
		lines = lines[1:] // première ligne probablement tronquée
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// Ancienne fonction (0.0.2) : adresse annoncée lue dans le journal de
// Gerbera. Inutile depuis 0.0.3 : l'adresse vient de l'annonce SSDP du
// programme lui-même.
// func boundTo(log string) string {
// 	const marker = "Server bound to:"
// 	i := strings.LastIndex(log, marker)
// 	if i < 0 {
// 		return ""
// 	}
// 	rest := log[i+len(marker):]
// 	if j := strings.IndexByte(rest, '\n'); j >= 0 {
// 		rest = rest[:j]
// 	}
// 	return strings.TrimSpace(rest)
// }

func (d *Diag) listShares() []string {
	ents, err := os.ReadDir(d.Shares)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		name := e.Name()
		// Volumes internes de QTS, pas des dossiers partagés.
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "CACHEDEV") || strings.HasSuffix(name, "_DATA") ||
			strings.HasPrefix(name, "external") || name == "HDA_DATA" {
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

func (d *Diag) State(dataDir string) ServerState {
	s := ServerState{Values: d.readState()}
	s.Known = len(s.Values) > 0
	if st, err := os.Stat(filepath.Join(d.StateDir, "etat.txt")); err == nil && time.Since(st.ModTime()) > 2*time.Minute {
		s.Stale = true
	}
	s.Shares = d.listShares()
	// Ancien fonctionnement (0.0.2, Gerbera), conservé pour référence :
	// s.GerberaLog = tail(filepath.Join(d.LogDir, "gerbera.log"), 60)
	// s.ErrorsLog = tail(filepath.Join(d.LogDir, "gerbera-erreurs.log"), 40)
	// s.BoundTo = boundTo(s.GerberaLog)
	s.ServerLog = tail(filepath.Join(d.LogDir, "serveur.log"), 80)
	s.SuperLog = tail(filepath.Join(d.StateDir, "superviseur.log"), 40)
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

// ValidMusicDir vérifie un dossier musique demandé depuis la page. Le
// superviseur refait les mêmes contrôles avant de l'appliquer.
func (d *Diag) ValidMusicDir(p string) (string, error) {
	if p == "" || len(p) > 512 || strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return "", errors.New("chemin invalide")
	}
	clean := filepath.Clean(p)
	root := filepath.Clean(d.Shares) + "/"
	if !strings.HasPrefix(clean+"/", root) || clean+"/" == root {
		return "", errors.New("le dossier doit être dans " + root)
	}
	st, err := os.Stat(clean)
	if err != nil || !st.IsDir() {
		return "", errors.New("dossier introuvable : " + clean)
	}
	return clean, nil
}

// writeRequest dépose une demande pour le superviseur (écriture atomique).
func writeRequest(dataDir, name string, content []byte) error {
	tmp := filepath.Join(dataDir, "."+name+".tmp")
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dataDir, name))
}
