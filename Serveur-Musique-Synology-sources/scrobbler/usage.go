package main

// Ressources utilisées par le programme, affichées sur la page :8790
// (0.0.4, demande de Bertrand) : mémoire réellement occupée (VmRSS) et
// processeur, en moyenne depuis le démarrage et sur la dernière minute.
// Lu dans /proc (Linux) : aucune commande lancée, aucun coût notable.

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const clockTicks = 100 // USER_HZ sous Linux (ARM 64 et x86-64)

var usageState struct {
	sync.Mutex
	start      time.Time
	startTicks int64
	lastAt     time.Time
	lastTicks  int64
	prevAt     time.Time
	prevTicks  int64
}

// cpuTicks : temps processeur consommé (utilisateur + système), en ticks.
func cpuTicks() (int64, bool) {
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, false
	}
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
	u, err1 := strconv.ParseInt(f[11], 10, 64) // utime (14e champ)
	k, err2 := strconv.ParseInt(f[12], 10, 64) // stime (15e champ)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return u + k, true
}

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

func init() {
	if t, ok := cpuTicks(); ok {
		usageState.start, usageState.startTicks = time.Now(), t
		usageState.lastAt, usageState.lastTicks = usageState.start, t
	}
}

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

func pct(ticks int64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(ticks) / clockTicks / d.Seconds() * 100
}

// processUsage : texte pour la page, vide si /proc est illisible.
func processUsage() string {
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
	s := fmt.Sprintf("mémoire %.0f Mo ; processeur %.1f %% d'un cœur en moyenne depuis le démarrage", float64(rss)/1024, avg)
	if !usageState.prevAt.IsZero() {
		recent := pct(now-usageState.prevTicks, time.Since(usageState.prevAt))
		s += fmt.Sprintf(", %.1f %% sur la dernière minute", recent)
	}
	return strings.ReplaceAll(s, ".", ",")
}
