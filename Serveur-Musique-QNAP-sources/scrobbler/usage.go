package main

// Resources used by the program, shown on the :8790 page: memory actually
// in use (VmRSS) and processor, on average since start-up and over the last
// minute. Read from /proc (Linux): no command is run, no noticeable cost.
// Ressources utilisées par le programme, affichées sur la page :8790 :
// mémoire réellement occupée (VmRSS) et processeur, en moyenne depuis le
// démarrage et sur la dernière minute. Lu dans /proc (Linux) : aucune
// commande lancée, aucun coût notable.

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const clockTicks = 100 // USER_HZ on Linux (ARM64 and x86-64) / USER_HZ sous Linux (ARM 64 et x86-64)

// usageState holds the start-up reference and the last two CPU samples.
// usageState garde la référence du démarrage et les deux dernières mesures du processeur.
var usageState struct {
	sync.Mutex
	start      time.Time
	startTicks int64
	lastAt     time.Time
	lastTicks  int64
	prevAt     time.Time
	prevTicks  int64
}

// cpuTicks returns the processor time used (user + system), in ticks.
// cpuTicks : temps processeur consommé (utilisateur + système), en ticks.
func cpuTicks() (int64, bool) {
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, false
	}
	// The program name (2nd field) is in parentheses and may contain spaces;
	// the following fields start after the last closing parenthesis.
	// Le nom du programme (2e champ) est entre parenthèses ; les champs
	// suivants commencent après la dernière parenthèse fermante.
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, false
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 13 {
		return 0, false
	}
	u, err1 := strconv.ParseInt(f[11], 10, 64) // utime (14th field / 14e champ)
	k, err2 := strconv.ParseInt(f[12], 10, 64) // stime (15th field / 15e champ)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return u + k, true
}

// rssKB returns the resident memory (VmRSS) in kB.
// rssKB renvoie la mémoire résidente (VmRSS) en ko.
func rssKB() (int64, bool) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "VmRSS:") {
			v := strings.Fields(sc.Text())
			if len(v) >= 2 {
				n, err := strconv.ParseInt(v[1], 10, 64)
				return n, err == nil
			}
		}
	}
	return 0, false
}

// init records the start-up reference.
// init enregistre la référence du démarrage.
func init() {
	if t, ok := cpuTicks(); ok {
		usageState.start, usageState.startTicks = time.Now(), t
		usageState.lastAt, usageState.lastTicks = usageState.start, t
	}
}

// sampleUsage, called every 30 s, keeps two samples to compute the recent
// load.
// sampleUsage : appelée toutes les 30 s, garde deux mesures pour calculer
// la charge récente.
func sampleUsage() {
	t, ok := cpuTicks()
	if !ok {
		return
	}
	usageState.Lock()
	usageState.prevAt, usageState.prevTicks = usageState.lastAt, usageState.lastTicks
	usageState.lastAt, usageState.lastTicks = time.Now(), t
	usageState.Unlock()
}

// pct converts ticks over a duration into a percentage of one core.
// pct convertit des ticks sur une durée en pourcentage d'un cœur.
func pct(ticks int64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(ticks) / clockTicks / d.Seconds() * 100
}

// processUsage describes memory and processor use in the given language,
// for the page; empty if /proc cannot be read.
// processUsage décrit la mémoire et le processeur utilisés, dans la langue
// donnée, pour la page ; vide si /proc est illisible.
func processUsage(lang string) string {
	rss, ok1 := rssKB()
	now, ok2 := cpuTicks()
	if !ok1 || !ok2 {
		return ""
	}
	usageState.Lock()
	defer usageState.Unlock()
	if usageState.start.IsZero() {
		return ""
	}
	avg := pct(now-usageState.startTicks, time.Since(usageState.start))
	s := trf(lang, "memory %s MB; processor %s %% of one core on average since start-up",
		fmt.Sprintf("%.0f", float64(rss)/1024), decimal(lang, avg))
	if !usageState.prevAt.IsZero() {
		recent := pct(now-usageState.prevTicks, time.Since(usageState.prevAt))
		s += trf(lang, ", %s %% over the last minute", decimal(lang, recent))
	}
	return s
}

// decimal formats v with one decimal, with a decimal comma in French.
// decimal écrit v avec une décimale, avec une virgule en français.
func decimal(lang string, v float64) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	if lang == langFR {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}
