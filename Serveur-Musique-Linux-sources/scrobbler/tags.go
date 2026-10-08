package main

// Reading of tags and stream properties of audio files, without any external
// library: FLAC (STREAMINFO, VORBIS_COMMENT, PICTURE), ID3v2 (MP3, DSF, AIFF,
// WAV), MP4/M4A (ALAC, AAC), WAV/AIFF/DSF headers.
//
// Files are only read, never modified. Only headers are read: embedded
// pictures are located (offset, length) but not loaded, and audio data is
// never scanned.
//
// Lecture des étiquettes et caractéristiques des fichiers audio, sans
// bibliothèque externe : FLAC (STREAMINFO, VORBIS_COMMENT, PICTURE), ID3v2
// (MP3, DSF, AIFF, WAV), MP4/M4A (ALAC, AAC), en-têtes WAV/AIFF/DSF.
//
// Les fichiers sont seulement lus (jamais modifiés). Seuls les en-têtes sont
// lus : les images intégrées sont repérées (position, taille) mais pas
// chargées, et les données audio ne sont jamais parcourues.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// tagInfo holds the tags and stream properties read from one audio file.
// tagInfo contient les étiquettes et caractéristiques lues dans un fichier audio.
type tagInfo struct {
	Title, Artist, AlbumArtist, Album, Composer string
	Genres                                      []string
	Disc, Number, Year                          int
	DurationMs                                  int64
	SampleRate, Bits, Channels                  int
	Art                                         ArtRef // embedded picture, front cover preferred / image intégrée, pochette de face de préférence
	artType                                     int    // picture type kept (3 = front) / type de l'image retenue (3 = face)
}

// Read limits: a damaged or malicious file must not make us load hundreds of
// MB into memory.
// Bornes de lecture : un fichier abîmé ou malveillant ne doit pas faire
// lire des centaines de Mo en mémoire.
const (
	maxCommentBlock = 4 << 20
	maxTextFrame    = 256 << 10
	maxID3Tag       = 64 << 20
)

var errNotSupported = errors.New("unrecognised format")

// readTags reads the tags according to the extension (lower case, with the dot).
// readTags lit les étiquettes selon l'extension (en minuscules, avec le point).
func readTags(r io.ReaderAt, size int64, path, ext string) (tagInfo, error) {
	var ti tagInfo
	var err error
	switch ext {
	case ".flac":
		err = parseFLAC(r, size, path, &ti)
	case ".mp3":
		err = parseMP3(r, size, path, &ti)
	case ".m4a", ".mp4", ".alac", ".aac":
		err = parseMP4(r, size, path, &ti)
	case ".wav":
		err = parseRIFF(r, size, path, &ti)
	case ".aif", ".aiff", ".aifc":
		err = parseAIFF(r, size, path, &ti)
	case ".dsf":
		err = parseDSF(r, size, path, &ti)
	default:
		err = errNotSupported
	}
	return ti, err
}

// ---------------------------------------------------------------------------
// Helpers / Utilitaires

// readAt reads exactly n bytes at off, or returns io.ErrUnexpectedEOF.
// readAt lit exactement n octets à off, sinon renvoie io.ErrUnexpectedEOF.
func readAt(r io.ReaderAt, off int64, n int) ([]byte, error) {
	if n < 0 || off < 0 {
		return nil, io.ErrUnexpectedEOF
	}
	b := make([]byte, n)
	m, err := r.ReadAt(b, off)
	if m == n {
		return b, nil
	}
	if err == nil || err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return b[:m], err
}

// leadingInt reads "3/12" -> 3.
// leadingInt lit « 3/12 » -> 3.
func leadingInt(s string) int {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	n, _ := strconv.Atoi(s[:i])
	return n
}

// yearOf reads the year in "1999", "1999-05-01", "05/01/1999".
// yearOf lit l'année dans « 1999 », « 1999-05-01 », « 05/01/1999 ».
func yearOf(s string) int {
	run := 0
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			run++
			if run == 4 && (i+1 == len(s) || s[i+1] < '0' || s[i+1] > '9') {
				y, _ := strconv.Atoi(s[i-3 : i+1])
				if y >= 1000 && y <= 2999 {
					return y
				}
			}
		} else {
			run = 0
		}
	}
	return 0
}

// latin1 decodes ISO-8859-1 text, unless it is already valid UTF-8.
// latin1 décode un texte ISO-8859-1, sauf s'il est déjà en UTF-8 valide.
func latin1(b []byte) string {
	if utf8.Valid(b) {
		// Many programs write UTF-8 while declaring it as Latin-1.
		// Beaucoup de logiciels écrivent de l'UTF-8 en le déclarant Latin-1.
		return string(b)
	}
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

func cleanText(s string) string {
	return strings.TrimSpace(strings.Trim(s, "\x00"))
}

// setField applies a tag (name normalised to upper case) to ti.
// setField applique une étiquette (nom normalisé en majuscules) à ti.
func (ti *tagInfo) setField(key, val string) {
	val = cleanText(val)
	if val == "" {
		return
	}
	switch key {
	case "TITLE":
		if ti.Title == "" {
			ti.Title = val
		}
	case "ARTIST":
		if ti.Artist == "" {
			ti.Artist = val
		} else if !strings.Contains(ti.Artist, val) {
			ti.Artist += ", " + val
		}
	case "ALBUMARTIST", "ALBUM ARTIST", "ALBUM_ARTIST":
		if ti.AlbumArtist == "" {
			ti.AlbumArtist = val
		}
	case "ALBUM":
		if ti.Album == "" {
			ti.Album = val
		}
	case "GENRE":
		ti.Genres = append(ti.Genres, val)
	case "COMPOSER":
		if ti.Composer == "" {
			ti.Composer = val
		}
	case "DISCNUMBER":
		if ti.Disc == 0 {
			ti.Disc = leadingInt(val)
		}
	case "TRACKNUMBER":
		if ti.Number == 0 {
			ti.Number = leadingInt(val)
		}
	case "DATE", "YEAR":
		if y := yearOf(val); y > 0 {
			ti.Year = y
		}
	case "ORIGINALDATE", "ORIGINALYEAR":
		if ti.Year == 0 {
			ti.Year = yearOf(val)
		}
	}
}

// setArt keeps an embedded picture: the front cover (type 3) wins over the
// other pictures. Only JPEG and PNG up to 32 MB are accepted.
// setArt retient une image intégrée : la pochette de face (type 3) l'emporte
// sur les autres images. Seuls JPEG et PNG jusqu'à 32 Mo sont acceptés.
func (ti *tagInfo) setArt(path string, off, length int64, mime string, ptype int) {
	mime = strings.ToLower(strings.TrimSpace(mime))
	switch mime {
	case "image/jpeg", "image/jpg", "jpg", "jpeg":
		mime = "image/jpeg"
	case "image/png", "png":
		mime = "image/png"
	default:
		return
	}
	if length <= 0 || length > 32<<20 {
		return
	}
	if ti.Art.Valid() && (ti.artType == 3 || ptype != 3) {
		return
	}
	ti.Art = ArtRef{Path: path, Offset: off, Length: length, Mime: mime}
	ti.artType = ptype
}

// ---------------------------------------------------------------------------
// FLAC

// parseFLAC reads the FLAC metadata blocks (at most 1024 of them).
// parseFLAC lit les blocs de métadonnées FLAC (au plus 1024).
func parseFLAC(r io.ReaderAt, size int64, path string, ti *tagInfo) error {
	var pos int64
	hdr, err := readAt(r, 0, 10)
	if err != nil {
		return err
	}
	// Some programs put an ID3v2 tag in front of the FLAC stream.
	// Certains logiciels ajoutent un ID3v2 devant le flux FLAC.
	if string(hdr[:3]) == "ID3" {
		pos = 10 + int64(syncsafe(hdr[6:10]))
		if hdr[5]&0x10 != 0 {
			pos += 10
		}
		hdr, err = readAt(r, pos, 4)
		if err != nil {
			return err
		}
	}
	if string(hdr[:4]) != "fLaC" {
		return errNotSupported
	}
	pos += 4
	for i := 0; i < 1024; i++ {
		h, err := readAt(r, pos, 4)
		if err != nil {
			return err
		}
		last := h[0]&0x80 != 0
		typ := h[0] & 0x7f
		n := int64(h[1])<<16 | int64(h[2])<<8 | int64(h[3])
		pos += 4
		if pos+n > size {
			return io.ErrUnexpectedEOF
		}
		switch typ {
		case 0: // STREAMINFO
			if n >= 18 {
				b, err := readAt(r, pos, 18)
				if err != nil {
					return err
				}
				sr := int(b[10])<<12 | int(b[11])<<4 | int(b[12])>>4
				ti.SampleRate = sr
				ti.Channels = int(b[12]>>1&7) + 1
				ti.Bits = int((b[12]&1)<<4|b[13]>>4) + 1
				total := uint64(b[13]&0x0f)<<32 | uint64(b[14])<<24 | uint64(b[15])<<16 | uint64(b[16])<<8 | uint64(b[17])
				if sr > 0 {
					ti.DurationMs = int64(total * 1000 / uint64(sr))
				}
			}
		case 4: // VORBIS_COMMENT
			if n <= maxCommentBlock {
				b, err := readAt(r, pos, int(n))
				if err != nil {
					return err
				}
				parseVorbisComment(b, ti)
			}
		case 6: // PICTURE
			parseFLACPicture(r, pos, n, path, ti)
		}
		pos += n
		if last {
			break
		}
	}
	return nil
}

// parseVorbisComment reads a VORBIS_COMMENT block (little-endian lengths).
// parseVorbisComment lit un bloc VORBIS_COMMENT (longueurs en petit-boutiste).
func parseVorbisComment(b []byte, ti *tagInfo) {
	if len(b) < 8 {
		return
	}
	vl := int(binary.LittleEndian.Uint32(b))
	p := 4 + vl
	if vl < 0 || p+4 > len(b) {
		return
	}
	count := int(binary.LittleEndian.Uint32(b[p:]))
	p += 4
	for i := 0; i < count && p+4 <= len(b); i++ {
		l := int(binary.LittleEndian.Uint32(b[p:]))
		p += 4
		if l < 0 || p+l > len(b) {
			return
		}
		if k, v, ok := strings.Cut(string(b[p:p+l]), "="); ok {
			ti.setField(strings.ToUpper(k), v)
		}
		p += l
	}
}

// parseFLACPicture locates the image data of a PICTURE block.
// parseFLACPicture repère les données d'image d'un bloc PICTURE.
func parseFLACPicture(r io.ReaderAt, pos, n int64, path string, ti *tagInfo) {
	end := pos + n
	b, err := readAt(r, pos, 8)
	if err != nil {
		return
	}
	ptype := int(binary.BigEndian.Uint32(b))
	ml := int64(binary.BigEndian.Uint32(b[4:]))
	p := pos + 8
	if ml > 256 || p+ml > end {
		return
	}
	mime, err := readAt(r, p, int(ml))
	if err != nil {
		return
	}
	p += ml
	b, err = readAt(r, p, 4)
	if err != nil {
		return
	}
	dl := int64(binary.BigEndian.Uint32(b))
	p += 4 + dl + 16 // description, width, height, depth, colours / description, largeur, hauteur, profondeur, couleurs
	b, err = readAt(r, p, 4)
	if err != nil || p+4 > end {
		return
	}
	l := int64(binary.BigEndian.Uint32(b))
	p += 4
	if p+l > end {
		return
	}
	ti.setArt(path, p, l, string(mime), ptype)
}

// ---------------------------------------------------------------------------
// ID3v2 (2.2, 2.3, 2.4)

// syncsafe decodes a 28-bit ID3v2 "syncsafe" integer (7 bits per byte).
// syncsafe décode un entier ID3v2 « syncsafe » de 28 bits (7 bits par octet).
func syncsafe(b []byte) uint32 {
	return uint32(b[0]&0x7f)<<21 | uint32(b[1]&0x7f)<<14 | uint32(b[2]&0x7f)<<7 | uint32(b[3]&0x7f)
}

// id3v22 maps ID3v2.2 three-letter frame IDs to their ID3v2.3/2.4 names.
// id3v22 associe les identifiants de cadre ID3v2.2 (3 lettres) à leur nom ID3v2.3/2.4.
var id3v22 = map[string]string{"TT2": "TIT2", "TP1": "TPE1", "TP2": "TPE2", "TAL": "TALB", "TCO": "TCON",
	"TRK": "TRCK", "TPA": "TPOS", "TYE": "TYER", "TCM": "TCOM", "PIC": "APIC", "TDA": "TDAT"}

// id3Fields maps ID3v2 text frames to the normalised tag names of setField.
// id3Fields associe les cadres texte ID3v2 aux noms normalisés de setField.
var id3Fields = map[string]string{"TIT2": "TITLE", "TPE1": "ARTIST", "TPE2": "ALBUMARTIST", "TALB": "ALBUM",
	"TCON": "GENRE", "TRCK": "TRACKNUMBER", "TPOS": "DISCNUMBER", "TYER": "DATE", "TDRC": "DATE",
	"TCOM": "COMPOSER", "TDOR": "ORIGINALDATE", "TORY": "ORIGINALYEAR"}

// parseID3 reads an ID3v2 tag located at base; it returns its total size
// (0 if absent).
// parseID3 lit un tag ID3v2 situé à base ; renvoie sa taille totale (0 si
// absent).
func parseID3(r io.ReaderAt, base int64, path string, ti *tagInfo) (int64, error) {
	h, err := readAt(r, base, 10)
	if err != nil || string(h[:3]) != "ID3" {
		return 0, errNotSupported
	}
	ver := h[3]
	flags := h[5]
	tagSize := int64(syncsafe(h[6:10]))
	total := 10 + tagSize
	if flags&0x10 != 0 {
		total += 10 // tag footer / pied de tag (2.4)
	}
	if ver < 2 || ver > 4 || tagSize > maxID3Tag {
		return total, errNotSupported
	}
	unsync := flags&0x80 != 0
	p := base + 10
	end := base + 10 + tagSize
	if flags&0x40 != 0 && ver >= 3 { // extended header / en-tête étendu
		e, err := readAt(r, p, 4)
		if err != nil {
			return total, err
		}
		if ver == 4 {
			p += int64(syncsafe(e))
		} else {
			p += 4 + int64(binary.BigEndian.Uint32(e))
		}
	}
	hl := int64(10)
	if ver == 2 {
		hl = 6
	}
	for p+hl <= end {
		fh, err := readAt(r, p, int(hl))
		if err != nil {
			break
		}
		if fh[0] == 0 {
			break // padding / remplissage
		}
		var id string
		var fsize int64
		var fflags uint16
		if ver == 2 {
			id = id3v22[string(fh[:3])]
			fsize = int64(fh[3])<<16 | int64(fh[4])<<8 | int64(fh[5])
		} else {
			id = string(fh[:4])
			if ver == 4 {
				fsize = int64(syncsafe(fh[4:8]))
			} else {
				fsize = int64(binary.BigEndian.Uint32(fh[4:8]))
			}
			fflags = binary.BigEndian.Uint16(fh[8:10])
		}
		data := p + hl
		p = data + fsize
		if fsize <= 0 || p > end {
			break
		}
		if ver == 4 {
			if fflags&0x000C != 0 { // compressed or encrypted: skipped / compressé ou chiffré : ignoré
				continue
			}
			if fflags&0x0001 != 0 { // data length indicator / indicateur de longueur
				data += 4
				fsize -= 4
			}
			if fflags&0x0002 != 0 {
				unsync = true
			}
		} else if ver == 3 && fflags&0x00C0 != 0 { // compressed or encrypted / compressé ou chiffré
			continue
		}
		// Unsynchronised pictures are skipped: their bytes cannot be served
		// as they are on disk.
		// Les images désynchronisées sont ignorées : leurs octets ne peuvent
		// pas être servis tels qu'ils sont sur le disque.
		if id == "APIC" {
			if !unsync {
				parseAPIC(r, data, fsize, ver == 2, path, ti)
			}
			continue
		}
		key := id3Fields[id]
		if key == "" || fsize > maxTextFrame {
			if id == "TXXX" && fsize <= maxTextFrame {
				if b, err := readAt(r, data, int(fsize)); err == nil {
					vals := id3Text(b)
					if len(vals) >= 2 {
						ti.setField(strings.ToUpper(vals[0]), vals[1])
					}
				}
			}
			continue
		}
		b, err := readAt(r, data, int(fsize))
		if err != nil {
			break
		}
		for _, v := range id3Text(b) {
			if key == "GENRE" {
				v = id3Genre(v)
			}
			ti.setField(key, v)
		}
	}
	return total, nil
}

// id3Text decodes a text frame (encoding byte, then values separated by NUL
// in 2.4).
// id3Text décode un cadre texte (octet d'encodage puis valeurs séparées par
// des NUL en 2.4).
func id3Text(b []byte) []string {
	if len(b) < 1 {
		return nil
	}
	enc, b := b[0], b[1:]
	var s string
	switch enc {
	case 0:
		s = latin1(b)
	case 1, 2:
		s = utf16Text(b, enc == 2)
	default:
		s = string(b)
	}
	var out []string
	for _, v := range strings.Split(s, "\x00") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// utf16Text decodes UTF-16; a byte order mark overrides bigEndian.
// utf16Text décode de l'UTF-16 ; une marque d'ordre des octets remplace bigEndian.
func utf16Text(b []byte, bigEndian bool) string {
	var u []uint16
	be := bigEndian
	for i := 0; i+1 < len(b); i += 2 {
		c0, c1 := b[i], b[i+1]
		if c0 == 0xFF && c1 == 0xFE {
			be = false
			continue
		}
		if c0 == 0xFE && c1 == 0xFF {
			be = true
			continue
		}
		if be {
			u = append(u, uint16(c0)<<8|uint16(c1))
		} else {
			u = append(u, uint16(c1)<<8|uint16(c0))
		}
	}
	return string(utf16.Decode(u))
}

// id3Genre turns "(17)Rock" into "Rock"; a bare "(17)" is discarded later
// (splitGenres in library.go).
// id3Genre : « (17)Rock » -> « Rock » ; « (17) » seul est écarté plus loin
// (splitGenres dans library.go).
func id3Genre(v string) string {
	for strings.HasPrefix(v, "(") {
		i := strings.IndexByte(v, ')')
		if i < 0 || i+1 >= len(v) {
			break
		}
		v = v[i+1:]
	}
	return v
}

// parseAPIC locates the image data of an APIC (or 2.2 PIC) frame.
// parseAPIC repère les données d'image d'un cadre APIC (ou PIC en 2.2).
func parseAPIC(r io.ReaderAt, data, size int64, v22 bool, path string, ti *tagInfo) {
	n := size
	if n > 1024 {
		n = 1024
	}
	b, err := readAt(r, data, int(n))
	if err != nil || len(b) < 4 {
		return
	}
	enc := b[0]
	p := 1
	var mime string
	if v22 {
		mime = strings.ToLower(string(b[1:4]))
		p = 4
	} else {
		i := bytes.IndexByte(b[p:], 0)
		if i < 0 {
			return
		}
		mime = string(b[p : p+i])
		p += i + 1
	}
	if p >= len(b) {
		return
	}
	ptype := int(b[p])
	p++
	// Description ended by NUL (aligned double NUL in UTF-16).
	// Description terminée par NUL (double NUL en UTF-16, aligné).
	if enc == 1 || enc == 2 {
		for ; p+1 < len(b); p += 2 {
			if b[p] == 0 && b[p+1] == 0 {
				p += 2
				break
			}
		}
	} else {
		i := bytes.IndexByte(b[p:], 0)
		if i < 0 {
			return
		}
		p += i + 1
	}
	if int64(p) >= size {
		return
	}
	ti.setArt(path, data+int64(p), size-int64(p), mime, ptype)
}

// ---------------------------------------------------------------------------
// MP3: ID3v2 + duration (Xing/Info header, otherwise constant bitrate)
// MP3 : ID3v2 + durée (en-tête Xing/Info, sinon débit constant)

var mp3Bitrates = [2][16]int{
	{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}, // MPEG-1 Layer III / couche III
	{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},     // MPEG-2/2.5 Layer III / couche III
}
var mp3Rates = [4][3]int{{11025, 12000, 8000}, {0, 0, 0}, {22050, 24000, 16000}, {44100, 48000, 32000}}

func parseMP3(r io.ReaderAt, size int64, path string, ti *tagInfo) error {
	start, _ := parseID3(r, 0, path, ti)
	// Look for the first frame in the first 64 KB after the tag.
	// Recherche de la première trame dans les 64 premiers Ko après le tag.
	b, _ := readAt(r, start, 64<<10)
	for i := 0; i+4 <= len(b); i++ {
		if b[i] != 0xFF || b[i+1]&0xE0 != 0xE0 {
			continue
		}
		verBits := b[i+1] >> 3 & 3
		layer := b[i+1] >> 1 & 3
		bri := b[i+2] >> 4
		sri := b[i+2] >> 2 & 3
		if verBits == 1 || layer != 1 || bri == 0 || bri == 15 || sri == 3 {
			continue
		}
		v1 := verBits == 3
		sr := mp3Rates[verBits][sri]
		tbl := 1
		if v1 {
			tbl = 0
		}
		br := mp3Bitrates[tbl][bri] * 1000
		mono := b[i+3]>>6 == 3
		ti.SampleRate, ti.Channels = sr, 2
		if mono {
			ti.Channels = 1
		}
		spf := 1152
		if !v1 {
			spf = 576
		}
		// Xing/Info header: number of frames (variable bitrate files).
		// Its offset depends on the MPEG version and channel mode.
		// En-tête Xing/Info : nombre de trames (fichiers à débit variable).
		// Sa position dépend de la version MPEG et du mode des canaux.
		off := 4 + 32
		switch {
		case v1 && mono:
			off = 4 + 17
		case !v1 && !mono:
			off = 4 + 17
		case !v1 && mono:
			off = 4 + 9
		}
		if i+off+12 <= len(b) {
			tag := string(b[i+off : i+off+4])
			if (tag == "Xing" || tag == "Info") && b[i+off+7]&1 != 0 {
				frames := int64(binary.BigEndian.Uint32(b[i+off+8:]))
				ti.DurationMs = frames * int64(spf) * 1000 / int64(sr)
				return nil
			}
		}
		if br > 0 {
			ti.DurationMs = (size - start - int64(i)) * 8 * 1000 / int64(br)
		}
		return nil
	}
	return nil
}

// ---------------------------------------------------------------------------
// MP4 / M4A (ALAC, AAC)

type mp4Box struct {
	typ        string
	start, end int64 // box content / contenu de la boîte
}

// mp4Children lists the boxes between start and end (at most 4096).
// mp4Children liste les boîtes entre start et end (au plus 4096).
func mp4Children(r io.ReaderAt, start, end int64) []mp4Box {
	var out []mp4Box
	p := start
	for p+8 <= end && len(out) < 4096 {
		h, err := readAt(r, p, 8)
		if err != nil {
			break
		}
		sz := int64(binary.BigEndian.Uint32(h))
		hl := int64(8)
		if sz == 1 {
			x, err := readAt(r, p+8, 8)
			if err != nil {
				break
			}
			sz = int64(binary.BigEndian.Uint64(x))
			hl = 16
		} else if sz == 0 {
			sz = end - p
		}
		if sz < hl || p+sz > end {
			break
		}
		out = append(out, mp4Box{typ: string(h[4:8]), start: p + hl, end: p + sz})
		p += sz
	}
	return out
}

// mp4Find follows a path of box types, e.g. "moov", "udta", "meta".
// mp4Find suit un chemin de types de boîtes, par ex. « moov », « udta », « meta ».
func mp4Find(r io.ReaderAt, start, end int64, path ...string) (mp4Box, bool) {
	var cur mp4Box
	for i, name := range path {
		found := false
		for _, b := range mp4Children(r, start, end) {
			if b.typ == name {
				cur, found = b, true
				break
			}
		}
		if !found {
			return cur, false
		}
		start, end = cur.start, cur.end
		if name == "meta" && i < len(path)-1 {
			start += 4 // "meta" is a full box (version + flags) / « meta » est une boîte complète (version + drapeaux)
		}
	}
	return cur, true
}

// mp4Fields maps iTunes metadata items to the normalised tag names.
// mp4Fields associe les éléments de métadonnées iTunes aux noms normalisés.
var mp4Fields = map[string]string{"\xa9nam": "TITLE", "\xa9ART": "ARTIST", "aART": "ALBUMARTIST", "\xa9alb": "ALBUM",
	"\xa9gen": "GENRE", "\xa9day": "DATE", "\xa9wrt": "COMPOSER"}

// parseMP4 reads the duration (mvhd), the audio sample entry and the ilst tags.
// parseMP4 lit la durée (mvhd), la description d'échantillons audio et les étiquettes ilst.
func parseMP4(r io.ReaderAt, size int64, path string, ti *tagInfo) error {
	moov, ok := mp4Find(r, 0, size, "moov")
	if !ok {
		return errNotSupported
	}
	if mv, ok := mp4Find(r, moov.start, moov.end, "mvhd"); ok {
		if b, err := readAt(r, mv.start, 32); err == nil {
			var scale, dur uint64
			if b[0] == 1 {
				scale = uint64(binary.BigEndian.Uint32(b[20:]))
				dur = binary.BigEndian.Uint64(b[24:])
			} else {
				scale = uint64(binary.BigEndian.Uint32(b[12:]))
				dur = uint64(binary.BigEndian.Uint32(b[16:]))
			}
			if scale > 0 {
				ti.DurationMs = int64(dur * 1000 / scale)
			}
		}
	}
	for _, trak := range mp4Children(r, moov.start, moov.end) {
		if trak.typ != "trak" {
			continue
		}
		stsd, ok := mp4Find(r, trak.start, trak.end, "mdia", "minf", "stbl", "stsd")
		if !ok {
			continue
		}
		entries := mp4Children(r, stsd.start+8, stsd.end)
		if len(entries) == 0 || (entries[0].typ != "mp4a" && entries[0].typ != "alac") {
			continue
		}
		e := entries[0]
		if b, err := readAt(r, e.start, 28); err == nil {
			ti.Channels = int(binary.BigEndian.Uint16(b[16:]))
			ti.Bits = int(binary.BigEndian.Uint16(b[18:]))
			ti.SampleRate = int(binary.BigEndian.Uint16(b[24:]))
		}
		// ALAC: the inner "alac" box gives the real sample rate (the generic
		// header cannot hold rates above 65535 Hz).
		// ALAC : la boîte « alac » interne donne la vraie fréquence (au-delà
		// de 65535 Hz, l'en-tête générique ne peut pas l'écrire).
		if e.typ == "alac" {
			for _, c := range mp4Children(r, e.start+28, e.end) {
				if c.typ == "alac" {
					if b, err := readAt(r, c.start+4, 24); err == nil {
						ti.Bits = int(b[5])
						ti.Channels = int(b[9])
						ti.SampleRate = int(binary.BigEndian.Uint32(b[20:]))
					}
				}
			}
		} else {
			ti.Bits = 0 // AAC: no PCM bit depth / AAC : pas de profondeur de bits au sens PCM
		}
		break
	}
	ilst, ok := mp4Find(r, moov.start, moov.end, "udta", "meta", "ilst")
	if !ok {
		return nil
	}
	for _, item := range mp4Children(r, ilst.start, ilst.end) {
		for _, d := range mp4Children(r, item.start, item.end) {
			if d.typ != "data" || d.end-d.start < 8 {
				continue
			}
			h, err := readAt(r, d.start, 8)
			if err != nil {
				continue
			}
			kind := binary.BigEndian.Uint32(h) & 0xFFFFFF
			vstart, vlen := d.start+8, d.end-d.start-8
			switch item.typ {
			case "covr":
				mime := ""
				switch kind {
				case 13:
					mime = "image/jpeg"
				case 14:
					mime = "image/png"
				}
				ti.setArt(path, vstart, vlen, mime, 3)
			case "trkn", "disk":
				if b, err := readAt(r, vstart, 4); err == nil {
					n := int(binary.BigEndian.Uint16(b[2:]))
					if item.typ == "trkn" {
						ti.Number = n
					} else {
						ti.Disc = n
					}
				}
			default:
				key := mp4Fields[item.typ]
				if key == "" || kind != 1 || vlen > maxTextFrame {
					continue
				}
				if b, err := readAt(r, vstart, int(vlen)); err == nil {
					ti.setField(key, string(b))
				}
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// WAV (RIFF): fmt, data, LIST/INFO, id3

// riffInfo maps RIFF INFO chunks to the normalised tag names.
// riffInfo associe les sous-blocs RIFF INFO aux noms normalisés.
var riffInfo = map[string]string{"INAM": "TITLE", "IART": "ARTIST", "IPRD": "ALBUM", "IGNR": "GENRE",
	"ICRD": "DATE", "ITRK": "TRACKNUMBER", "IPRT": "TRACKNUMBER"}

func parseRIFF(r io.ReaderAt, size int64, path string, ti *tagInfo) error {
	h, err := readAt(r, 0, 12)
	if err != nil || string(h[:4]) != "RIFF" || string(h[8:12]) != "WAVE" {
		return errNotSupported
	}
	var byteRate, dataSize int64
	p := int64(12)
	for i := 0; p+8 <= size && i < 256; i++ {
		ch, err := readAt(r, p, 8)
		if err != nil {
			break
		}
		id := string(ch[:4])
		n := int64(binary.LittleEndian.Uint32(ch[4:]))
		body := p + 8
		switch id {
		case "fmt ":
			if b, err := readAt(r, body, 16); err == nil {
				ti.Channels = int(binary.LittleEndian.Uint16(b[2:]))
				ti.SampleRate = int(binary.LittleEndian.Uint32(b[4:]))
				byteRate = int64(binary.LittleEndian.Uint32(b[8:]))
				ti.Bits = int(binary.LittleEndian.Uint16(b[14:]))
			}
		case "data":
			dataSize = n
			if body+n > size {
				dataSize = size - body
			}
		case "id3 ", "ID3 ":
			parseID3(r, body, path, ti)
		case "LIST":
			if n >= 4 && n <= maxCommentBlock {
				if b, err := readAt(r, body, int(n)); err == nil && string(b[:4]) == "INFO" {
					q := 4
					for q+8 <= len(b) {
						sid := string(b[q : q+4])
						sl := int(binary.LittleEndian.Uint32(b[q+4:]))
						q += 8
						if sl < 0 || q+sl > len(b) {
							break
						}
						if key := riffInfo[sid]; key != "" {
							ti.setField(key, latin1(bytes.TrimRight(b[q:q+sl], "\x00")))
						}
						q += sl + sl&1
					}
				}
			}
		}
		p = body + n + n&1
	}
	if byteRate > 0 && dataSize > 0 {
		ti.DurationMs = dataSize * 1000 / byteRate
	}
	return nil
}

// ---------------------------------------------------------------------------
// AIFF: COMM, ID3

func parseAIFF(r io.ReaderAt, size int64, path string, ti *tagInfo) error {
	h, err := readAt(r, 0, 12)
	if err != nil || string(h[:4]) != "FORM" || (string(h[8:12]) != "AIFF" && string(h[8:12]) != "AIFC") {
		return errNotSupported
	}
	var frames int64
	p := int64(12)
	for i := 0; p+8 <= size && i < 256; i++ {
		ch, err := readAt(r, p, 8)
		if err != nil {
			break
		}
		id := string(ch[:4])
		n := int64(binary.BigEndian.Uint32(ch[4:]))
		body := p + 8
		switch id {
		case "COMM":
			if b, err := readAt(r, body, 18); err == nil {
				ti.Channels = int(binary.BigEndian.Uint16(b))
				frames = int64(binary.BigEndian.Uint32(b[2:]))
				ti.Bits = int(binary.BigEndian.Uint16(b[6:]))
				ti.SampleRate = int(ext80(b[8:18]))
			}
		case "ID3 ", "id3 ":
			parseID3(r, body, path, ti)
		}
		p = body + n + n&1
	}
	if ti.SampleRate > 0 {
		ti.DurationMs = frames * 1000 / int64(ti.SampleRate)
	}
	return nil
}

// ext80 converts an 80-bit IEEE 754 extended float (AIFF sample rate);
// negative values give 0.
// ext80 convertit un flottant IEEE 754 80 bits (fréquence AIFF) ; les valeurs
// négatives donnent 0.
func ext80(b []byte) float64 {
	exp := int(binary.BigEndian.Uint16(b)&0x7fff) - 16383 - 63
	mant := binary.BigEndian.Uint64(b[2:])
	if b[0]&0x80 != 0 {
		return 0
	}
	return math.Ldexp(float64(mant), exp)
}

// ---------------------------------------------------------------------------
// DSF (DSD): "fmt " chunk and ID3 at the end of the file
// DSF (DSD) : bloc « fmt » et ID3 en fin de fichier

func parseDSF(r io.ReaderAt, size int64, path string, ti *tagInfo) error {
	b, err := readAt(r, 0, 80)
	if err != nil || string(b[:4]) != "DSD " || string(b[28:32]) != "fmt " {
		return errNotSupported
	}
	meta := int64(binary.LittleEndian.Uint64(b[20:]))
	f := b[28:]
	ti.Channels = int(binary.LittleEndian.Uint32(f[24:]))
	ti.SampleRate = int(binary.LittleEndian.Uint32(f[28:]))
	ti.Bits = int(binary.LittleEndian.Uint32(f[32:]))
	samples := int64(binary.LittleEndian.Uint64(f[36:]))
	if ti.SampleRate > 0 {
		ti.DurationMs = samples * 1000 / int64(ti.SampleRate)
	}
	if meta > 0 && meta < size {
		parseID3(r, meta, path, ti)
	}
	return nil
}
