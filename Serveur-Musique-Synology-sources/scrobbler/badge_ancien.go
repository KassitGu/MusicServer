package main

/*
 * Ancien dessin du badge (0.0.4), conservé pour référence.
 * Remplacé en 0.0.5 à la demande de Bertrand (2026-10-07) : logos fournis
 * par lui (MP3, CD, Hi-Res Audio) et taille fixe. En 0.0.4 la taille
 * dépendait du côté de la pochette (petites pochettes ou pochettes non
 * carrées : badge plus petit, parfois illisible).
 *
 * // ---------------------------------------------------------------------------
 * // Dessin
 *
 * // Police bitmap 5x7 (une ligne = 5 bits), réduite aux caractères utiles.
 * var glyphs = map[rune][7]uint8{
 * 	'A': {0x0E, 0x11, 0x11, 0x1F, 0x11, 0x11, 0x11},
 * 	'C': {0x0E, 0x11, 0x10, 0x10, 0x10, 0x11, 0x0E},
 * 	'D': {0x1E, 0x11, 0x11, 0x11, 0x11, 0x11, 0x1E},
 * 	'E': {0x1F, 0x10, 0x10, 0x1E, 0x10, 0x10, 0x1F},
 * 	'I': {0x0E, 0x04, 0x04, 0x04, 0x04, 0x04, 0x0E},
 * 	'M': {0x11, 0x1B, 0x15, 0x15, 0x11, 0x11, 0x11},
 * 	'O': {0x0E, 0x11, 0x11, 0x11, 0x11, 0x11, 0x0E},
 * 	'R': {0x1E, 0x11, 0x11, 0x1E, 0x14, 0x12, 0x11},
 * 	'S': {0x0F, 0x10, 0x10, 0x0E, 0x01, 0x01, 0x1E},
 * 	'T': {0x1F, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04},
 * 	'U': {0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x0E},
 * 	'0': {0x0E, 0x11, 0x13, 0x15, 0x19, 0x11, 0x0E},
 * 	'1': {0x04, 0x0C, 0x04, 0x04, 0x04, 0x04, 0x0E},
 * 	'2': {0x0E, 0x11, 0x01, 0x02, 0x04, 0x08, 0x1F},
 * 	'3': {0x1F, 0x02, 0x04, 0x02, 0x01, 0x11, 0x0E},
 * 	'4': {0x02, 0x06, 0x0A, 0x12, 0x1F, 0x02, 0x02},
 * 	'5': {0x1F, 0x10, 0x1E, 0x01, 0x01, 0x11, 0x0E},
 * 	'6': {0x06, 0x08, 0x10, 0x1E, 0x11, 0x11, 0x0E},
 * 	'7': {0x1F, 0x01, 0x02, 0x04, 0x08, 0x08, 0x08},
 * 	'8': {0x0E, 0x11, 0x11, 0x0E, 0x11, 0x11, 0x0E},
 * 	'9': {0x0E, 0x11, 0x11, 0x0F, 0x01, 0x02, 0x0C},
 * 	'/': {0x01, 0x01, 0x02, 0x04, 0x08, 0x10, 0x10},
 * 	'.': {0x00, 0x00, 0x00, 0x00, 0x00, 0x0C, 0x0C},
 * 	' ': {},
 * }
 *
 * type badgeStyle struct {
 * 	top, bottom, border, text color.RGBA
 * }
 *
 * var (
 * 	// Argent pour CD, or pour Studio Master (code couleur validé par
 * 	// Bertrand le 2026-10-06).
 * 	silverStyle = badgeStyle{top: color.RGBA{0xF4, 0xF5, 0xF7, 0xFF}, bottom: color.RGBA{0xA4, 0xA9, 0xB0, 0xFF},
 * 		border: color.RGBA{0x6E, 0x74, 0x7C, 0xFF}, text: color.RGBA{0x17, 0x1A, 0x1F, 0xFF}}
 * 	goldStyle = badgeStyle{top: color.RGBA{0xF8, 0xE0, 0x8E, 0xFF}, bottom: color.RGBA{0xB9, 0x86, 0x2C, 0xFF},
 * 		border: color.RGBA{0x7E, 0x58, 0x16, 0xFF}, text: color.RGBA{0x24, 0x18, 0x02, 0xFF}}
 * )
 *
 * const badgeSS = 4 // suréchantillonnage : bords et lettres lissés
 *
 * func textWidth(s string, scale int) int { return len([]rune(s))*6*scale - scale }
 *
 * func drawText(dst *image.RGBA, s string, x, y, scale int, c color.RGBA) {
 * 	for _, r := range s {
 * 		g := glyphs[r]
 * 		for row := 0; row < 7; row++ {
 * 			for col := 0; col < 5; col++ {
 * 				if g[row]&(1<<(4-col)) != 0 {
 * 					draw.Draw(dst, image.Rect(x+col*scale, y+row*scale, x+(col+1)*scale, y+(row+1)*scale),
 * 						image.NewUniform(c), image.Point{}, draw.Src)
 * 				}
 * 			}
 * 		}
 * 		x += 6 * scale
 * 	}
 * }
 *
 * // roundedMask : vrai si (x, y) est dans le rectangle w×h aux coins de rayon r.
 * func roundedMask(x, y, w, h, r int) bool {
 * 	dx, dy := 0, 0
 * 	if x < r {
 * 		dx = r - x
 * 	} else if x >= w-r {
 * 		dx = x - (w - r - 1)
 * 	}
 * 	if y < r {
 * 		dy = r - y
 * 	} else if y >= h-r {
 * 		dy = y - (h - r - 1)
 * 	}
 * 	return dx*dx+dy*dy <= r*r
 * }
 *
 * // badgeLayer dessine la pastille (fond transparent) à la taille d'une
 * // pochette de côté s pixels. Tailles relatives : à 800 px, lettres de
 * // 28 px (« CD », « STUDIO MASTER ») et 21 px (format).
 * func badgeLayer(q Quality, s int) *image.NRGBA {
 * 	st := silverStyle
 * 	if q.Master {
 * 		st = goldStyle
 * 	}
 * 	k := badgeSS
 * 	px := func(v int) int { // v pixels à 800 px, en pixels suréchantillonnés
 * 		n := v * s * k / 800
 * 		if n < 1 {
 * 			n = 1
 * 		}
 * 		return n
 * 	}
 * 	s1 := max(1, px(28)/7)
 * 	s2 := max(1, px(21)/7)
 * 	l1, l2 := q.Label(), q.Format()
 * 	tw := max(textWidth(l1, s1), textWidth(l2, s2))
 * 	padX, padY, gap := px(14), px(10), px(7)
 * 	border := max(k, px(2))
 * 	w, h := tw+2*padX, 7*s1+7*s2+gap+2*padY
 * 	r := px(16)
 *
 * 	hi := image.NewRGBA(image.Rect(0, 0, w, h))
 * 	for y := 0; y < h; y++ {
 * 		f := float64(y) / float64(h-1)
 * 		mix := func(a, b uint8) uint8 { return uint8(float64(a)*(1-f) + float64(b)*f + 0.5) }
 * 		fill := color.RGBA{mix(st.top.R, st.bottom.R), mix(st.top.G, st.bottom.G), mix(st.top.B, st.bottom.B), 255}
 * 		for x := 0; x < w; x++ {
 * 			if !roundedMask(x, y, w, h, r) {
 * 				continue
 * 			}
 * 			c := fill
 * 			if !roundedMask(x-border, y-border, w-2*border, h-2*border, max(0, r-border)) ||
 * 				x < border || y < border || x >= w-border || y >= h-border {
 * 				c = st.border
 * 			}
 * 			hi.SetRGBA(x, y, c)
 * 		}
 * 	}
 * 	drawText(hi, l1, (w-textWidth(l1, s1))/2, padY, s1, st.text)
 * 	drawText(hi, l2, (w-textWidth(l2, s2))/2, padY+7*s1+gap, s2, st.text)
 *
 * 	// Réduction k×k avec alpha : bords anti-crénelés.
 * 	ow, oh := w/k, h/k
 * 	out := image.NewNRGBA(image.Rect(0, 0, ow, oh))
 * 	for y := 0; y < oh; y++ {
 * 		for x := 0; x < ow; x++ {
 * 			var sr, sg, sb, sa int
 * 			for j := 0; j < k; j++ {
 * 				for i := 0; i < k; i++ {
 * 					c := hi.RGBAAt(x*k+i, y*k+j) // prémultiplié
 * 					sr += int(c.R)
 * 					sg += int(c.G)
 * 					sb += int(c.B)
 * 					sa += int(c.A)
 * 				}
 * 			}
 * 			if sa == 0 {
 * 				continue
 * 			}
 * 			out.SetNRGBA(x, y, color.NRGBA{uint8(sr * 255 / sa), uint8(sg * 255 / sa), uint8(sb * 255 / sa), uint8(sa / (k * k))})
 * 		}
 * 	}
 * 	return out
 * }
 *
 * // drawBadge pose le badge en bas à droite de l'image, avec une ombre
 * // légère pour rester visible sur une pochette claire.
 * func drawBadge(img *image.RGBA, q Quality) {
 * 	if !q.Valid() {
 * 		return
 * 	}
 * 	b := img.Bounds()
 * 	s := min(b.Dx(), b.Dy())
 * 	if s < 64 {
 * 		return // vignette minuscule : badge illisible
 * 	}
 * 	layer := badgeLayer(q, s)
 * 	lw, lh := layer.Bounds().Dx(), layer.Bounds().Dy()
 * 	m := max(2, s*18/800)
 * 	x0, y0 := b.Max.X-lw-m, b.Max.Y-lh-m
 * 	off := max(1, s*3/800)
 * 	shadow := image.NewUniform(color.NRGBA{0, 0, 0, 110})
 * 	draw.DrawMask(img, image.Rect(x0+off, y0+off, x0+off+lw, y0+off+lh), shadow, image.Point{}, layer, image.Point{}, draw.Over)
 * 	draw.Draw(img, image.Rect(x0, y0, x0+lw, y0+lh), layer, image.Point{}, draw.Over)
 * }
 *
 */
