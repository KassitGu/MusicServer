package main

// Badge de qualité sur les pochettes d'album, en bas à droite.
//
// 0.0.4 (demande de Bertrand, 2026-10-06) : pastille « CD » argent ou
// « STUDIO MASTER » dorée, dessinée en proportion de la pochette.
// 0.0.5 (demande du 2026-10-07) : logos choisis par Bertrand (MP3, CD Audio,
// Hi-Res Audio pour les studio masters) avec, dessous, la résolution comme
// en 0.0.4 (« 24/96 », « 16/44.1 », « DSD64 », « 44.1 kHz » pour le MP3).
// La taille est désormais FIXE : en 0.0.4 elle suivait le côté de la
// pochette réduite, si bien qu'une petite pochette (300 px) ou une pochette
// non carrée recevait un badge plus petit, parfois illisible une fois
// affiché. Maintenant la pochette avec badge est toujours ramenée à un
// carré de badgeCanvas pixels (agrandie si besoin, bandes sombres si elle
// n'est pas carrée) et le badge y a toujours les mêmes dimensions.
// Pas de badge pour l'AAC ni les autres formats avec pertes : aucun logo
// n'a été fourni pour eux.
//
// Le badge est posé sur la pochette réduite (art.go) et gardé en cache sous
// un autre nom : la pochette sans badge reste servie pour les genres, les
// photos d'artiste de secours et l'écran de lecture.

import (
	"bytes"
	"embed"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png" // logos des badges
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Quality : format dominant d'un album.
type Quality struct {
	Bits   int
	Rate   int
	DSD    bool
	Master bool
	MP3    bool // 0.0.5 : badge MP3 (avec pertes, pas de profondeur en bits)
}

func (q Quality) Valid() bool { return q.Rate > 0 }

// Lossless : vrai pour un format sans perte (FLAC, ALAC, WAV, AIFF, DSD).
func (q Quality) Lossless() bool { return q.Valid() && !q.MP3 }

func (q Quality) Label() string {
	switch {
	case q.MP3:
		return "MP3"
	case q.Master:
		return "STUDIO MASTER"
	}
	return "CD"
}

// Format : « 24/96 », « 16/44.1 », « DSD64 », « 44.1 kHz » (MP3).
func (q Quality) Format() string {
	khz := strconv.FormatFloat(float64(q.Rate)/1000, 'f', -1, 64)
	switch {
	case q.DSD:
		return "DSD" + strconv.Itoa(q.Rate/44100)
	case q.MP3:
		return khz + " kHz"
	}
	return strconv.Itoa(q.Bits) + "/" + khz
}

// code : partie du nom de fichier en cache. « b3 » = version du dessin
// (b1 en 0.0.4, b2 en 0.0.5) : à changer si le badge change, pour ne pas resservir
// l'ancien ; les anciens fichiers sont purgés à l'indexation suivante.
func (q Quality) code() string {
	k := "cd"
	switch {
	case q.MP3:
		k = "mp3"
	case q.DSD:
		k = "dsd"
	case q.Master:
		k = "sm"
	}
	return "b3-" + k + "-" + strconv.Itoa(q.Bits) + "-" + strconv.Itoa(q.Rate)
}

// trackQuality : qualité d'une piste ; invalide pour un format avec pertes
// autre que le MP3 (AAC : pas de profondeur en bits ni de logo).
func trackQuality(t *Track) Quality {
	if t == nil || t.SampleRate <= 0 {
		return Quality{}
	}
	if t.Mime == "audio/x-dsf" || t.Mime == "audio/x-dff" || t.SampleRate >= 2822400 {
		return Quality{Bits: 1, Rate: t.SampleRate, DSD: true, Master: true}
	}
	if t.Mime == "audio/mpeg" {
		return Quality{Rate: t.SampleRate, MP3: true}
	}
	if t.Bits <= 0 {
		return Quality{}
	}
	return Quality{Bits: t.Bits, Rate: t.SampleRate, Master: t.Bits > 16 || t.SampleRate > 48000}
}

// albumQuality : format le plus fréquent parmi les pistes (à égalité, le
// meilleur). Un album mêlant MP3 et FLAC prend le format majoritaire.
func albumQuality(a *Album) Quality {
	if a == nil {
		return Quality{}
	}
	count := map[Quality]int{}
	var best Quality
	bestN := 0
	for _, t := range a.Tracks {
		q := trackQuality(t)
		count[q]++
		n := count[q]
		if n > bestN || (n == bestN && better(q, best)) {
			best, bestN = q, n
		}
	}
	return best
}

func better(x, y Quality) bool {
	if x.Valid() != y.Valid() {
		return x.Valid()
	}
	if x.MP3 != y.MP3 {
		return !x.MP3
	}
	if x.DSD != y.DSD {
		return x.DSD
	}
	if x.Rate != y.Rate {
		return x.Rate > y.Rate
	}
	return x.Bits > y.Bits
}

// ---------------------------------------------------------------------------
// Dessin (0.0.5). Ancien dessin 0.0.4 : badge_ancien.go.

// Logos fournis par Bertrand, intégrés au programme (aucun téléchargement
// sur le NAS). Marques de leurs propriétaires ; usage personnel.
//
//go:embed badges/*.png badges/police.json
var badgeFS embed.FS

// Dimensions fixes, en pixels, sur la pochette carrée de badgeCanvas px.
// L'appli affiche la pochette à la taille de la tuile : le badge occupe
// donc toujours la même part de la tuile (20 % de la largeur). Les logos
// fournis sont carrés ou en hauteur : zone du logo presque carrée.
const (
	badgeCanvas = artSize // 800
	badgeW      = 160     // pastille
	badgeH      = 146
	badgeRadius = 14
	badgeBorder = 2
	badgeMargin = 18 // écart avec les bords de la pochette
	badgePad    = 8
	badgeLogoW  = badgeW - 2*badgePad // 144
	badgeLogoH  = 100
	badgeShadow = 3
)

// Police de la résolution (0.0.6) : Inter Bold, rendue d'avance et lissée
// (badges/police.png + police.json, produits par
// paquet/outils-build/police_badge.py). Remplace la police « pixels » 5x7
// de la 0.0.5 (jugée peu jolie par Bertrand, 2026-10-07 ; voir
// badge_ancien.go pour l'ancienne police).
type fontGlyph struct {
	X     int `json:"x"`
	W     int `json:"w"`
	OX    int `json:"ox"`
	Adv64 int `json:"adv64"`
}

type badgeFont struct {
	Height    int                  `json:"height"`
	Ascent    int                  `json:"ascent"`
	CapHeight int                  `json:"capHeight"`
	Glyphs    map[string]fontGlyph `json:"glyphs"`
	atlas     *image.Alpha
}

var (
	fontOnce sync.Once
	fontData *badgeFont
)

func loadBadgeFont() *badgeFont {
	fontOnce.Do(func() {
		var f badgeFont
		b, err := badgeFS.ReadFile("badges/police.json")
		if err == nil {
			err = json.Unmarshal(b, &f)
		}
		var im image.Image
		if err == nil {
			var r io.ReadCloser
			if r, err = badgeFS.Open("badges/police.png"); err == nil {
				im, _, err = image.Decode(r)
				r.Close()
			}
		}
		if err != nil {
			logf("badge : police illisible : %v", err)
			return
		}
		// Le gris de l'atlas sert de masque : il faut une image Alpha (une
		// image Gray est opaque partout en tant que masque).
		g := image.NewGray(im.Bounds())
		draw.Draw(g, g.Bounds(), im, im.Bounds().Min, draw.Src)
		f.atlas = &image.Alpha{Pix: g.Pix, Stride: g.Stride, Rect: g.Rect}
		fontData = &f
	})
	return fontData
}

// textWidth : largeur du texte en pixels (avances cumulées).
func (f *badgeFont) textWidth(s string) int {
	w := 0
	for _, r := range s {
		w += f.Glyphs[string(r)].Adv64
	}
	return (w + 32) / 64
}

// drawText : texte de couleur c, ligne de base à baseline, à partir de x.
func (f *badgeFont) drawText(dst draw.Image, s string, x, baseline int, c color.RGBA) {
	pen := x * 64
	top := baseline - f.Ascent
	src := image.NewUniform(c)
	for _, r := range s {
		g, ok := f.Glyphs[string(r)]
		if !ok {
			continue
		}
		gx := (pen+32)/64 + g.OX
		mask := f.atlas.SubImage(image.Rect(g.X, 0, g.X+g.W, f.Height))
		draw.DrawMask(dst, image.Rect(gx, top, gx+g.W, top+f.Height), src, image.Point{}, mask, image.Pt(g.X, 0), draw.Over)
		pen += g.Adv64
	}
}

var (
	plateFill   = color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
	plateBorder = color.RGBA{0x8A, 0x8F, 0x96, 0xFF}
	plateText   = color.RGBA{0x1A, 0x1C, 0x20, 0xFF}
)

const badgeSS = 4 // suréchantillonnage : coins de la pastille lissés

// roundedMask : vrai si (x, y) est dans le rectangle w×h aux coins de rayon r.
func roundedMask(x, y, w, h, r int) bool {
	dx, dy := 0, 0
	if x < r {
		dx = r - x
	} else if x >= w-r {
		dx = x - (w - r - 1)
	}
	if y < r {
		dy = r - y
	} else if y >= h-r {
		dy = y - (h - r - 1)
	}
	return dx*dx+dy*dy <= r*r
}

func logoFile(q Quality) string {
	switch {
	case q.MP3:
		return "badges/mp3.png"
	case q.Master:
		return "badges/hires.png"
	}
	return "badges/cd.png"
}

// Logos ramenés une fois pour toutes à la zone badgeLogoW×badgeLogoH, posés
// sur le blanc de la pastille (transparence aplatie).
var (
	logoMu    sync.Mutex
	logoCache = map[string]*image.RGBA{}
)

func badgeLogo(name string) *image.RGBA {
	logoMu.Lock()
	defer logoMu.Unlock()
	if im, ok := logoCache[name]; ok {
		return im
	}
	var out *image.RGBA
	if f, err := badgeFS.Open(name); err == nil {
		src, _, derr := image.Decode(f)
		f.Close()
		if derr == nil {
			out = fitOnWhite(src, badgeLogoW, badgeLogoH)
		} else {
			logf("badge : logo %s illisible : %v", name, derr)
		}
	}
	logoCache[name] = out
	return out
}

// fitOnWhite : image ramenée (réduite ou agrandie, proportions gardées) dans
// un cadre w×h, transparence aplatie sur du blanc.
func fitOnWhite(src image.Image, w, h int) *image.RGBA {
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil
	}
	flat := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(flat, flat.Bounds(), image.NewUniform(plateFill), image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), src, b.Min, draw.Over)
	nw, nh := w, b.Dy()*w/b.Dx()
	if nh > h {
		nw, nh = b.Dx()*h/b.Dy(), h
	}
	return resizeRGBA(flat, max(1, nw), max(1, nh))
}

// resizeRGBA : réduction par moyenne de zones (downscale, art.go) ou
// agrandissement bilinéaire.
func resizeRGBA(src *image.RGBA, nw, nh int) *image.RGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if nw == w && nh == h {
		return src
	}
	if nw <= w && nh <= h {
		if r, ok := downscale(src, max(nw, nh)).(*image.RGBA); ok && r.Bounds().Dx() == nw && r.Bounds().Dy() == nh {
			return r
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		fy := (float64(y)+0.5)*float64(h)/float64(nh) - 0.5
		y0 := int(fy)
		if fy < 0 {
			fy, y0 = 0, 0
		}
		y1 := min(y0+1, h-1)
		ty := fy - float64(y0)
		for x := 0; x < nw; x++ {
			fx := (float64(x)+0.5)*float64(w)/float64(nw) - 0.5
			x0 := int(fx)
			if fx < 0 {
				fx, x0 = 0, 0
			}
			x1 := min(x0+1, w-1)
			tx := fx - float64(x0)
			p00, p01 := src.PixOffset(x0, y0), src.PixOffset(x1, y0)
			p10, p11 := src.PixOffset(x0, y1), src.PixOffset(x1, y1)
			d := dst.PixOffset(x, y)
			for c := 0; c < 4; c++ {
				top := float64(src.Pix[p00+c])*(1-tx) + float64(src.Pix[p01+c])*tx
				bot := float64(src.Pix[p10+c])*(1-tx) + float64(src.Pix[p11+c])*tx
				dst.Pix[d+c] = uint8(top*(1-ty) + bot*ty + 0.5)
			}
		}
	}
	return dst
}

// badgeLayer : la pastille (fond transparent autour des coins arrondis),
// toujours badgeW×badgeH : logo en haut, résolution en dessous.
func badgeLayer(q Quality) *image.NRGBA {
	k := badgeSS
	w, h, r, bd := badgeW*k, badgeH*k, badgeRadius*k, badgeBorder*k
	hi := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !roundedMask(x, y, w, h, r) {
				continue
			}
			c := plateFill
			if !roundedMask(x-bd, y-bd, w-2*bd, h-2*bd, max(0, r-bd)) ||
				x < bd || y < bd || x >= w-bd || y >= h-bd {
				c = plateBorder
			}
			hi.SetRGBA(x, y, c)
		}
	}
	// Réduction k×k avec alpha : bords anti-crénelés.
	out := image.NewNRGBA(image.Rect(0, 0, badgeW, badgeH))
	for y := 0; y < badgeH; y++ {
		for x := 0; x < badgeW; x++ {
			var sr, sg, sb, sa int
			for j := 0; j < k; j++ {
				for i := 0; i < k; i++ {
					c := hi.RGBAAt(x*k+i, y*k+j) // prémultiplié
					sr += int(c.R)
					sg += int(c.G)
					sb += int(c.B)
					sa += int(c.A)
				}
			}
			if sa == 0 {
				continue
			}
			out.SetNRGBA(x, y, color.NRGBA{uint8(sr * 255 / sa), uint8(sg * 255 / sa), uint8(sb * 255 / sa), uint8(sa / (k * k))})
		}
	}
	// Logo centré dans sa zone, résolution centrée dessous.
	pad := badgePad
	if logo := badgeLogo(logoFile(q)); logo != nil {
		lw, lh := logo.Bounds().Dx(), logo.Bounds().Dy()
		x0, y0 := pad+(badgeLogoW-lw)/2, pad+(badgeLogoH-lh)/2
		draw.Draw(out, image.Rect(x0, y0, x0+lw, y0+lh), logo, image.Point{}, draw.Over)
	}
	// Résolution centrée dans la bande sous le logo (chiffres de
	// CapHeight px). Ancien code 0.0.5 : drawText de la police 5x7.
	if f := loadBadgeFont(); f != nil {
		txt := q.Format()
		band := pad + badgeLogoH
		baseline := band + (badgeH-band+f.CapHeight)/2 - 1
		f.drawText(out, txt, (badgeW-f.textWidth(txt))/2, baseline, plateText)
	}
	return out
}

// squareCanvas : pochette ramenée à un carré de côté n (agrandie ou
// réduite, proportions gardées, bandes sombres si elle n'est pas carrée).
func squareCanvas(im image.Image, n int) *image.RGBA {
	b := im.Bounds()
	flat := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(flat, flat.Bounds(), im, b.Min, draw.Src)
	w, h := b.Dx(), b.Dy()
	nw, nh := n, max(1, h*n/max(1, w))
	if h > w {
		nw, nh = max(1, w*n/h), n
	}
	sc := resizeRGBA(flat, nw, nh)
	if nw == n && nh == n {
		return sc
	}
	dst := image.NewRGBA(image.Rect(0, 0, n, n))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.RGBA{0x11, 0x11, 0x11, 0xFF}), image.Point{}, draw.Src)
	x0, y0 := (n-nw)/2, (n-nh)/2
	draw.Draw(dst, image.Rect(x0, y0, x0+nw, y0+nh), sc, image.Point{}, draw.Src)
	return dst
}

// drawBadge pose le badge en bas à droite d'une pochette de badgeCanvas px,
// avec une ombre légère pour rester visible sur une pochette claire.
func drawBadge(img *image.RGBA, q Quality) {
	if !q.Valid() {
		return
	}
	b := img.Bounds()
	if b.Dx() < badgeW+2*badgeMargin || b.Dy() < badgeH+2*badgeMargin {
		return // ne devrait pas arriver : la pochette est ramenée à badgeCanvas
	}
	layer := badgeLayer(q)
	x0, y0 := b.Max.X-badgeW-badgeMargin, b.Max.Y-badgeH-badgeMargin
	shadow := image.NewUniform(color.NRGBA{0, 0, 0, 110})
	draw.DrawMask(img, image.Rect(x0+badgeShadow, y0+badgeShadow, x0+badgeShadow+badgeW, y0+badgeShadow+badgeH),
		shadow, image.Point{}, layer, image.Point{}, draw.Over)
	draw.Draw(img, image.Rect(x0, y0, x0+badgeW, y0+badgeH), layer, image.Point{}, draw.Over)
}

// badgedJPEG : pochette réduite (JPEG) ramenée au carré badgeCanvas, puis
// badge. (0.0.4 : badge posé sur l'image à sa taille d'origine.)
func badgedJPEG(src []byte, q Quality) ([]byte, error) {
	im, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	rgba := squareCanvas(im, badgeCanvas)
	drawBadge(rgba, q)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, rgba, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func badgedCacheName(a ArtRef, q Quality) string {
	return strings.TrimSuffix(artCacheName(a), ".jpg") + "-" + q.code() + ".jpg"
}

// GetBadged renvoie la pochette réduite avec le badge de qualité (sans
// badge si la qualité est inconnue).
func (c *artCache) GetBadged(a ArtRef, q Quality) ([]byte, error) {
	if !q.Valid() {
		return c.Get(a)
	}
	p := filepath.Join(c.dir, badgedCacheName(a, q))
	if b, err := os.ReadFile(p); err == nil {
		return b, nil
	}
	base, err := c.Get(a)
	if err != nil {
		return nil, err
	}
	c.sem <- struct{}{}
	out, err := badgedJPEG(base, q)
	<-c.sem
	if err != nil {
		return nil, err
	}
	// Fichier temporaire unique : deux demandes simultanées ne s'écrivent
	// pas l'une sur l'autre.
	if f, err := os.CreateTemp(c.dir, "badge-*.tmp"); err == nil {
		_, werr := f.Write(out)
		f.Close()
		if werr == nil {
			os.Rename(f.Name(), p)
		} else {
			os.Remove(f.Name())
		}
	}
	return out, nil
}
