package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestTrackQuality(t *testing.T) {
	cases := []struct {
		tr     Track
		valid  bool
		master bool
		format string
		label  string
	}{
		{Track{Mime: "audio/flac", Bits: 16, SampleRate: 44100}, true, false, "16/44.1", "CD"},
		{Track{Mime: "audio/flac", Bits: 16, SampleRate: 48000}, true, false, "16/48", "CD"},
		{Track{Mime: "audio/flac", Bits: 24, SampleRate: 44100}, true, true, "24/44.1", "STUDIO MASTER"},
		{Track{Mime: "audio/flac", Bits: 16, SampleRate: 96000}, true, true, "16/96", "STUDIO MASTER"},
		{Track{Mime: "audio/flac", Bits: 24, SampleRate: 192000}, true, true, "24/192", "STUDIO MASTER"},
		{Track{Mime: "audio/wav", Bits: 24, SampleRate: 176400}, true, true, "24/176.4", "STUDIO MASTER"},
		{Track{Mime: "audio/x-dsf", Bits: 1, SampleRate: 2822400}, true, true, "DSD64", "STUDIO MASTER"},
		{Track{Mime: "audio/x-dsf", Bits: 1, SampleRate: 5644800}, true, true, "DSD128", "STUDIO MASTER"},
		{Track{Mime: "audio/mpeg", SampleRate: 44100}, true, false, "44.1 kHz", "MP3"},        // 0.0.5 : badge MP3
		{Track{Mime: "audio/mp4", SampleRate: 44100}, false, false, "", ""},                   // AAC
		{Track{Mime: "audio/mp4", Bits: 16, SampleRate: 44100}, true, false, "16/44.1", "CD"}, // ALAC
		{Track{Mime: "audio/flac"}, false, false, "", ""},
	}
	for i, c := range cases {
		q := trackQuality(&c.tr)
		if q.Valid() != c.valid {
			t.Errorf("%d : valide = %v, attendu %v", i, q.Valid(), c.valid)
			continue
		}
		if !c.valid {
			continue
		}
		if q.Master != c.master || q.Format() != c.format || q.Label() != c.label {
			t.Errorf("%d : %+v -> %s %s", i, q, q.Label(), q.Format())
		}
	}
}

func TestAlbumQuality(t *testing.T) {
	tr := func(bits, rate int, mime string) *Track { return &Track{Mime: mime, Bits: bits, SampleRate: rate} }
	// Majorité de pistes 24/96 avec un bonus 16/44.1 : Studio Master.
	a := &Album{Tracks: []*Track{tr(24, 96000, "audio/flac"), tr(24, 96000, "audio/flac"), tr(16, 44100, "audio/flac")}}
	if q := albumQuality(a); !q.Master || q.Format() != "24/96" {
		t.Fatalf("majorité : %+v", q)
	}
	// À égalité, le meilleur format.
	a = &Album{Tracks: []*Track{tr(16, 44100, "audio/flac"), tr(24, 96000, "audio/flac")}}
	if q := albumQuality(a); q.Format() != "24/96" {
		t.Fatalf("égalité : %+v", q)
	}
	// Album en MP3 (0.0.5) : badge MP3, même avec une piste FLAC.
	a = &Album{Tracks: []*Track{tr(0, 44100, "audio/mpeg"), tr(0, 44100, "audio/mpeg"), tr(16, 44100, "audio/flac")}}
	if q := albumQuality(a); !q.MP3 || q.Lossless() || q.Label() != "MP3" {
		t.Fatalf("mp3 : %+v", q)
	}
	// À égalité MP3 / FLAC : le FLAC l'emporte.
	a = &Album{Tracks: []*Track{tr(0, 48000, "audio/mpeg"), tr(16, 44100, "audio/flac")}}
	if q := albumQuality(a); q.MP3 || q.Format() != "16/44.1" {
		t.Fatalf("égalité mp3 : %+v", q)
	}
	// AAC : toujours sans badge.
	a = &Album{Tracks: []*Track{tr(0, 44100, "audio/mp4")}}
	if albumQuality(a).Valid() {
		t.Fatal("aac")
	}
	if albumQuality(nil).Valid() || albumQuality(&Album{}).Valid() {
		t.Fatal("album vide")
	}
}

func coverImage(w, h int, c color.RGBA) *image.RGBA {
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(im.Pix); i += 4 {
		im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3] = c.R, c.G, c.B, 255
	}
	return im
}

// 0.0.5 : badge en bas à droite, toujours de la même taille, quelle que
// soit la pochette (petite, grande, non carrée), et rien d'autre ne change.
func TestBadgePlacement(t *testing.T) {
	qs := []Quality{{Bits: 16, Rate: 44100}, {Bits: 24, Rate: 192000, Master: true}, {Rate: 44100, MP3: true},
		{Bits: 1, Rate: 2822400, DSD: true, Master: true}}
	for _, q := range qs {
		for _, sz := range [][2]int{{800, 800}, {300, 300}, {1200, 900}, {500, 700}} {
			bg := color.RGBA{30, 60, 90, 255}
			var buf bytes.Buffer
			jpeg.Encode(&buf, coverImage(sz[0], sz[1], bg), &jpeg.Options{Quality: 100})
			b, err := badgedJPEG(buf.Bytes(), q)
			if err != nil {
				t.Fatal(err)
			}
			im, _, _ := image.Decode(bytes.NewReader(b))
			if im.Bounds().Dx() != badgeCanvas || im.Bounds().Dy() != badgeCanvas {
				t.Fatalf("%v : pochette %v, attendu %d px de côté", sz, im.Bounds(), badgeCanvas)
			}
			// Zone claire de la pastille (blanc) : position et taille fixes.
			minX, minY, maxX, maxY := badgeCanvas, badgeCanvas, 0, 0
			for y := 0; y < badgeCanvas; y++ {
				for x := 0; x < badgeCanvas; x++ {
					r, g, bl, _ := im.At(x, y).RGBA()
					if r>>8 > 200 && g>>8 > 200 && bl>>8 > 200 {
						minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
					}
				}
			}
			wantX0, wantY0 := badgeCanvas-badgeMargin-badgeW, badgeCanvas-badgeMargin-badgeH
			if absInt(minX-wantX0) > 4 || absInt(minY-wantY0) > 4 || absInt(maxX-(wantX0+badgeW-1)) > 4 || absInt(maxY-(wantY0+badgeH-1)) > 4 {
				t.Errorf("%s %v : pastille (%d,%d)-(%d,%d), attendu (%d,%d)-(%d,%d)", q.Label(), sz, minX, minY, maxX, maxY,
					wantX0, wantY0, wantX0+badgeW-1, wantY0+badgeH-1)
			}
		}
	}
	// Texte de la résolution (0.0.6, Inter) : caractères tous connus de la
	// police, et texte le plus long contenu dans la pastille.
	f := loadBadgeFont()
	if f == nil || f.CapHeight < 18 {
		t.Fatal("police des badges")
	}
	for _, q := range append(qs, Quality{Bits: 24, Rate: 176400, Master: true}, Quality{Bits: 1, Rate: 45158400, DSD: true, Master: true}) {
		for _, r := range q.Format() {
			if _, ok := f.Glyphs[string(r)]; !ok {
				t.Errorf("caractère %q absent de la police", r)
			}
		}
		if w := f.textWidth(q.Format()); w > badgeLogoW {
			t.Errorf("%s : %d px, plus large que la pastille", q.Format(), w)
		}
	}
	// Chaque qualité a son logo intégré au programme.
	for _, q := range qs {
		if badgeLogo(logoFile(q)) == nil {
			t.Errorf("logo %s absent", logoFile(q))
		}
	}
	// Image minuscule ou qualité inconnue : rien n'est dessiné.
	im := coverImage(40, 40, color.RGBA{200, 200, 200, 255})
	drawBadge(im, qs[0])
	drawBadge(coverImage(800, 800, color.RGBA{}), Quality{})
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestGetBadgedCache(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "cover.jpg")
	var buf bytes.Buffer
	jpeg.Encode(&buf, coverImage(1200, 1200, color.RGBA{10, 120, 40, 255}), nil)
	os.WriteFile(src, buf.Bytes(), 0o644)
	c := newArtCache(filepath.Join(dir, "cache"))
	ref := ArtRef{Path: src, Mime: "image/jpeg"}
	q := Quality{Bits: 24, Rate: 96000, Master: true}
	b1, err := c.GetBadged(ref, q)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := c.Get(ref)
	if bytes.Equal(b1, plain) {
		t.Fatal("pochette avec badge identique à la pochette simple")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b1))
	if err != nil || cfg.Width != 800 {
		t.Fatalf("taille %v %v", cfg, err)
	}
	if _, err := os.Stat(filepath.Join(c.dir, badgedCacheName(ref, q))); err != nil {
		t.Fatal("pas en cache")
	}
	b2, _ := c.GetBadged(ref, q)
	if !bytes.Equal(b1, b2) {
		t.Fatal("cache : contenu différent")
	}
	// Qualité inconnue : pochette simple.
	b3, _ := c.GetBadged(ref, Quality{})
	if !bytes.Equal(b3, plain) {
		t.Fatal("sans qualité : badge ajouté")
	}
	// La purge garde les deux versions des pochettes encore utilisées.
	c.Purge(map[string]bool{artCacheName(ref): true, badgedCacheName(ref, q): true})
	if ents, _ := os.ReadDir(c.dir); len(ents) != 2 {
		t.Fatalf("purge : %d fichiers", len(ents))
	}
}

// Aperçus pour validation visuelle : BADGE_APERCU=<dossier> <image>...
func TestBadgePreview(t *testing.T) {
	out := os.Getenv("BADGE_APERCU")
	if out == "" {
		t.Skip("aperçus non demandés")
	}
	srcs, _ := filepath.Glob(os.Getenv("BADGE_SOURCES"))
	qs := []Quality{{Bits: 16, Rate: 44100}, {Bits: 24, Rate: 96000, Master: true}, {Rate: 44100, MP3: true}, {Bits: 1, Rate: 2822400, DSD: true, Master: true}}
	for i, s := range srcs {
		b, err := os.ReadFile(s)
		if err != nil {
			t.Fatal(err)
		}
		small, err := scaledJPEG(b, artSize)
		if err != nil {
			t.Fatal(err)
		}
		q := qs[i%len(qs)]
		res, err := badgedJPEG(small, q)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(out, filepath.Base(s)+"-"+q.code()+".jpg"), res, 0o644)
	}
}
