package main

// Last.fm listening rules: a track is scrobbled if it lasts more than 30 s
// and has actually been played for half its duration or 4 minutes (whichever
// is shorter). The actual playing time is counted from the player's seconds
// counter, leaving out jumps (seeks).
// Règles d'écoute Last.fm : une piste est « scrobblée » si elle dure plus de
// 30 s et a été réellement jouée pendant la moitié de sa durée ou 4 minutes
// (le plus petit des deux). On compte le temps de lecture effectif à partir
// du compteur de secondes du lecteur, sans compter les sauts (seek).

import "time"

const (
	minTrackSeconds = 30  // seconds / secondes
	maxThreshold    = 240 // seconds / secondes
)

// Observation is one periodic reading of the player.
// Observation est un relevé périodique du lecteur.
type Observation struct {
	At         time.Time
	TrackCount int
	Duration   int
	Seconds    int
	// Meta is only set when the track has just changed.
	// Meta n'est renseigné que lorsque la piste vient de changer.
	Meta       *TrackMeta
	SourceType string
}

// Event is an action to send to Last.fm.
// Event est une action à mener vers Last.fm.
type Event struct {
	NowPlaying *Scrobble
	Scrobble   *Scrobble
}

// Tracker follows the playback of one player.
// Tracker suit la lecture d'un lecteur.
type Tracker struct {
	Player     string
	PollPeriod time.Duration
	// AllowedSources: OpenHome source types that are scrobbled (empty = all).
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

// NeedsMeta reports whether the next reading must include the metadata.
// NeedsMeta indique si le prochain relevé doit inclure les métadonnées.
func (t *Tracker) NeedsMeta(trackCount int) bool {
	return !t.started || trackCount != t.trackCount
}

// threshold returns the seconds to play before scrobbling: half the
// duration, at most 4 minutes.
// threshold renvoie les secondes à jouer avant le scrobble : la moitié de
// la durée, au plus 4 minutes.
func threshold(duration int) int {
	h := duration / 2
	if h > maxThreshold {
		return maxThreshold
	}
	return h
}

// Observe updates the state and returns any Last.fm events. A change of
// track counter starts a new track and produces no event yet.
// Observe met à jour l'état et renvoie les événements Last.fm éventuels. Un
// changement du compteur de pistes démarre une nouvelle piste, sans
// événement pour l'instant.
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
	// While playing, the counter moves forward by about the polling period.
	// A larger jump is a seek and is not counted as listened.
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

// scrobble builds the Last.fm record of the current track.
// scrobble construit l'enregistrement Last.fm de la piste en cours.
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

// Current returns the tracked track, seconds played, duration and whether
// it was scrobbled (for the status page).
// Current renvoie la piste suivie, les secondes jouées, la durée et si elle
// a été scrobblée (pour la page d'état).
func (t *Tracker) Current() (TrackMeta, int, int, bool) {
	return t.meta, t.played, t.duration, t.scrobbled
}
