package main

// Pochettes envoyées à l'application de contrôle : image du dossier ou image intégrée au
// fichier audio, réduite à artSize pixels (JPEG) et gardée en cache sur le
// NAS. Pourquoi : les pochettes « studio » font souvent 3000 px et plusieurs
// Mo ; envoyées telles quelles, elles ralentissent l'affichage des tuiles
// (défaut constaté avec l'ancien serveur).

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

const (
	artSize      = 800
	maxArtBytes  = 32 << 20
	maxArtPixels = 10000 * 10000
)

// loadArtBytes lit l'image désignée (fichier entier, ou segment d'un fichier
// audio pour une image intégrée).
func loadArtBytes(a ArtRef) ([]byte, error) {
	f, err := os.Open(a.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if a.Length > 0 {
		if a.Length > maxArtBytes {
			return nil, errors.New("image trop grande")
		}
		b := make([]byte, a.Length)
		if _, err := f.ReadAt(b, a.Offset); err != nil {
			return nil, err
		}
		return b, nil
	}
	return io.ReadAll(io.LimitReader(f, maxArtBytes))
}

// scaledJPEG décode une image JPEG ou PNG et la renvoie en JPEG d'au plus
// max pixels de côté. Une image JPEG déjà assez petite est renvoyée telle
// quelle.
func scaledJPEG(src []byte, max int) ([]byte, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxArtPixels {
		return nil, errors.New("dimensions d'image refusées")
	}
	if format == "jpeg" && cfg.Width <= max && cfg.Height <= max {
		return src, nil
	}
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	img = downscale(img, max)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// downscale réduit par moyenne de zones (filtre « boîte ») : simple, sans
// dépendance, et de bonne qualité pour une réduction.
func downscale(src image.Image, max int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= max && h <= max {
		return src
	}
	nw, nh := max, max
	if w > h {
		nh = int(float64(h) * float64(max) / float64(w))
	} else {
		nw = int(float64(w) * float64(max) / float64(h))
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	// Lecture rapide des formats courants, générique sinon.
	at := func(x, y int) (r, g, bl uint32) {
		c := src.At(b.Min.X+x, b.Min.Y+y)
		r, g, bl, _ = c.RGBA()
		return r >> 8, g >> 8, bl >> 8
	}
	if yc, ok := src.(*image.YCbCr); ok {
		at = func(x, y int) (uint32, uint32, uint32) {
			c := yc.YCbCrAt(b.Min.X+x, b.Min.Y+y)
			r, g, bl := color.YCbCrToRGB(c.Y, c.Cb, c.Cr)
			return uint32(r), uint32(g), uint32(bl)
		}
	} else if rg, ok := src.(*image.NRGBA); ok {
		at = func(x, y int) (uint32, uint32, uint32) {
			i := rg.PixOffset(b.Min.X+x, b.Min.Y+y)
			p := rg.Pix[i : i+4]
			a := uint32(p[3])
			// Fond blanc sous les zones transparentes (PNG).
			return (uint32(p[0])*a + 255*(255-a)) / 255, (uint32(p[1])*a + 255*(255-a)) / 255, (uint32(p[2])*a + 255*(255-a)) / 255
		}
	}
	for dy := 0; dy < nh; dy++ {
		y0 := dy * h / nh
		y1 := (dy + 1) * h / nh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for dx := 0; dx < nw; dx++ {
			x0 := dx * w / nw
			x1 := (dx + 1) * w / nw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var sr, sg, sb, n uint32
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					r, g, bl := at(x, y)
					sr += r
					sg += g
					sb += bl
					n++
				}
			}
			i := dst.PixOffset(dx, dy)
			dst.Pix[i] = uint8(sr / n)
			dst.Pix[i+1] = uint8(sg / n)
			dst.Pix[i+2] = uint8(sb / n)
			dst.Pix[i+3] = 255
		}
	}
	return dst
}

// artCache : pochettes réduites, sur disque. Le nom du fichier dépend de la
// source (chemin, position, taille, date) : une pochette changée produit un
// nouveau fichier, l'ancien est purgé à l'indexation suivante.
type artCache struct {
	dir  string
	sem  chan struct{} // réductions simultanées (processeur ARM du NAS)
	mu   sync.Mutex
	busy map[string]*sync.WaitGroup
}

func newArtCache(dir string) *artCache {
	os.MkdirAll(dir, 0o700)
	return &artCache{dir: dir, sem: make(chan struct{}, 2), busy: map[string]*sync.WaitGroup{}}
}

func artCacheName(a ArtRef) string {
	var mt int64
	if st, err := os.Stat(a.Path); err == nil {
		mt = st.ModTime().UnixNano()
	}
	return hashKey(a.Path, strconv.FormatInt(a.Offset, 10), strconv.FormatInt(a.Length, 10), strconv.FormatInt(mt, 10)) + ".jpg"
}

// Get renvoie la pochette réduite, en la calculant au premier appel. Deux
// demandes simultanées de la même image ne la calculent qu'une fois.
func (c *artCache) Get(a ArtRef) ([]byte, error) {
	name := artCacheName(a)
	p := filepath.Join(c.dir, name)
	if b, err := os.ReadFile(p); err == nil {
		return b, nil
	}
	c.mu.Lock()
	if wg := c.busy[name]; wg != nil {
		c.mu.Unlock()
		wg.Wait()
		return os.ReadFile(p)
	}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	c.busy[name] = wg
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.busy, name)
		c.mu.Unlock()
		wg.Done()
	}()

	c.sem <- struct{}{}
	defer func() { <-c.sem }()
	src, err := loadArtBytes(a)
	if err != nil {
		return nil, err
	}
	out, err := scaledJPEG(src, artSize)
	if err != nil {
		return nil, err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err == nil {
		os.Rename(tmp, p)
	}
	return out, nil
}

// Purge supprime les fichiers du cache qui ne correspondent plus à aucune
// pochette de la bibliothèque.
func (c *artCache) Purge(keep map[string]bool) {
	ents, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if !keep[e.Name()] {
			os.Remove(filepath.Join(c.dir, e.Name()))
		}
	}
}

// Les décodeurs sont enregistrés par leurs paquets ; png est importé
// explicitement pour image.Decode.
var _ = png.Decode
