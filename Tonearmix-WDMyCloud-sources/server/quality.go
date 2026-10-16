package main

// Quality badge drawn in the bottom-right corner of album covers: a dark,
// translucent band holding the format logo on the left (CD, Hi-Res Audio
// for studio masters and DSD, or MP3) and the resolution on the right
// ("16/44.1", "24/96", "DSD64", "44.1 kHz" for MP3).
// The round CD logo sits in the rounded left end of the band; the square
// Hi-Res and MP3 logos run along a straight left edge, full height.
// The band has a FIXED size: the badged cover is always brought to a square
// of badgeCanvas pixels (enlarged if needed, dark bands if it is not square),
// so a small or non-square cover never gets a smaller, unreadable badge.
// No badge for AAC or other lossy formats: there is no logo for them.
//
// The badge is drawn on the reduced cover (art.go) and cached under another
// name: the unbadged cover is still served for genres, fallback artist
// photos and the playback screen.
//
// Badge de qualité dessiné en bas à droite des pochettes d'album : un
// bandeau sombre translucide portant à gauche le logo du format (CD,
// Hi-Res Audio pour les studio masters et le DSD, ou MP3) et à droite la
// résolution (« 16/44.1 », « 24/96 », « DSD64 », « 44.1 kHz » pour le MP3).
// Le logo rond du CD se loge dans l'extrémité gauche arrondie du bandeau ;
// les logos carrés Hi-Res et MP3 longent un bord gauche droit, sur toute la
// hauteur.
// Le bandeau a une taille FIXE : la pochette avec badge est toujours ramenée
// à un carré de badgeCanvas pixels (agrandie si besoin, bandes sombres si
// elle n'est pas carrée), si bien qu'une pochette petite ou non carrée ne
// reçoit jamais un badge plus petit, illisible.
// Pas de badge pour l'AAC ni les autres formats avec pertes : il n'existe pas
// de logo pour eux.
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
	_ "image/png" // badge logos / logos des badges
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Quality is the dominant format of an album.
// Quality : format dominant d'un album.
type Quality struct {
	Bits   int
	Rate   int
	DSD    bool
	Master bool
	MP3    bool // MP3 badge (lossy, no bit depth) / badge MP3 (avec pertes, pas de profondeur en bits)
}

// Valid reports whether the quality is known (a badge can be drawn).
// Valid indique si la qualité est connue (un badge peut être dessiné).
func (q Quality) Valid() bool { return q.Rate > 0 }

// Lossless reports a lossless format (FLAC, ALAC, WAV, AIFF, DSD).
// Lossless : vrai pour un format sans perte (FLAC, ALAC, WAV, AIFF, DSD).
func (q Quality) Lossless() bool { return q.Valid() && !q.MP3 }

// Label is the badge category name.
// Label est le nom de la catégorie du badge.
func (q Quality) Label() string {
	switch {
	case q.MP3:
		return "MP3"
	case q.Master:
		return "STUDIO MASTER"
	}
	return "CD"
}

// Format returns "24/96", "16/44.1", "DSD64" or "44.1 kHz" (MP3).
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

// code is part of the cache file name. "b9" is the drawing version: change it
// whenever the badge drawing changes so that stale badges are not served;
// unused files are purged at the next indexing.
// code : partie du nom de fichier en cache. « b9 » = version du dessin : à
// changer si le dessin du badge change, pour ne pas resservir un badge
// périmé ; les fichiers inutilisés sont purgés à l'indexation suivante.
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
	return "b9-" + k + "-" + strconv.Itoa(q.Bits) + "-" + strconv.Itoa(q.Rate)
}

// trackQuality returns a track's quality; invalid for a lossy format other
// than MP3 (AAC: no bit depth and no logo).
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

// albumQuality returns the most frequent format among the tracks (the better
// one on a tie). An album mixing MP3 and FLAC takes the majority format.
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

// better reports whether x ranks above y: valid, then lossless, then DSD,
// then higher sample rate, then more bits.
// better indique si x passe avant y : valide, puis sans perte, puis DSD,
// puis fréquence plus haute, puis plus de bits.
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
// Drawing / Dessin

// badgeFS holds the logos and font, embedded in the binary (nothing is
// downloaded on the NAS), drawn by package/build-tools/badge_logos.py.
// badgeFS contient les logos et la police, intégrés au programme (aucun
// téléchargement sur le NAS), dessinés par package/build-tools/badge_logos.py.
//
//go:embed badges/*.png badges/font.json
var badgeFS embed.FS

// Fixed dimensions, in pixels, on the square cover of badgeCanvas px. The
// control app shows the cover at tile size, so the band always takes the
// same share of the tile (about a third of the width). Its width is set by
// the widest resolution text ("24/352.8"), so every badge has the same size.
// Dimensions fixes, en pixels, sur la pochette carrée de badgeCanvas px.
// L'application de contrôle affiche la pochette à la taille de la tuile : le
// bandeau occupe donc toujours la même part de la tuile (environ un tiers de
// la largeur). Sa largeur est fixée par la résolution la plus large
// (« 24/352.8 ») : tous les badges ont la même taille.
const (
	badgeCanvas = artSize // 800
	badgeH      = 84      // band height / hauteur du bandeau
	badgeTextW  = 140     // text area, widest resolution included / zone du texte, résolution la plus large comprise
	badgeGap    = 12      // logo to text / logo-texte
	badgeEnd    = 24      // text to the rounded right end / texte-extrémité droite arrondie
	badgeW      = badgeH + badgeGap + badgeTextW + badgeEnd
	badgeMargin = 18 // gap to the cover edges / écart avec les bords de la pochette
	badgeDiscIn = 6  // CD disc inset in the rounded end / retrait du disque CD dans l'extrémité arrondie
)

// fontGlyph is one glyph of the resolution font: Inter Bold, pre-rendered and
// anti-aliased (badges/font.png + badges/font.json, generated by
// package/build-tools/badge_font.py). X and W locate it in the atlas, OX is
// the horizontal offset from the pen, Adv64 the advance in 1/64 px.
// fontGlyph : un glyphe de la police de la résolution : Inter Bold, rendue
// d'avance et lissée (badges/font.png + badges/font.json, produits par
// package/build-tools/badge_font.py). X et W le situent dans l'atlas, OX est
// le décalage horizontal par rapport au stylo, Adv64 l'avance en 1/64 px.
type fontGlyph struct {
	X     int `json:"x"`
	W     int `json:"w"`
	OX    int `json:"ox"`
	Adv64 int `json:"adv64"`
}

// badgeFont is the font metrics plus the glyph atlas used as a mask.
// badgeFont : métriques de la police et atlas des glyphes servant de masque.
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

// loadBadgeFont loads the embedded font once; nil if it cannot be read.
// loadBadgeFont charge une seule fois la police intégrée ; nil si illisible.
func loadBadgeFont() *badgeFont {
	fontOnce.Do(func() {
		var f badgeFont
		b, err := badgeFS.ReadFile("badges/font.json")
		if err == nil {
			err = json.Unmarshal(b, &f)
		}
		var im image.Image
		if err == nil {
			var r io.ReadCloser
			if r, err = badgeFS.Open("badges/font.png"); err == nil {
				im, _, err = image.Decode(r)
				r.Close()
			}
		}
		if err != nil {
			logf("badge: unreadable font: %v", err)
			return
		}
		// The atlas grey levels serve as a mask, which needs an Alpha image
		// (a Gray image is fully opaque when used as a mask).
		// Le gris de l'atlas sert de masque : il faut une image Alpha (une
		// image Gray est opaque partout en tant que masque).
		g := image.NewGray(im.Bounds())
		draw.Draw(g, g.Bounds(), im, im.Bounds().Min, draw.Src)
		f.atlas = &image.Alpha{Pix: g.Pix, Stride: g.Stride, Rect: g.Rect}
		fontData = &f
	})
	return fontData
}

// textWidth returns the text width in pixels (sum of advances).
// textWidth : largeur du texte en pixels (avances cumulées).
func (f *badgeFont) textWidth(s string) int {
	w := 0
	for _, r := range s {
		w += f.Glyphs[string(r)].Adv64
	}
	return (w + 32) / 64
}

// drawText draws s in colour c, baseline at baseline, starting at x.
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

// Band colours: dark translucent fill, light text, light grey behind
// logos with transparent parts (MP3).
// Couleurs du bandeau : fond sombre translucide, texte clair, gris clair
// derrière les logos à parties transparentes (MP3).
var (
	bandFill   = color.NRGBA{0x14, 0x14, 0x18, 0xBE}
	bandText   = color.RGBA{0xF0, 0xF0, 0xF0, 0xFF}
	logoBehind = color.RGBA{0xEB, 0xEB, 0xEB, 0xFF}
)

const badgeSS = 4 // supersampling: smooth band edges / suréchantillonnage : bords du bandeau lissés

// inBand reports whether (x, y) lies in the w×h band: right end rounded
// (radius h/2), left end rounded too when roundLeft, square otherwise.
// inBand indique si (x, y) est dans le bandeau w×h : extrémité droite
// arrondie (rayon h/2), extrémité gauche arrondie aussi si roundLeft,
// droite sinon.
func inBand(x, y, w, h int, roundLeft bool) bool {
	r := h / 2
	cx := -1
	switch {
	case x >= w-r:
		cx = w - r - 1
	case roundLeft && x < r:
		cx = r
	}
	if cx < 0 {
		return true
	}
	dx, dy := 2*(x-cx)+1, 2*(y-r)+1-h%2
	return dx*dx+dy*dy <= h*h
}

// roundLogo reports a round logo (the CD), placed inside a rounded left end.
// roundLogo : logo rond (le CD), posé dans une extrémité gauche arrondie.
func roundLogo(q Quality) bool { return !q.MP3 && !q.Master }

// logoFile returns the embedded logo for the quality.
// logoFile renvoie le logo intégré correspondant à la qualité.
func logoFile(q Quality) string {
	switch {
	case q.MP3:
		return "badges/mp3.png"
	case q.Master:
		return "badges/hires.png"
	}
	return "badges/cd.png"
}

// Logos fitted once and for all to the band. A nil entry records a failed load.
// Logos ajustés une fois pour toutes au bandeau. Une entrée nil note un échec.
var (
	logoMu    sync.Mutex
	logoCache = map[string]*image.RGBA{}
)

// badgeLogo returns the fitted logo, from the cache when possible: the CD
// disc keeps its transparency (badgeH-2×badgeDiscIn px square); the square
// logos are cropped to their visible part, brought to the band height and
// flattened on light grey.
// badgeLogo renvoie le logo ajusté, depuis le cache si possible : le disque
// CD garde sa transparence (carré de badgeH-2×badgeDiscIn px) ; les logos
// carrés sont recadrés sur leur partie visible, ramenés à la hauteur du
// bandeau et aplatis sur du gris clair.
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
		switch {
		case derr != nil:
			logf("badge: unreadable logo %s: %v", name, derr)
		case name == "badges/cd.png":
			n := badgeH - 2*badgeDiscIn
			out = shrinkRGBA(toRGBA(src, nil), n, n)
		default:
			flat := toRGBA(src, &logoBehind)
			vis := visibleBounds(src)
			sub := flat.SubImage(vis).(*image.RGBA)
			out = shrinkRGBA(toRGBA(sub, nil), max(1, vis.Dx()*badgeH/max(1, vis.Dy())), badgeH)
		}
	}
	logoCache[name] = out
	return out
}

// shrinkRGBA reduces by averaging the four premultiplied channels over each
// source area, so transparent edges stay clean (the CD disc). Logos are
// always larger than the band; an enlargement falls back to resizeRGBA.
// shrinkRGBA réduit en moyennant les quatre canaux prémultipliés sur chaque
// zone source : les bords transparents restent propres (le disque CD). Les
// logos sont toujours plus grands que le bandeau ; un agrandissement passe
// par resizeRGBA.
func shrinkRGBA(src *image.RGBA, nw, nh int) *image.RGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if nw > w || nh > h {
		return resizeRGBA(src, nw, nh)
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for dy := 0; dy < nh; dy++ {
		y0, y1 := dy*h/nh, max((dy+1)*h/nh, dy*h/nh+1)
		for dx := 0; dx < nw; dx++ {
			x0, x1 := dx*w/nw, max((dx+1)*w/nw, dx*w/nw+1)
			var sum [4]int
			for y := y0; y < y1; y++ {
				i := src.PixOffset(x0, y)
				for x := x0; x < x1; x, i = x+1, i+4 {
					for c := 0; c < 4; c++ {
						sum[c] += int(src.Pix[i+c])
					}
				}
			}
			n := (x1 - x0) * (y1 - y0)
			d := dst.PixOffset(dx, dy)
			for c := 0; c < 4; c++ {
				dst.Pix[d+c] = uint8((sum[c] + n/2) / n)
			}
		}
	}
	return dst
}

// toRGBA copies an image into an RGBA image starting at (0, 0), flattened on
// the given background colour, or keeping its transparency when bg is nil.
// toRGBA copie une image dans une image RGBA commençant en (0, 0), aplatie
// sur la couleur de fond donnée, ou en gardant sa transparence si bg est nil.
func toRGBA(src image.Image, bg *color.RGBA) *image.RGBA {
	b := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	if bg != nil {
		draw.Draw(out, out.Bounds(), image.NewUniform(*bg), image.Point{}, draw.Src)
	}
	draw.Draw(out, out.Bounds(), src, b.Min, draw.Over)
	return out
}

// visibleBounds returns the bounds of the non-transparent pixels, relative to
// (0, 0); the whole image when it is opaque or empty.
// visibleBounds renvoie le cadre des pixels non transparents, rapporté à
// (0, 0) ; l'image entière si elle est opaque ou vide.
func visibleBounds(src image.Image) image.Rectangle {
	b := src.Bounds()
	r := image.Rectangle{}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := src.At(x, y).RGBA(); a > 0x1000 {
				r = r.Union(image.Rect(x-b.Min.X, y-b.Min.Y, x-b.Min.X+1, y-b.Min.Y+1))
			}
		}
	}
	if r.Empty() {
		return image.Rect(0, 0, b.Dx(), b.Dy())
	}
	return r
}

// resizeRGBA shrinks by area averaging (downscale, art.go) or enlarges
// bilinearly.
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

// badgeLayer returns the band (transparent outside its shape), always
// badgeW×badgeH: logo on the left, resolution centred in the text area.
// badgeLayer renvoie le bandeau (transparent hors de sa forme), toujours
// badgeW×badgeH : logo à gauche, résolution centrée dans la zone de texte.
func badgeLayer(q Quality) *image.NRGBA {
	key := q.code()
	if l, ok := layerCache.Load(key); ok {
		return l.(*image.NRGBA)
	}
	l := drawLayer(q)
	layerCache.Store(key, l)
	return l
}

// layerCache keeps the drawn bands by quality: a few dozen at most (one per
// format and resolution), never redrawn.
// layerCache garde les bandeaux dessinés par qualité : quelques dizaines au
// plus (un par format et résolution), jamais redessinés.
var layerCache sync.Map

// drawLayer draws the band of one quality.
// drawLayer dessine le bandeau d'une qualité.
func drawLayer(q Quality) *image.NRGBA {
	round := roundLogo(q)
	// Shape coverage by k×k supersampling: anti-aliased edges.
	// Couverture de la forme par suréchantillonnage k×k : bords lissés.
	k := badgeSS
	mask := image.NewAlpha(image.Rect(0, 0, badgeW, badgeH))
	for y := 0; y < badgeH; y++ {
		for x := 0; x < badgeW; x++ {
			n := 0
			for j := 0; j < k; j++ {
				for i := 0; i < k; i++ {
					if inBand(x*k+i, y*k+j, badgeW*k, badgeH*k, round) {
						n++
					}
				}
			}
			mask.Pix[y*mask.Stride+x] = uint8(n * 255 / (k * k))
		}
	}
	out := image.NewNRGBA(mask.Rect)
	draw.DrawMask(out, out.Rect, image.NewUniform(bandFill), image.Point{}, mask, image.Point{}, draw.Src)

	textX := badgeH + badgeGap
	if logo := badgeLogo(logoFile(q)); logo != nil {
		lb := logo.Bounds()
		at := image.Pt(badgeDiscIn, badgeDiscIn)
		if !round {
			at = image.Point{}
			textX = lb.Dx() + badgeGap
		}
		draw.Draw(out, lb.Add(at), logo, image.Point{}, draw.Over)
	}
	if f := loadBadgeFont(); f != nil {
		txt := q.Format()
		area := badgeW - badgeEnd - textX
		baseline := (badgeH + f.CapHeight) / 2
		f.drawText(out, txt, textX+(area-f.textWidth(txt))/2, baseline, bandText)
	}
	return out
}

// squareCanvas brings the cover to a square of side n (enlarged or reduced,
// proportions kept, dark bands if it is not square).
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

// drawBadge draws the band in the bottom-right corner of a badgeCanvas px
// cover. Its dark translucent fill keeps it readable on pale and dark covers
// alike, with no outline.
// drawBadge pose le bandeau en bas à droite d'une pochette de badgeCanvas
// px. Son fond sombre translucide le garde lisible sur les pochettes claires
// comme sombres, sans contour.
func drawBadge(img *image.RGBA, q Quality) {
	if !q.Valid() {
		return
	}
	b := img.Bounds()
	if b.Dx() < badgeW+2*badgeMargin || b.Dy() < badgeH+2*badgeMargin {
		return // should not happen: the cover is brought to badgeCanvas / ne devrait pas arriver : la pochette est ramenée à badgeCanvas
	}
	x0, y0 := b.Max.X-badgeW-badgeMargin, b.Max.Y-badgeH-badgeMargin
	draw.Draw(img, image.Rect(x0, y0, x0+badgeW, y0+badgeH), badgeLayer(q), image.Point{}, draw.Over)
}

// badgedJPEG brings the reduced cover (JPEG) to the badgeCanvas square, then
// draws the badge.
// badgedJPEG : pochette réduite (JPEG) ramenée au carré badgeCanvas, puis
// badge.
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

// badgedCacheName is the cache file name of the badged cover.
// badgedCacheName : nom en cache de la pochette avec badge.
func badgedCacheName(a ArtRef, q Quality) string {
	return strings.TrimSuffix(artCacheName(a), ".jpg") + "-" + q.code() + ".jpg"
}

// GetBadged returns the reduced cover with the quality badge (without a
// badge when the quality is unknown).
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
	// Unique temporary file: two simultaneous requests do not overwrite each
	// other's output.
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
