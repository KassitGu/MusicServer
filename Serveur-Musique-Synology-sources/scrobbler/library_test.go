package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Fabrication de fichiers de test (sans outil externe)

func be32(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }
func le32(v uint32) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, v); return b }

func flacBlock(typ byte, last bool, body []byte) []byte {
	h := []byte{typ, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}
	if last {
		h[0] |= 0x80
	}
	return append(h, body...)
}

// makeFLAC écrit un FLAC minimal : STREAMINFO, étiquettes, image, puis des
// octets « audio » quelconques (le parseur ne lit pas l'audio).
func makeFLAC(t *testing.T, path string, tags [][2]string, pic []byte, picMime string, sr, bits, ch int, samples uint64, audio []byte) {
	t.Helper()
	si := make([]byte, 34)
	binary.BigEndian.PutUint16(si[0:], 4096)
	binary.BigEndian.PutUint16(si[2:], 4096)
	v := uint64(sr)<<44 | uint64(ch-1)<<41 | uint64(bits-1)<<36 | samples
	binary.BigEndian.PutUint64(si[10:], v)
	var vc bytes.Buffer
	vendor := "test"
	vc.Write(le32(uint32(len(vendor))))
	vc.WriteString(vendor)
	vc.Write(le32(uint32(len(tags))))
	for _, kv := range tags {
		e := kv[0] + "=" + kv[1]
		vc.Write(le32(uint32(len(e))))
		vc.WriteString(e)
	}
	var out bytes.Buffer
	out.WriteString("fLaC")
	out.Write(flacBlock(0, false, si))
	out.Write(flacBlock(1, false, make([]byte, 100))) // PADDING
	if pic != nil {
		var pb bytes.Buffer
		pb.Write(be32(3))
		pb.Write(be32(uint32(len(picMime))))
		pb.WriteString(picMime)
		pb.Write(be32(5))
		pb.WriteString("front")
		pb.Write(make([]byte, 16))
		pb.Write(be32(uint32(len(pic))))
		pb.Write(pic)
		out.Write(flacBlock(4, false, vc.Bytes()))
		out.Write(flacBlock(6, true, pb.Bytes()))
	} else {
		out.Write(flacBlock(4, true, vc.Bytes()))
	}
	out.Write(audio)
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testJPEG(w, h int, c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, nil)
	return b.Bytes()
}

func testPNG(w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func id3Frame(id string, body []byte) []byte {
	return append(append([]byte(id), append(be32(uint32(len(body))), 0, 0)...), body...)
}

func syncsafeBytes(n int) []byte {
	return []byte{byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
}

// ---------------------------------------------------------------------------
// Étiquettes

func TestParseFLAC(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.flac")
	cover := testJPEG(50, 50, color.RGBA{255, 0, 0, 255})
	makeFLAC(t, p, [][2]string{{"TITLE", "So What"}, {"artist", "Miles Davis"}, {"ALBUMARTIST", "Miles Davis"},
		{"ALBUM", "Kind of Blue"}, {"GENRE", "Jazz"}, {"GENRE", "Modal; Cool"}, {"TRACKNUMBER", "1/5"},
		{"DISCNUMBER", "1/1"}, {"DATE", "1959-08-17"}, {"COMPOSER", "Miles Davis"}},
		cover, "image/jpeg", 192000, 24, 2, 192000*545, make([]byte, 1000))
	f, _ := os.Open(p)
	defer f.Close()
	st, _ := f.Stat()
	ti, err := readTags(f, st.Size(), p, ".flac")
	if err != nil {
		t.Fatal(err)
	}
	if ti.Title != "So What" || ti.Artist != "Miles Davis" || ti.Album != "Kind of Blue" || ti.Year != 1959 ||
		ti.Number != 1 || ti.Disc != 1 || ti.SampleRate != 192000 || ti.Bits != 24 || ti.Channels != 2 ||
		ti.DurationMs != 545000 || len(ti.Genres) != 2 {
		t.Fatalf("FLAC : %+v", ti)
	}
	b, err := loadArtBytes(ti.Art)
	if err != nil || !bytes.Equal(b, cover) || ti.Art.Mime != "image/jpeg" {
		t.Fatalf("image intégrée : %v %+v", err, ti.Art)
	}
}

func TestParseMP3ID3(t *testing.T) {
	cover := testJPEG(20, 20, color.White)
	var frames bytes.Buffer
	frames.Write(id3Frame("TIT2", append([]byte{3}, "Titre é"...)))
	// UTF-16 avec BOM
	u16 := []byte{1, 0xFF, 0xFE}
	for _, r := range "Artiste" {
		u16 = append(u16, byte(r), 0)
	}
	frames.Write(id3Frame("TPE1", u16))
	frames.Write(id3Frame("TALB", append([]byte{0}, "Album"...)))
	frames.Write(id3Frame("TCON", append([]byte{0}, "(17)Rock"...)))
	frames.Write(id3Frame("TRCK", append([]byte{0}, "7/12"...)))
	frames.Write(id3Frame("TYER", append([]byte{0}, "2001"...)))
	apic := append([]byte{0}, "image/jpeg\x00"...)
	apic = append(apic, 3)
	apic = append(apic, "desc\x00"...)
	apic = append(apic, cover...)
	frames.Write(id3Frame("APIC", apic))
	frames.Write(make([]byte, 20)) // remplissage
	var f bytes.Buffer
	f.WriteString("ID3")
	f.Write([]byte{3, 0, 0})
	f.Write(syncsafeBytes(frames.Len()))
	f.Write(frames.Bytes())
	// Trame MPEG-1 couche III, 128 kb/s, 44,1 kHz, stéréo, avec en-tête Xing.
	frame := make([]byte, 417)
	copy(frame, []byte{0xFF, 0xFB, 0x90, 0x00})
	copy(frame[36:], "Xing")
	copy(frame[40:], be32(1))
	copy(frame[44:], be32(1000)) // 1000 trames = 26,12 s
	f.Write(frame)
	p := filepath.Join(t.TempDir(), "a.mp3")
	os.WriteFile(p, f.Bytes(), 0o644)
	r, _ := os.Open(p)
	defer r.Close()
	ti, err := readTags(r, int64(f.Len()), p, ".mp3")
	if err != nil {
		t.Fatal(err)
	}
	if ti.Title != "Titre é" || ti.Artist != "Artiste" || ti.Album != "Album" || ti.Number != 7 || ti.Year != 2001 ||
		len(ti.Genres) != 1 || ti.Genres[0] != "Rock" || ti.SampleRate != 44100 || ti.DurationMs != 26122 {
		t.Fatalf("MP3 : %+v", ti)
	}
	b, err := loadArtBytes(ti.Art)
	if err != nil || !bytes.Equal(b, cover) {
		t.Fatalf("APIC : %v", err)
	}
}

func mp4box(typ string, body ...[]byte) []byte {
	n := 8
	for _, b := range body {
		n += len(b)
	}
	out := append(be32(uint32(n)), typ...)
	for _, b := range body {
		out = append(out, b...)
	}
	return out
}

func TestParseMP4ALAC(t *testing.T) {
	cover := testJPEG(10, 10, color.Black)
	mvhd := make([]byte, 100)
	copy(mvhd[12:], be32(1000))   // échelle
	copy(mvhd[16:], be32(300500)) // 300,5 s
	entry := make([]byte, 28)
	binary.BigEndian.PutUint16(entry[16:], 2)
	binary.BigEndian.PutUint16(entry[18:], 16)
	binary.BigEndian.PutUint16(entry[24:], 44100) // faux : la boîte alac fait foi
	cfg := make([]byte, 28)
	cfg[4+5] = 24
	cfg[4+9] = 2
	copy(cfg[4+20:], be32(176400))
	alac := mp4box("alac", entry, mp4box("alac", cfg))
	stsd := mp4box("stsd", make([]byte, 4), be32(1), alac)
	trak := mp4box("trak", mp4box("mdia", mp4box("minf", mp4box("stbl", stsd))))
	data := func(kind uint32, v []byte) []byte { return mp4box("data", be32(kind), be32(0), v) }
	ilst := mp4box("ilst",
		mp4box("\xa9nam", data(1, []byte("Morceau"))),
		mp4box("\xa9ART", data(1, []byte("Artiste"))),
		mp4box("aART", data(1, []byte("Artiste d'album"))),
		mp4box("\xa9alb", data(1, []byte("Album"))),
		mp4box("\xa9gen", data(1, []byte("Jazz"))),
		mp4box("trkn", data(0, []byte{0, 0, 0, 3, 0, 9, 0, 0})),
		mp4box("disk", data(0, []byte{0, 0, 0, 2, 0, 2})),
		mp4box("\xa9day", data(1, []byte("1999-01-01"))),
		mp4box("covr", data(13, cover)))
	moov := mp4box("moov", mp4box("mvhd", mvhd), trak, mp4box("udta", mp4box("meta", make([]byte, 4), ilst)))
	file := append(mp4box("ftyp", []byte("M4A ")), moov...)
	file = append(file, mp4box("mdat", make([]byte, 50))...)
	p := filepath.Join(t.TempDir(), "a.m4a")
	os.WriteFile(p, file, 0o644)
	r, _ := os.Open(p)
	defer r.Close()
	ti, err := readTags(r, int64(len(file)), p, ".m4a")
	if err != nil {
		t.Fatal(err)
	}
	if ti.Title != "Morceau" || ti.AlbumArtist != "Artiste d'album" || ti.Number != 3 || ti.Disc != 2 || ti.Year != 1999 ||
		ti.SampleRate != 176400 || ti.Bits != 24 || ti.Channels != 2 || ti.DurationMs != 300500 {
		t.Fatalf("M4A : %+v", ti)
	}
	if b, err := loadArtBytes(ti.Art); err != nil || !bytes.Equal(b, cover) {
		t.Fatalf("covr : %v", err)
	}
}

func TestParseWAVAIFFDSF(t *testing.T) {
	dir := t.TempDir()
	// WAV 96 kHz / 24 bits, 2 s, étiquettes LIST/INFO.
	fmtc := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmtc[0:], 1)
	binary.LittleEndian.PutUint16(fmtc[2:], 2)
	binary.LittleEndian.PutUint32(fmtc[4:], 96000)
	binary.LittleEndian.PutUint32(fmtc[8:], 96000*6)
	binary.LittleEndian.PutUint16(fmtc[12:], 6)
	binary.LittleEndian.PutUint16(fmtc[14:], 24)
	info := []byte("INFO")
	for _, kv := range [][2]string{{"INAM", "Piste WAV"}, {"IART", "Artiste WAV"}} {
		v := kv[1] + "\x00"
		if len(v)%2 == 1 {
			v += "\x00"
		}
		info = append(append(append(info, kv[0]...), le32(uint32(len(v)))...), v...)
	}
	chunk := func(id string, b []byte) []byte { return append(append([]byte(id), le32(uint32(len(b)))...), b...) }
	body := append([]byte("WAVE"), chunk("fmt ", fmtc)...)
	body = append(body, chunk("LIST", info)...)
	body = append(body, chunk("data", make([]byte, 96000*6*2))...)
	wav := append(append([]byte("RIFF"), le32(uint32(len(body)))...), body...)
	pw := filepath.Join(dir, "a.wav")
	os.WriteFile(pw, wav, 0o644)
	r, _ := os.Open(pw)
	ti, err := readTags(r, int64(len(wav)), pw, ".wav")
	r.Close()
	if err != nil || ti.Title != "Piste WAV" || ti.Artist != "Artiste WAV" || ti.SampleRate != 96000 || ti.Bits != 24 || ti.DurationMs != 2000 {
		t.Fatalf("WAV : %v %+v", err, ti)
	}

	// AIFF 44,1 kHz, 88200 trames = 2 s.
	comm := make([]byte, 18)
	binary.BigEndian.PutUint16(comm[0:], 2)
	binary.BigEndian.PutUint32(comm[2:], 88200)
	binary.BigEndian.PutUint16(comm[6:], 16)
	copy(comm[8:], []byte{0x40, 0x0E, 0xAC, 0x44, 0, 0, 0, 0, 0, 0}) // 44100
	bchunk := func(id string, b []byte) []byte { return append(append([]byte(id), be32(uint32(len(b)))...), b...) }
	ab := append([]byte("AIFF"), bchunk("COMM", comm)...)
	aiff := append(append([]byte("FORM"), be32(uint32(len(ab)))...), ab...)
	pa := filepath.Join(dir, "a.aiff")
	os.WriteFile(pa, aiff, 0o644)
	r, _ = os.Open(pa)
	ti, err = readTags(r, int64(len(aiff)), pa, ".aiff")
	r.Close()
	if err != nil || ti.SampleRate != 44100 || ti.DurationMs != 2000 || ti.Channels != 2 {
		t.Fatalf("AIFF : %v %+v", err, ti)
	}

	// DSF DSD64, 1 s, ID3 en fin de fichier.
	id3body := id3Frame("TIT2", append([]byte{3}, "Piste DSD"...))
	tag := append(append([]byte("ID3\x03\x00\x00"), syncsafeBytes(len(id3body))...), id3body...)
	dsf := make([]byte, 92)
	copy(dsf, "DSD ")
	binary.LittleEndian.PutUint64(dsf[4:], 28)
	copy(dsf[28:], "fmt ")
	binary.LittleEndian.PutUint64(dsf[32:], 52)
	binary.LittleEndian.PutUint32(dsf[28+24:], 2)
	binary.LittleEndian.PutUint32(dsf[28+28:], 2822400)
	binary.LittleEndian.PutUint32(dsf[28+32:], 1)
	binary.LittleEndian.PutUint64(dsf[28+36:], 2822400)
	dsf = append(dsf, make([]byte, 64)...)
	binary.LittleEndian.PutUint64(dsf[20:], uint64(len(dsf)))
	dsf = append(dsf, tag...)
	pd := filepath.Join(dir, "a.dsf")
	os.WriteFile(pd, dsf, 0o644)
	r, _ = os.Open(pd)
	ti, err = readTags(r, int64(len(dsf)), pd, ".dsf")
	r.Close()
	if err != nil || ti.Title != "Piste DSD" || ti.SampleRate != 2822400 || ti.DurationMs != 1000 {
		t.Fatalf("DSF : %v %+v", err, ti)
	}
}

// Fichiers abîmés : pas de panique, pas de lecture démesurée.
func TestParseCorrupt(t *testing.T) {
	dir := t.TempDir()
	cases := map[string][]byte{
		".flac": append([]byte("fLaC"), 0x84, 0xff, 0xff, 0xff),
		".mp3":  append([]byte("ID3\x04\x00\x00\x7f\x7f\x7f\x7f"), bytes.Repeat([]byte{0xff}, 30)...),
		".m4a":  append(be32(0xffffffff), "moov"...),
		".wav":  append([]byte("RIFF\xff\xff\xff\xffWAVELIST"), 0xff, 0xff, 0xff, 0x7f),
		".dsf":  []byte("DSD "),
	}
	for ext, b := range cases {
		p := filepath.Join(dir, "x"+ext)
		os.WriteFile(p, b, 0o644)
		r, _ := os.Open(p)
		readTags(r, int64(len(b)), p, ext)
		r.Close()
	}
}

// ---------------------------------------------------------------------------
// Bibliothèque de test : indexation complète

type testLib struct {
	root, data string
	cover      []byte
	files      map[string]string // nom court -> chemin
}

func buildTestLibrary(t *testing.T) *testLib {
	t.Helper()
	base := t.TempDir()
	tl := &testLib{root: filepath.Join(base, "Musique"), data: filepath.Join(base, "data"), files: map[string]string{}}
	audio := func(n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(i * 7)
		}
		return b
	}
	tl.cover = testJPEG(1200, 1200, color.RGBA{0, 0, 255, 255})
	// Album avec pochette « cover.jpg » (grande : doit être réduite).
	kob := filepath.Join(tl.root, "Miles Davis", "Kind of Blue")
	for i, title := range []string{"So What", "Freddie Freeloader", "Blue in Green"} {
		p := filepath.Join(kob, "0"+string(rune('1'+i))+" "+title+".flac")
		makeFLAC(t, p, [][2]string{{"TITLE", title}, {"ARTIST", "Miles Davis"}, {"ALBUMARTIST", "Miles Davis"},
			{"ALBUM", "Kind of Blue"}, {"GENRE", "Jazz"}, {"TRACKNUMBER", string(rune('1' + i))}, {"DATE", "1959"}},
			nil, "", 192000, 24, 2, 192000*60, audio(50000))
		tl.files[title] = p
	}
	os.WriteFile(filepath.Join(kob, "cover.jpg"), tl.cover, 0o644)
	// Album sur deux CD, image intégrée seulement, genre « Classical ».
	for d := 1; d <= 2; d++ {
		p := filepath.Join(tl.root, "Bach", "Variations Goldberg", "CD"+string(rune('0'+d)), "01.flac")
		makeFLAC(t, p, [][2]string{{"TITLE", "Aria " + string(rune('0'+d))}, {"ARTIST", "Glenn Gould"}, {"ALBUMARTIST", "Glenn Gould"},
			{"ALBUM", "Goldberg Variations"}, {"GENRE", "Classical"}, {"DISCNUMBER", string(rune('0' + d))}, {"TRACKNUMBER", "1"}, {"DATE", "1981"}},
			testPNG(1600, 1600), "image/png", 44100, 16, 2, 44100*30, audio(2000))
	}
	// Autre album, genre « Classique » (doit fusionner avec Classical),
	// genre multiple « Électronique, Ambient ».
	p := filepath.Join(tl.root, "Divers", "Mix", "01 Piste.flac")
	makeFLAC(t, p, [][2]string{{"TITLE", "Piste"}, {"ARTIST", "Artiste A"}, {"ALBUM", "Mix"},
		{"GENRE", "Classique"}, {"GENRE", "Électronique, Ambient"}}, nil, "", 44100, 16, 2, 44100, audio(100))
	p2 := filepath.Join(tl.root, "Divers", "Mix", "02 Autre.flac")
	makeFLAC(t, p2, [][2]string{{"TITLE", "Autre"}, {"ARTIST", "Artiste B"}, {"ALBUM", "Mix"}, {"GENRE", "Electronic"}},
		nil, "", 44100, 16, 2, 44100, audio(100))
	// Fichier sans étiquettes, rangé Artiste/Album/NN Titre.flac.
	p3 := filepath.Join(tl.root, "Daft Punk", "Discovery", "01 One More Time.flac")
	makeFLAC(t, p3, nil, nil, "", 44100, 16, 2, 44100*320, audio(100))
	os.WriteFile(filepath.Join(filepath.Dir(p3), "Folder.JPG"), testJPEG(300, 300, color.White), 0o644)
	// Dossiers techniques ignorés, lien symbolique ignoré.
	makeFLAC(t, filepath.Join(tl.root, "@Recycle", "x.flac"), nil, nil, "", 44100, 16, 2, 1, nil)
	makeFLAC(t, filepath.Join(tl.root, ".@__thumb", "y.flac"), nil, nil, "", 44100, 16, 2, 1, nil)
	os.Symlink("/etc", filepath.Join(tl.root, "lien"))
	// Playlist relative.
	os.WriteFile(filepath.Join(tl.root, "Ma playlist.m3u"), []byte("#EXTM3U\n#EXTINF:1,x\nMiles Davis/Kind of Blue/02 Freddie Freeloader.flac\nDaft Punk\\Discovery\\01 One More Time.flac\nhttp://radio/x\nabsent.flac\n"), 0o644)
	return tl
}

func scanLibrary(t *testing.T, tl *testLib) (*LibraryService, *Scanner, *Store) {
	t.Helper()
	store, err := OpenStore(tl.data)
	if err != nil {
		t.Fatal(err)
	}
	sc := NewScanner(tl.root, filepath.Join(tl.data, "cache", "index.json"))
	os.MkdirAll(filepath.Join(tl.data, "cache"), 0o700)
	ls := NewLibraryService(store, sc, newArtCache(filepath.Join(tl.data, "cache", "pochettes")), NewTopLists(store, tl.data),
		NewPhotos(filepath.Join(tl.data, "cache", "photos"), store))
	tracks, lists, err := sc.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ls.publish(tracks, lists)
	return ls, sc, store
}

func TestScanAndIndex(t *testing.T) {
	tl := buildTestLibrary(t)
	ls, sc, _ := scanLibrary(t, tl)
	ix := ls.Index()
	if len(ix.Tracks) != 8 {
		t.Fatalf("pistes : %d", len(ix.Tracks))
	}
	if st := sc.Status(); st.Read != 8 || st.Running {
		t.Fatalf("état : %+v", st)
	}
	names := map[string]*Album{}
	for _, a := range ix.Albums {
		names[a.Title] = a
	}
	if len(ix.Albums) != 4 {
		t.Fatalf("albums : %d %v", len(ix.Albums), names)
	}
	gb := names["Goldberg Variations"]
	if gb == nil || len(gb.Tracks) != 2 || gb.Artist != "Glenn Gould" || gb.Tracks[0].Disc != 1 || gb.Art.Mime != "image/png" {
		t.Fatalf("album sur 2 CD : %+v", gb)
	}
	kob := names["Kind of Blue"]
	if kob == nil || !strings.HasSuffix(kob.Art.Path, "cover.jpg") || kob.Tracks[0].Title != "So What" {
		t.Fatalf("Kind of Blue : %+v", kob)
	}
	if mix := names["Mix"]; mix == nil || mix.Artist != variousArtists {
		t.Fatalf("compilation : %+v", mix)
	}
	dp := names["Discovery"]
	if dp == nil || dp.Artist != "Daft Punk" || dp.Tracks[0].Title != "One More Time" || dp.Tracks[0].Number != 1 || !dp.Art.Valid() {
		t.Fatalf("fichier sans étiquettes : %+v %+v", dp, dp.Tracks[0])
	}
	genres := map[string]int{}
	for _, g := range ix.Genres {
		genres[g.Name] = len(g.Albums)
	}
	// Classical + Classique fusionnés ; Électronique + Electronic aussi.
	if genres["Classique"] != 2 || genres["Électronique"] != 1 || genres["Ambient"] != 1 || genres["Jazz"] != 1 || len(genres) != 4 {
		t.Fatalf("genres : %v", genres)
	}
	if len(ix.Playlists) != 1 || len(ix.Playlists[0].Tracks) != 2 || ix.Playlists[0].Name != "Ma playlist" {
		t.Fatalf("playlist : %+v", ix.Playlists)
	}
	for _, tr := range ix.Tracks {
		if strings.Contains(tr.Path, "@Recycle") || strings.Contains(tr.Path, "__thumb") || strings.Contains(tr.Path, "lien") {
			t.Fatalf("fichier ignoré indexé : %s", tr.Path)
		}
	}

	// Deuxième indexation : rien n'est relu ; une piste modifiée l'est.
	time.Sleep(10 * time.Millisecond)
	later := time.Now().Add(time.Minute)
	os.Chtimes(tl.files["So What"], later, later)
	tracks, _, err := sc.Scan(context.Background())
	if err != nil || len(tracks) != 8 {
		t.Fatal(err)
	}
	if st := sc.Status(); st.Read != 1 {
		t.Fatalf("relues : %d", st.Read)
	}
	// Redémarrage : le cache redonne l'index sans parcourir le disque.
	sc2 := NewScanner(tl.root, filepath.Join(tl.data, "cache", "index.json"))
	tr2, pl2, ok := sc2.LoadCache()
	if !ok || len(tr2) != 8 || len(pl2) != 1 {
		t.Fatalf("cache : %v %d %d", ok, len(tr2), len(pl2))
	}
	// Autre dossier musique : cache ignoré.
	if _, _, ok := NewScanner(tl.root+"/Bach", filepath.Join(tl.data, "cache", "index.json")).LoadCache(); ok {
		t.Fatal("cache d'un autre dossier accepté")
	}
}

// ---------------------------------------------------------------------------
// ContentDirectory, fichiers et pochettes

type didl struct {
	Containers []struct {
		ID     string   `xml:"id,attr"`
		Parent string   `xml:"parentID,attr"`
		Count  int      `xml:"childCount,attr"`
		Title  string   `xml:"title"`
		Class  string   `xml:"class"`
		Art    string   `xml:"albumArtURI"`
		Artist []string `xml:"artist"`
	} `xml:"container"`
	Items []struct {
		ID     string `xml:"id,attr"`
		Parent string `xml:"parentID,attr"`
		Title  string `xml:"title"`
		Album  string `xml:"album"`
		Art    string `xml:"albumArtURI"`
		Res    struct {
			URL      string `xml:",chardata"`
			Proto    string `xml:"protocolInfo,attr"`
			Size     int64  `xml:"size,attr"`
			Duration string `xml:"duration,attr"`
			Rate     int    `xml:"sampleFrequency,attr"`
			Bits     int    `xml:"bitsPerSample,attr"`
		} `xml:"res"`
	} `xml:"item"`
}

type browseResult struct {
	D        didl
	Returned int
	Total    int
	Code     int
	Raw      string
}

func cdsCall(t *testing.T, h http.Handler, action string, args [][2]string) (map[string]string, int) {
	t.Helper()
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:` + action + ` xmlns:u="` + cdsType + `">`)
	for _, kv := range args {
		b.WriteString("<" + kv[0] + ">" + xmlEsc(kv[1]) + "</" + kv[0] + ">")
	}
	b.WriteString(`</u:` + action + `></s:Body></s:Envelope>`)
	req := httptest.NewRequest("POST", "/upnp/cds/control", strings.NewReader(b.String()))
	req.Header.Set("SOAPACTION", `"`+cdsType+`#`+action+`"`)
	req.RemoteAddr = "192.168.68.20:5000"
	req.Host = "192.168.68.54:49494"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// Réponse : on relit les éléments de la réponse SOAP.
	out := map[string]string{}
	dec := xml.NewDecoder(strings.NewReader(rec.Body.String()))
	var cur string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch x := tok.(type) {
		case xml.StartElement:
			cur = x.Name.Local
		case xml.CharData:
			if cur != "" {
				out[cur] += string(x)
			}
		case xml.EndElement:
			cur = ""
		}
	}
	return out, rec.Code
}

func browse(t *testing.T, h http.Handler, id, flag string, start, count int) browseResult {
	t.Helper()
	out, code := cdsCall(t, h, "Browse", [][2]string{{"ObjectID", id}, {"BrowseFlag", flag}, {"Filter", "*"},
		{"StartingIndex", itoa(start)}, {"RequestedCount", itoa(count)}, {"SortCriteria", ""}})
	var r browseResult
	r.Code = code
	r.Raw = out["Result"]
	if code == 200 {
		if err := xml.Unmarshal([]byte(out["Result"]), &r.D); err != nil {
			t.Fatalf("DIDL invalide pour %s : %v\n%s", id, err, out["Result"])
		}
	}
	r.Returned = atoi(out["NumberReturned"])
	r.Total = atoi(out["TotalMatches"])
	return r
}

func itoa(i int) string { return strconv.Itoa(i) }
func atoi(s string) int { return leadingInt(s) }

func TestContentDirectory(t *testing.T) {
	tl := buildTestLibrary(t)
	ls, _, store := scanLibrary(t, tl)
	ms := NewMediaServer("Musique NAS", "uuid:11111111-2222-3333-4444-555555555555", "test", ls)
	mux := http.NewServeMux()
	ms.Routes(mux)
	h := lanOnly(mux)

	root := browse(t, h, "0", "BrowseDirectChildren", 0, 0)
	var titles []string
	for _, c := range root.D.Containers {
		titles = append(titles, c.Title)
	}
	if strings.Join(titles, "|") != "Genres|Artistes|Albums|Playlists" || root.Total != 4 {
		t.Fatalf("accueil : %v", titles)
	}
	// Genres : favoris automatiques (les plus fournis) puis « Tous les genres… ».
	g := browse(t, h, "genres", "BrowseDirectChildren", 0, 0)
	last := g.D.Containers[len(g.D.Containers)-1]
	if g.D.Containers[0].Title != "Classique" || last.Title != "Tous les genres…" || last.Count != 4 {
		t.Fatalf("genres : %+v", g.D.Containers)
	}
	if !strings.HasPrefix(g.D.Containers[0].Art, "http://192.168.68.54:49494/art/album/") {
		t.Fatalf("image du genre : %q", g.D.Containers[0].Art)
	}
	// Favoris choisis : ordre respecté, genre inconnu ignoré.
	store.UpdateConfig(func(c *Config) { c.FavoriteGenres = []string{"jazz", "inconnu", "electronique"} })
	g = browse(t, h, "genres", "BrowseDirectChildren", 0, 0)
	if len(g.D.Containers) != 3 || g.D.Containers[0].Title != "Jazz" || g.D.Containers[1].Title != "Électronique" {
		t.Fatalf("favoris : %+v", g.D.Containers)
	}
	// Artistes : pas d'écoutes -> pas de « Les plus écoutés ».
	ar := browse(t, h, "artists", "BrowseDirectChildren", 0, 0)
	if len(ar.D.Containers) != 2 || ar.D.Containers[0].Title != "Derniers ajoutés" {
		t.Fatalf("artistes : %+v", ar.D.Containers)
	}
	ls.top.Record(Scrobble{Artist: "Miles Davis", Album: "Kind of Blue", Track: "So What"})
	ar = browse(t, h, "artists", "BrowseDirectChildren", 0, 0)
	if len(ar.D.Containers) != 3 || ar.D.Containers[0].Title != "Les plus écoutés" {
		t.Fatalf("artistes après écoute : %+v", ar.D.Containers)
	}
	top := browse(t, h, "albums/top", "BrowseDirectChildren", 0, 0)
	if len(top.D.Containers) != 1 || top.D.Containers[0].Title != "Kind of Blue" {
		t.Fatalf("albums les plus écoutés : %+v", top.D.Containers)
	}
	// Tous les albums, avec pagination.
	all := browse(t, h, "albums/all", "BrowseDirectChildren", 1, 2)
	if all.Total != 4 || all.Returned != 2 {
		t.Fatalf("pagination : %d/%d", all.Returned, all.Total)
	}
	var kob string
	full := browse(t, h, "albums/all", "BrowseDirectChildren", 0, 0)
	for _, c := range full.D.Containers {
		if c.Title == "Kind of Blue" {
			kob = c.ID
			if c.Class != "object.container.album.musicAlbum" || len(c.Artist) == 0 || c.Artist[0] != "Miles Davis" || c.Art == "" {
				t.Fatalf("album : %+v", c)
			}
		}
	}
	tr := browse(t, h, kob, "BrowseDirectChildren", 0, 0)
	if len(tr.D.Items) != 3 || tr.D.Items[0].Title != "So What" || tr.D.Items[0].Parent != kob {
		t.Fatalf("pistes : %+v", tr.D.Items)
	}
	it := tr.D.Items[0]
	if it.Res.Rate != 192000 || it.Res.Bits != 24 || it.Res.Duration != "0:01:00.000" ||
		!strings.HasPrefix(it.Res.Proto, "http-get:*:audio/flac:DLNA.ORG_PN=FLAC") || !strings.HasSuffix(it.Res.URL, ".flac") {
		t.Fatalf("ressource : %+v", it.Res)
	}
	// Métadonnées d'une piste : parent = son album.
	md := browse(t, h, it.ID, "BrowseMetadata", 0, 0)
	if len(md.D.Items) != 1 || md.D.Items[0].Parent != kob {
		t.Fatalf("métadonnées : %+v", md.D.Items)
	}
	if r := browse(t, h, "al/inexistant", "BrowseDirectChildren", 0, 0); r.Code != 500 || !strings.Contains(r.Raw, "") {
		t.Fatalf("objet inconnu : %d", r.Code)
	}
	// Recherche.
	out, _ := cdsCall(t, h, "Search", [][2]string{{"ContainerID", "0"}, {"SearchCriteria", `dc:title contains "freddie"`},
		{"Filter", "*"}, {"StartingIndex", "0"}, {"RequestedCount", "10"}})
	if !strings.Contains(out["Result"], "Freddie Freeloader") || out["TotalMatches"] != "1" {
		t.Fatalf("recherche : %v", out)
	}

	// Envoi du fichier : identique octet par octet, reprise (Range).
	u, _ := url.Parse(it.Res.URL)
	want, _ := os.ReadFile(tl.files["So What"])
	req := httptest.NewRequest("GET", u.Path, nil)
	req.RemoteAddr = "192.168.68.58:4000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), want) || rec.Header().Get("Content-Type") != "audio/flac" ||
		rec.Header().Get("transferMode.dlna.org") != "Streaming" {
		t.Fatalf("fichier : %d %d octets", rec.Code, rec.Body.Len())
	}
	req = httptest.NewRequest("GET", u.Path, nil)
	req.RemoteAddr = "192.168.68.58:4000"
	req.Header.Set("Range", "bytes=1000-1999")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 206 || !bytes.Equal(rec.Body.Bytes(), want[1000:2000]) {
		t.Fatalf("Range : %d", rec.Code)
	}
	req = httptest.NewRequest("HEAD", u.Path, nil)
	req.RemoteAddr = "192.168.68.58:4000"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Content-Length") != itoa(len(want)) {
		t.Fatalf("HEAD : %d %q", rec.Code, rec.Header().Get("Content-Length"))
	}
	for _, bad := range []string{"/f/../../etc/passwd", "/f/%2e%2e%2fetc%2fpasswd", "/f/inconnu.flac"} {
		req = httptest.NewRequest("GET", "/x", nil)
		req.URL.Path = bad
		req.URL.RawPath = ""
		req.RemoteAddr = "192.168.68.58:4000"
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == 200 {
			t.Fatalf("%s accessible", bad)
		}
	}
	// Hors réseau local : refusé.
	req = httptest.NewRequest("GET", u.Path, nil)
	req.RemoteAddr = "8.8.8.8:4000"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("accès public : %d", rec.Code)
	}

	// Pochette : grande image réduite à 800 px, en JPEG, puis servie du cache.
	art, _ := url.Parse(tr.D.Items[0].Art)
	req = httptest.NewRequest("GET", art.Path, nil)
	req.RemoteAddr = "192.168.68.20:4000"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	cfg, format, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
	if rec.Code != 200 || err != nil || format != "jpeg" || cfg.Width != 800 {
		t.Fatalf("pochette : %d %v %s %d", rec.Code, err, format, cfg.Width)
	}
	ents, _ := os.ReadDir(filepath.Join(tl.data, "cache", "pochettes"))
	if len(ents) != 1 {
		t.Fatalf("cache des pochettes : %d fichiers", len(ents))
	}
	// Pochette PNG intégrée (Goldberg) : réduite et convertie.
	var gbArt string
	for _, c := range full.D.Containers {
		if c.Title == "Goldberg Variations" {
			gbArt = c.Art
		}
	}
	art, _ = url.Parse(gbArt)
	req = httptest.NewRequest("GET", art.Path, nil)
	req.RemoteAddr = "192.168.68.20:4000"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes())); err != nil || format != "jpeg" || cfg.Width != 800 {
		t.Fatalf("pochette intégrée PNG : %v %s", err, format)
	}

	// Description UPnP et icônes de l'App Center.
	req = httptest.NewRequest("GET", "/description.xml", nil)
	req.RemoteAddr = "192.168.68.20:4000"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "<friendlyName>Musique NAS</friendlyName>") || !strings.Contains(rec.Body.String(), "/icons/icon-120.png") {
		t.Fatal("description")
	}
	for _, ic := range []string{"icon-120.png", "icon-48.jpg", "icon-32.png"} {
		req = httptest.NewRequest("GET", "/icons/"+ic, nil)
		req.RemoteAddr = "192.168.68.20:4000"
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 || rec.Body.Len() < 100 {
			t.Fatalf("icône %s : %d", ic, rec.Code)
		}
	}
	// Abonnement aux événements : rappel vers Internet refusé.
	req = httptest.NewRequest("SUBSCRIBE", "/upnp/cds/event", nil)
	req.RemoteAddr = "192.168.68.20:4000"
	req.Header.Set("CALLBACK", "<http://8.8.8.8/x>")
	req.Header.Set("NT", "upnp:event")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("abonnement externe : %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Photos d'artistes (services simulés)

func TestPhotosDeezerAndTidal(t *testing.T) {
	photo := testJPEG(1000, 1000, color.RGBA{10, 200, 10, 255})
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/deezer":
			q := r.URL.Query().Get("q")
			switch q {
			case "Daft Punk":
				io.WriteString(w, `{"data":[{"name":"Daft Punk Tribute","picture_xl":"`+srv.URL+`/img/x.jpg"},{"name":"Daft Punk","picture_xl":"`+srv.URL+`/img/dp.jpg"}]}`)
			case "Sans Photo":
				io.WriteString(w, `{"data":[{"name":"Sans Photo","picture_xl":"https://e-cdns-images.dzcdn.net/images/artist//1000x1000-000000-80-0-0.jpg"}]}`)
			default:
				io.WriteString(w, `{"data":[]}`)
			}
		case r.URL.Path == "/img/dp.jpg" || r.URL.Path == "/img/tidal.jpg":
			w.Write(photo)
		case r.URL.Path == "/token":
			if u, p, ok := r.BasicAuth(); !ok || u != "client-id-123" || p != "secret-456789" {
				w.WriteHeader(401)
				return
			}
			io.WriteString(w, `{"access_token":"tok","expires_in":3600}`)
		case r.URL.Path == "/v2/searchResults":
			if r.Header.Get("Authorization") != "Bearer tok" || r.URL.Query().Get("filter[query]") != "Miles Davis" {
				w.WriteHeader(400)
				return
			}
			io.WriteString(w, `{"data":[{"id":"Miles Davis","type":"searchResults","relationships":{"artists":{"data":[{"id":"1","type":"artists"},{"id":"2","type":"artists"}]}}}],
			"included":[{"id":"1","type":"artists","attributes":{"name":"Miles Davis Quintet"}},{"id":"2","type":"artists","attributes":{"name":"Miles Davis"}}]}`)
		case r.URL.Path == "/v2/artists/2":
			io.WriteString(w, `{"data":{"id":"2","type":"artists"},"included":[{"id":"a","type":"artworks","attributes":{"files":[
			{"href":"`+srv.URL+`/img/small.jpg","meta":{"width":160,"height":160}},{"href":"`+srv.URL+`/img/tidal.jpg","meta":{"width":750,"height":750}},{"href":"`+srv.URL+`/img/huge.jpg","meta":{"width":3000,"height":3000}}]}}]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	oldD, oldT, oldA := deezerSearchURL, tidalTokenURL, tidalAPI
	deezerSearchURL, tidalTokenURL, tidalAPI = srv.URL+"/deezer", srv.URL+"/token", srv.URL+"/v2"
	defer func() { deezerSearchURL, tidalTokenURL, tidalAPI = oldD, oldT, oldA }()

	dir := t.TempDir()
	store, _ := OpenStore(dir)
	p := NewPhotos(filepath.Join(dir, "photos"), store)
	p.client = srv.Client()
	ctx := context.Background()

	// Aucun service : rien n'est cherché.
	p.pass(ctx, "", []string{"Daft Punk"})
	if p.Has("Daft Punk") {
		t.Fatal("photo sans service")
	}
	store.UpdateConfig(func(c *Config) { c.PhotoProvider = "deezer" })
	p.pass(ctx, "deezer", []string{"Daft Punk", "Sans Photo", "Inconnu"})
	if b, ok := p.Get("daft punk"); !ok || len(b) == 0 {
		t.Fatal("photo Deezer absente")
	} else if cfg, _, _ := image.DecodeConfig(bytes.NewReader(b)); cfg.Width != 800 {
		t.Fatalf("photo non réduite : %d", cfg.Width)
	}
	if p.Has("Sans Photo") || p.Has("Inconnu") {
		t.Fatal("photo générique acceptée")
	}
	if found, missing, _ := p.Status(); found != 1 || missing != 2 {
		t.Fatalf("état : %d/%d", found, missing)
	}
	// 0.0.4 : la partie TIDAL de ce test est retirée avec TIDAL.
	// Changement de service : les photos Deezer ne sont plus utilisées.
	store.UpdateConfig(func(c *Config) { c.PhotoProvider = "" })
	if p.Has("Daft Punk") {
		t.Fatal("photo d'un service non choisi utilisée")
	}
}

// ---------------------------------------------------------------------------
// Classement Last.fm (service simulé)

func TestTopListsLastfm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if strings.ToLower(q.Get("method")) == "user.getrecenttracks" && q.Get("user") == "bertrand" {
			// 0.0.4 : morceau en cours (sans date) ignoré ; titre « Remastered » rapproché.
			io.WriteString(w, `{"recenttracks":{"track":[{"name":"Blue in Green","artist":{"#text":"Miles Davis"},"album":{"#text":"Kind of Blue"},"@attr":{"nowplaying":"true"}},`+
				`{"name":"Freddie Freeloader (Remastered)","artist":{"#text":"Miles Davis"},"album":{"#text":"Kind of Blue"},"date":{"uts":"2000"}},`+
				`{"name":"Inconnu","artist":{"#text":"Absent"},"date":{"uts":"1900"}}]}}`)
			return
		}
		if q.Get("user") != "bertrand" || q.Get("api_key") == "" || q.Get("period") != "12month" {
			io.WriteString(w, `{"error":6,"message":"bad"}`)
			return
		}
		switch strings.ToLower(q.Get("method")) {
		case "user.gettopartists":
			io.WriteString(w, `{"topartists":{"artist":[{"name":"Absent du NAS","playcount":"900"},{"name":"glenn gould","playcount":"50"},{"name":"Miles Davis","playcount":"40"}]}}`)
		case "user.gettopalbums":
			io.WriteString(w, `{"topalbums":{"album":{"name":"Kind of Blue (Legacy Edition)","playcount":"12","artist":{"name":"Miles Davis"}}}}`)
		case "user.gettoptracks":
			io.WriteString(w, `{"toptracks":{"track":[{"name":"So What","playcount":"30","artist":{"name":"MILES DAVIS"}},{"name":"Absent","playcount":"9","artist":{"name":"X"}}]}}`)
		}
	}))
	defer srv.Close()
	old := lastfmEndpoint
	lastfmEndpoint = srv.URL + "/"
	defer func() { lastfmEndpoint = old }()

	tl := buildTestLibrary(t)
	ls, _, store := scanLibrary(t, tl)
	store.UpdateConfig(func(c *Config) { c.Username, c.APIKey = "bertrand", "0123456789abcdef0123456789abcdef" })
	if err := ls.top.fetch(store.Config()); err != nil {
		t.Fatal(err)
	}
	ix := ls.Index()
	ars := ls.topArtists(ix, 9)
	if len(ars) != 2 || ars[0].Name != "Glenn Gould" || ars[1].Name != "Miles Davis" {
		t.Fatalf("artistes : %v", ars)
	}
	als := ls.topAlbums(ix, 9)
	if len(als) != 1 || als[0].Title != "Kind of Blue" {
		t.Fatalf("albums (titre « Legacy Edition » rapproché) : %v", als)
	}
	// Écoutes locales : complètent le classement sans doublon.
	ls.top.Record(Scrobble{Artist: "Daft Punk", Album: "Discovery"})
	ls.top.Record(Scrobble{Artist: "Miles Davis", Album: "Kind of Blue"})
	if ars = ls.topArtists(ix, 9); len(ars) != 3 || ars[2].Name != "Daft Punk" {
		t.Fatalf("artistes complétés : %v", ars)
	}
	// 0.0.4 : morceaux les plus écoutés et derniers écoutés.
	if tr := ls.topTracks(ix, 9); len(tr) != 1 || tr[0].Title != "So What" {
		t.Fatalf("morceaux les plus écoutés : %v", tr)
	}
	ls.top.Record(Scrobble{Artist: "Miles Davis", Track: "Blue in Green", Album: "Kind of Blue", Timestamp: 1000})
	if tr := ls.topTracks(ix, 9); len(tr) != 2 || tr[1].Title != "Blue in Green" {
		t.Fatalf("morceaux complétés par les écoutes locales : %v", tr)
	}
	if _, err := ls.top.fetchRecent(store.Config()); err != nil {
		t.Fatal(err)
	}
	if tr := ls.recentTracks(ix, 9); len(tr) != 2 || tr[0].Title != "Freddie Freeloader" || tr[1].Title != "Blue in Green" {
		t.Fatalf("derniers écoutés (ordre par date, Last.fm et lecteurs mêlés) : %v", tr)
	}
	// Relecture après redémarrage.
	tl2 := NewTopLists(store, tl.data)
	if u, _, na, nb, _ := tl2.Status(); u != "bertrand" || na != 3 || nb != 1 {
		t.Fatalf("relecture : %s %d %d", u, na, nb)
	}
}

// ---------------------------------------------------------------------------
// Page 8790 : réglages de l'accueil

func TestHomeSettingsPage(t *testing.T) {
	tl := buildTestLibrary(t)
	ls, sc, store := scanLibrary(t, tl)
	app := &App{store: store, actors: map[string]*actor{}, kick: make(chan struct{}, 1), lib: ls, scanner: sc,
		top: ls.top, photos: ls.photos, ms: NewMediaServer("Musique NAS", "uuid:1", "t", ls)}
	h := app.httpServer(":0").Handler
	post := func(form url.Values, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/home", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Host = "nas:8790"
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		r.RemoteAddr = "192.168.68.56:5000"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := post(url.Values{"fav": {"jazz"}}, ""); w.Code != 403 {
		t.Fatalf("CSRF : %d", w.Code)
	}
	w := post(url.Values{"fav": {"jazz", "", "classique", "jazz"}, "photo_provider": {"deezer"}}, "http://nas:8790")
	if w.Code != http.StatusSeeOther || strings.Join(store.Config().FavoriteGenres, ",") != "jazz,classique" || store.Config().PhotoProvider != "deezer" {
		t.Fatalf("enregistrement : %d %+v", w.Code, store.Config())
	}
	w = post(url.Values{"fav": {"<script>"}}, "http://nas:8790")
	if !strings.Contains(w.Header().Get("Location"), "inconnu") || len(store.Config().FavoriteGenres) != 2 {
		t.Fatal("genre inconnu accepté")
	}
	w = post(url.Values{"photo_provider": {"qobuz"}}, "http://nas:8790")
	if store.Config().PhotoProvider != "deezer" {
		t.Fatal("service inconnu accepté")
	}
	// 0.0.4 : TIDAL n'est plus proposé.
	w = post(url.Values{"photo_provider": {"tidal"}, "tidal_id": {"client-id-123"}, "tidal_secret": {"tres-secret-987"}}, "http://nas:8790")
	if c := store.Config(); c.PhotoProvider != "deezer" || c.TidalClientSecret != "" {
		t.Fatal("TIDAL accepté")
	}
	if !strings.Contains(w.Header().Get("Location"), "inconnu") || strings.Contains(w.Header().Get("Location"), "ok=1") {
		t.Fatal("refus TIDAL non signalé comme erreur")
	}
	// Le formulaire renvoie toujours les 9 tuiles ; succès = bandeau.
	// 0.0.4 : retour sur l'écran et la rubrique d'origine.
	w = post(url.Values{"fav": {"jazz", "classique", "", "", "", "", "", "", ""}, "photo_provider": {"deezer"}, "back": {"appli"}, "at": {"genres"}}, "http://nas:8790")
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/?p=appli&ok=1&") || !strings.HasSuffix(loc, "#genres") {
		t.Fatalf("succès sans bandeau : %s", w.Header().Get("Location"))
	}
	// Page : genres proposés et tuiles gardant leur valeur, secret jamais
	// réaffiché, contenu échappé.
	r := httptest.NewRequest("GET", "/?p=appli", nil)
	r.RemoteAddr = "192.168.68.56:5000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	body := rec.Body.String()
	if !strings.Contains(body, `<option value="jazz" selected>Jazz (1)</option>`) || !strings.Contains(body, `<option value="classique" selected>`) || strings.Contains(body, "tres-secret-987") ||
		strings.Contains(body, "TIDAL") || strings.Contains(body, "Qobuz") || !strings.Contains(body, "Kind of Blue (Miles Davis)") || !strings.Contains(body, `src="/logo.png"`) {
		t.Fatalf("page :\n%s", body)
	}
	// Bandeau de réussite : affiché avec le message, échappé.
	r = httptest.NewRequest("GET", "/?ok=1&msg="+url.QueryEscape("Enregistré <b>"), nil)
	r.RemoteAddr = "192.168.68.56:5000"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if !strings.Contains(rec.Body.String(), `<p class="toast good" role="status">✓ Enregistré &lt;b&gt;</p>`) {
		t.Fatal("bandeau absent ou non échappé")
	}
}
