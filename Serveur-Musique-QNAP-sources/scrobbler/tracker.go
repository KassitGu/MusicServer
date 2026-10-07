package main

// Règles d'écoute Last.fm : une piste est « scrobblée » si elle dure plus de
// 30 s et a été réellement jouée pendant la moitié de sa durée ou 4 minutes
// (le plus petit des deux). On compte le temps de lecture effectif à partir
// du compteur de secondes du lecteur, sans compter les sauts (seek).

import "time"

const (
	minTrackSeconds = 30
	maxThreshold    = 240
)

// Observation : un relevé périodique du lecteur.
type Observation struct {
	At         time.Time
	TrackCount int
	Duration   int
	Seconds    int
	// Meta n'est renseigné que lorsque la piste vient de changer.
	Meta       *TrackMeta
	SourceType string
}

// Event : action à mener vers Last.fm.
type Event struct {
	NowPlaying *Scrobble
	Scrobble   *Scrobble
}

// Tracker suit la lecture d'un lecteur.
type Tracker struct {
	Player     string
	PollPeriod time.Duration
	// AllowedSources : types de source OpenHome scrobblés (vide = tous).
	AllowedSources map[string]bool

	started     bool
	trackCount  int
	meta        TrackMeta
	duration    int
	lastSeconds int
	played      int
	startedAt   time.Time
	scrobbled   bool
	eligible    bool
	nowSent     bool
}

// NeedsMeta indique si le prochain relevé doit inclure les métadonnées.
func (t *Tracker) NeedsMeta(trackCount int) bool {
	return !t.started || trackCount != t.trackCount
}

func threshold(duration int) int {
	h := duration / 2
	if h > maxThreshold {
		return maxThreshold
	}
	return h
}

// Observe met à jour l'état et renvoie les événements Last.fm éventuels.
func (t *Tracker) Observe(o Observation) []Event {
	var evs []Event
	if !t.started || o.TrackCount != t.trackCount {
		t.started = true
		t.trackCount = o.TrackCount
		t.meta = TrackMeta{}
		if o.Meta != nil {
			t.meta = *o.Meta
		}
		t.duration = o.Duration
		t.lastSeconds = o.Seconds
		t.played = 0
		t.startedAt = o.At.Add(-time.Duration(o.Seconds) * time.Second)
		t.scrobbled = false
		t.nowSent = false
		t.eligible = t.meta.Title != "" && t.meta.Artist != "" &&
			(len(t.AllowedSources) == 0 || t.AllowedSources[o.SourceType])
		return evs
	}
	if o.Duration > 0 {
		t.duration = o.Duration
	}
	delta := o.Seconds - t.lastSeconds
	t.lastSeconds = o.Seconds
	// Lecture en cours : le compteur avance d'environ la période de relevé.
	// Un saut plus grand est un « seek » et n'est pas compté comme écouté.
	maxDelta := int(t.PollPeriod/time.Second)*2 + 2
	if delta <= 0 || delta > maxDelta {
		return evs
	}
	t.played += delta
	if !t.eligible || t.duration <= minTrackSeconds {
		return evs
	}
	s := t.scrobble()
	if !t.nowSent {
		t.nowSent = true
		evs = append(evs, Event{NowPlaying: &s})
	}
	if !t.scrobbled && t.played >= threshold(t.duration) {
		t.scrobbled = true
		evs = append(evs, Event{Scrobble: &s})
	}
	return evs
}

func (t *Tracker) scrobble() Scrobble {
	return Scrobble{
		Artist:      t.meta.Artist,
		Track:       t.meta.Title,
		Album:       t.meta.Album,
		AlbumArtist: t.meta.AlbumArtist,
		TrackNumber: t.meta.TrackNumber,
		Duration:    t.duration,
		Timestamp:   t.startedAt.Unix(),
		Player:      t.Player,
	}
}

// Current renvoie la piste suivie (pour la page d'état).
func (t *Tracker) Current() (TrackMeta, int, int, bool) {
	return t.meta, t.played, t.duration, t.scrobbled
}
