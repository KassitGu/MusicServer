package main

// ContentDirectory : l'arborescence que l'application de contrôle affiche.
//
//   0  Musique NAS
//   ├── genres          Genres : favoris (9 au plus) puis « Tous les genres… »
//   ├── artists         Artistes : plus écoutés, derniers ajoutés, tous
//   ├── albums          Albums : plus écoutés, derniers ajoutés, tous
//   └── playlists       Playlists (.m3u de la bibliothèque)
//
// Identifiants : g/<clé> genre, ar/<clé> artiste, al/<clé> album,
// t/<id> piste, pl/<clé> playlist. Les clés sont stables d'une indexation
// à l'autre (dérivées des noms et chemins), pour que les favoris de
// l'application de contrôle restent valides.

import (
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
)

const tileCount = 9

type didlEntry struct {
	container bool
	id        string
	title     string
	class     string
	children  int
	art       string
	artist    string
	genre     string
	year      int
	track     *Track
	album     *Album
}

func (ms *MediaServer) handleCDS(w http.ResponseWriter, r *http.Request) {
	action, args, err := parseSOAP(r)
	if err != nil {
		soapFault(w, 402, "Invalid Args")
		return
	}
	base := "http://" + r.Host
	uid := strconv.FormatUint(uint64(ms.updateID.Load()), 10)
	switch action {
	case "GetSearchCapabilities":
		soapReply(w, cdsType, action, [][2]string{{"SearchCaps", "dc:title,upnp:artist,upnp:album,upnp:genre,upnp:class"}})
	case "GetSortCapabilities":
		soapReply(w, cdsType, action, [][2]string{{"SortCaps", ""}})
	case "GetSystemUpdateID":
		soapReply(w, cdsType, action, [][2]string{{"Id", uid}})
	case "Browse":
		start, _ := strconv.Atoi(args["StartingIndex"])
		count, _ := strconv.Atoi(args["RequestedCount"])
		id := args["ObjectID"]
		var entries []didlEntry
		var parent string
		total := 0
		switch args["BrowseFlag"] {
		case "BrowseMetadata":
			e, p, ok := ms.lib.metadata(id)
			if !ok {
				soapFault(w, 701, "No such object")
				return
			}
			entries, parent, total = []didlEntry{e}, p, 1
		case "BrowseDirectChildren":
			list, ok := ms.lib.children(id)
			if !ok {
				soapFault(w, 701, "No such object")
				return
			}
			total = len(list)
			entries = page(list, start, count)
			parent = id
		default:
			soapFault(w, 402, "Invalid Args")
			return
		}
		didl := ms.lib.renderDIDL(entries, parent, base)
		soapReply(w, cdsType, action, [][2]string{{"Result", didl}, {"NumberReturned", strconv.Itoa(len(entries))},
			{"TotalMatches", strconv.Itoa(total)}, {"UpdateID", uid}})
	case "Search":
		start, _ := strconv.Atoi(args["StartingIndex"])
		count, _ := strconv.Atoi(args["RequestedCount"])
		list := ms.lib.search(args["SearchCriteria"])
		entries := page(list, start, count)
		didl := ms.lib.renderDIDL(entries, "", base)
		soapReply(w, cdsType, action, [][2]string{{"Result", didl}, {"NumberReturned", strconv.Itoa(len(entries))},
			{"TotalMatches", strconv.Itoa(len(list))}, {"UpdateID", uid}})
	default:
		soapFault(w, 401, "Invalid Action")
	}
}

func page(list []didlEntry, start, count int) []didlEntry {
	if start < 0 || start >= len(list) {
		return nil
	}
	end := len(list)
	if count > 0 && start+count < end {
		end = start + count
	}
	return list[start:end]
}

// ---------------------------------------------------------------------------
// Construction des entrées

func folder(id, title string, n int) didlEntry {
	return didlEntry{container: true, id: id, title: title, class: "object.container.storageFolder", children: n}
}

func (ls *LibraryService) genreEntry(g *Genre) didlEntry {
	e := didlEntry{container: true, id: "g/" + g.Key, title: g.Name, class: "object.container.genre.musicGenre", children: len(g.Albums)}
	if a := latestWithArt(g.Albums); a != nil {
		e.art = "/art/album/" + a.Key + ".jpg"
	}
	return e
}

// artistEntry : photo de l'artiste si le service choisi l'a fournie, sinon
// pochette de son dernier album (adresse différente : l'application de contrôle
// recharge l'image quand la photo arrive).
func (ls *LibraryService) artistEntry(ar *Artist) didlEntry {
	e := didlEntry{container: true, id: "ar/" + ar.Key, title: ar.Name, class: "object.container.person.musicArtist",
		children: len(ar.Albums)}
	if ls.photos != nil && ls.photos.Has(ar.Name) {
		e.art = "/art/artist/" + ar.Key + ".jpg"
	} else if a := latestWithArt(ar.Albums); a != nil {
		e.art = "/art/album/" + a.Key + ".jpg"
	}
	return e
}

func albumEntry(a *Album) didlEntry {
	e := didlEntry{container: true, id: "al/" + a.Key, title: a.Title, class: "object.container.album.musicAlbum",
		children: len(a.Tracks), artist: a.Artist, year: a.Year, album: a}
	if a.Art.Valid() {
		// 0.0.4 : tuile d'album avec badge de qualité (ancienne adresse
		// "/art/album/", sans badge, gardée pour genres et pistes).
		// 0.0.6 : le code du dessin (b3-…) fait partie de l'adresse. Les
		// applications gardent les images en mémoire par adresse : en 0.0.5,
		// l'adresse inchangée leur faisait garder l'ancien badge 0.0.4.
		// Ancien code : e.art = "/art/badge/" + a.Key + ".jpg"
		e.art = "/art/badge/" + a.Key + "-" + albumQuality(a).code() + ".jpg"
	}
	if len(a.Genres) > 0 {
		e.genre = a.Genres[0].Name
	}
	return e
}

func trackEntry(t *Track) didlEntry {
	return didlEntry{id: "t/" + t.ID, title: t.Title, class: "object.item.audioItem.musicTrack", track: t}
}

func latestWithArt(list []*Album) *Album {
	var best *Album
	for _, a := range list {
		if a.Art.Valid() && (best == nil || a.Added > best.Added) {
			best = a
		}
	}
	return best
}

func (ls *LibraryService) children(id string) ([]didlEntry, bool) {
	ix := ls.Index()
	var out []didlEntry
	switch {
	case id == "0":
		out = append(out, folder("genres", "Genres", len(ls.favoriteGenres(ix))+1))
		n := 2
		if len(ls.topArtists(ix, tileCount)) > 0 {
			n++
		}
		out = append(out, folder("artists", "Artistes", n))
		n = 2
		if len(ls.topAlbums(ix, tileCount)) > 0 {
			n++
		}
		out = append(out, folder("albums", "Albums", n))
		if len(ix.Playlists) > 0 {
			out = append(out, folder("playlists", "Playlists", len(ix.Playlists)))
		}
	case id == "genres":
		for _, g := range ls.favoriteGenres(ix) {
			out = append(out, ls.genreEntry(g))
		}
		out = append(out, folder("genres/all", "Tous les genres…", len(ix.Genres)))
	case id == "genres/all":
		for _, g := range ix.Genres {
			out = append(out, ls.genreEntry(g))
		}
	case id == "artists":
		if top := ls.topArtists(ix, tileCount); len(top) > 0 {
			out = append(out, folder("artists/top", "Les plus écoutés", len(top)))
		}
		out = append(out, folder("artists/recent", "Derniers ajoutés", len(ix.RecentArtists(tileCount))))
		out = append(out, folder("artists/all", "Tous les artistes…", len(ix.Artists)))
	case id == "artists/top":
		for _, ar := range ls.topArtists(ix, tileCount) {
			out = append(out, ls.artistEntry(ar))
		}
	case id == "artists/recent":
		for _, ar := range ix.RecentArtists(tileCount) {
			out = append(out, ls.artistEntry(ar))
		}
	case id == "artists/all":
		for _, ar := range ix.Artists {
			out = append(out, ls.artistEntry(ar))
		}
	case id == "albums":
		if top := ls.topAlbums(ix, tileCount); len(top) > 0 {
			out = append(out, folder("albums/top", "Les plus écoutés", len(top)))
		}
		out = append(out, folder("albums/recent", "Derniers ajoutés", min(tileCount, len(ix.Recent))))
		out = append(out, folder("albums/all", "Tous les albums…", len(ix.Albums)))
	case id == "albums/top":
		for _, a := range ls.topAlbums(ix, tileCount) {
			out = append(out, albumEntry(a))
		}
	case id == "albums/recent":
		for i, a := range ix.Recent {
			if i == tileCount {
				break
			}
			out = append(out, albumEntry(a))
		}
	case id == "albums/all":
		for _, a := range ix.Albums {
			out = append(out, albumEntry(a))
		}
	case id == "playlists":
		for _, p := range ix.Playlists {
			out = append(out, didlEntry{container: true, id: "pl/" + p.Key, title: p.Name,
				class: "object.container.playlistContainer", children: len(p.Tracks)})
		}
	case strings.HasPrefix(id, "g/"):
		g := ix.Genre(id[2:])
		if g == nil {
			return nil, false
		}
		for _, a := range g.Albums {
			out = append(out, albumEntry(a))
		}
	case strings.HasPrefix(id, "ar/"):
		ar := ix.Artist(id[3:])
		if ar == nil {
			return nil, false
		}
		for _, a := range ar.Albums {
			out = append(out, albumEntry(a))
		}
	case strings.HasPrefix(id, "al/"):
		a := ix.Album(id[3:])
		if a == nil {
			return nil, false
		}
		for _, t := range a.Tracks {
			out = append(out, trackEntry(t))
		}
	case strings.HasPrefix(id, "pl/"):
		p := ix.Playlist(id[3:])
		if p == nil {
			return nil, false
		}
		for _, t := range p.Tracks {
			out = append(out, trackEntry(t))
		}
	default:
		return nil, false
	}
	if out == nil {
		out = []didlEntry{}
	}
	return out, true
}

// metadata : l'objet lui-même et son parent (BrowseMetadata).
func (ls *LibraryService) metadata(id string) (didlEntry, string, bool) {
	ix := ls.Index()
	switch id {
	case "0":
		return folder("0", "Musique NAS", 4), "-1", true
	case "genres", "artists", "albums", "playlists":
		list, _ := ls.children("0")
		for _, e := range list {
			if e.id == id {
				return e, "0", true
			}
		}
		return folder(id, "Playlists", 0), "0", true
	case "genres/all", "artists/top", "artists/recent", "artists/all", "albums/top", "albums/recent", "albums/all":
		parent := id[:strings.IndexByte(id, '/')]
		list, _ := ls.children(parent)
		for _, e := range list {
			if e.id == id {
				return e, parent, true
			}
		}
		return folder(id, id, 0), parent, true
	}
	switch {
	case strings.HasPrefix(id, "g/"):
		if g := ix.Genre(id[2:]); g != nil {
			return ls.genreEntry(g), "genres/all", true
		}
	case strings.HasPrefix(id, "ar/"):
		if ar := ix.Artist(id[3:]); ar != nil {
			return ls.artistEntry(ar), "artists/all", true
		}
	case strings.HasPrefix(id, "al/"):
		if a := ix.Album(id[3:]); a != nil {
			return albumEntry(a), "albums/all", true
		}
	case strings.HasPrefix(id, "pl/"):
		if p := ix.Playlist(id[3:]); p != nil {
			return didlEntry{container: true, id: id, title: p.Name, class: "object.container.playlistContainer", children: len(p.Tracks)}, "playlists", true
		}
	case strings.HasPrefix(id, "t/"):
		if t := ix.Tracks[id[2:]]; t != nil {
			parent := "0"
			if a := ix.AlbumOfTrack(t); a != nil {
				parent = "al/" + a.Key
			}
			return trackEntry(t), parent, true
		}
	}
	return didlEntry{}, "", false
}

// ---------------------------------------------------------------------------
// Recherche (simple) : titres, artistes, albums contenant le texte cherché.

var critValue = regexp.MustCompile(`(dc:title|upnp:artist|upnp:album|upnp:genre|dc:creator)\s+(contains|=)\s+"((?:[^"\\]|\\.)*)"`)
var critClass = regexp.MustCompile(`upnp:class\s+(derivedfrom|=)\s+"([^"]*)"`)

func (ls *LibraryService) search(criteria string) []didlEntry {
	ix := ls.Index()
	var terms []string
	for _, m := range critValue.FindAllStringSubmatch(criteria, -1) {
		if v := fold(strings.ReplaceAll(m[3], `\"`, `"`)); v != "" {
			terms = append(terms, v)
		}
	}
	class := ""
	if m := critClass.FindStringSubmatch(criteria); m != nil {
		class = m[2]
	}
	if len(terms) == 0 {
		return nil
	}
	match := func(fields ...string) bool {
		f := fold(strings.Join(fields, " "))
		for _, t := range terms {
			if !strings.Contains(f, t) {
				return false
			}
		}
		return true
	}
	var out []didlEntry
	const limit = 200
	if class == "" || strings.HasPrefix("object.container.person.musicArtist", class) && class != "object.container" {
		for _, ar := range ix.Artists {
			if len(out) < limit && match(ar.Name) {
				out = append(out, ls.artistEntry(ar))
			}
		}
	}
	if class == "" || strings.HasPrefix("object.container.album.musicAlbum", class) {
		for _, a := range ix.Albums {
			if len(out) < limit && match(a.Title, a.Artist) {
				out = append(out, albumEntry(a))
			}
		}
	}
	if class == "" || strings.HasPrefix(class, "object.item") {
		for _, a := range ix.Albums {
			for _, t := range a.Tracks {
				if len(out) < limit && match(t.Title, t.Artist, a.Title) {
					out = append(out, trackEntry(t))
				}
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// DIDL-Lite

func fmtDuration(ms int64) string {
	s := ms / 1000
	return fmt.Sprintf("%d:%02d:%02d.%03d", s/3600, (s/60)%60, s%60, ms%1000)
}

func dlnaProfile(mime string) string {
	switch mime {
	case "audio/flac", "audio/x-flac":
		return "DLNA.ORG_PN=FLAC;"
	case "audio/mpeg":
		return "DLNA.ORG_PN=MP3;"
	}
	return ""
}

func protocolInfo(mime string) string {
	return "http-get:*:" + mime + ":" + dlnaProfile(mime) + "DLNA.ORG_OP=01;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=01700000000000000000000000000000"
}

func (ls *LibraryService) renderDIDL(entries []didlEntry, parent, base string) string {
	ix := ls.Index()
	var b strings.Builder
	b.WriteString(`<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/" xmlns:dlna="urn:schemas-dlna-org:metadata-1-0/">`)
	tag := func(name, val string) {
		if val != "" {
			b.WriteString("<" + name + ">" + xmlEsc(val) + "</" + name + ">")
		}
	}
	art := func(u string) {
		if u != "" {
			b.WriteString(`<upnp:albumArtURI dlna:profileID="JPEG_TN">` + xmlEsc(base+u) + `</upnp:albumArtURI>`)
		}
	}
	for _, e := range entries {
		p := parent
		if p == "" || e.id == parent {
			// Recherche ou métadonnées : parent calculé.
			if _, pp, ok := ls.metadata(e.id); ok {
				p = pp
			}
		}
		if e.container {
			b.WriteString(`<container id="` + xmlEsc(e.id) + `" parentID="` + xmlEsc(p) + `" restricted="1" searchable="1" childCount="` + strconv.Itoa(e.children) + `">`)
			tag("dc:title", e.title)
			tag("upnp:class", e.class)
			if e.artist != "" {
				tag("upnp:artist", e.artist)
				b.WriteString(`<upnp:artist role="AlbumArtist">` + xmlEsc(e.artist) + `</upnp:artist>`)
				tag("dc:creator", e.artist)
			}
			tag("upnp:genre", e.genre)
			if e.year > 0 {
				tag("dc:date", fmt.Sprintf("%04d-01-01", e.year))
			}
			art(e.art)
			b.WriteString(`</container>`)
			continue
		}
		t := e.track
		a := ix.AlbumOfTrack(t)
		title := t.Title
		if title == "" {
			title = strings.TrimSuffix(path.Base(t.Path), path.Ext(t.Path))
		}
		b.WriteString(`<item id="` + xmlEsc(e.id) + `" parentID="` + xmlEsc(p) + `" restricted="1">`)
		tag("dc:title", title)
		tag("upnp:class", e.class)
		artist := t.Artist
		if artist == "" && a != nil {
			artist = a.Artist
		}
		tag("dc:creator", artist)
		tag("upnp:artist", artist)
		if a != nil {
			b.WriteString(`<upnp:artist role="AlbumArtist">` + xmlEsc(a.Artist) + `</upnp:artist>`)
			tag("upnp:album", a.Title)
			if len(a.Genres) > 0 {
				tag("upnp:genre", a.Genres[0].Name)
			}
			if a.Art.Valid() {
				art("/art/album/" + a.Key + ".jpg")
			}
		}
		tag("upnp:composer", t.Composer)
		if t.Number > 0 {
			tag("upnp:originalTrackNumber", strconv.Itoa(t.Number))
		}
		if t.Disc > 0 {
			tag("upnp:originalDiscNumber", strconv.Itoa(t.Disc))
		}
		if t.Year > 0 {
			tag("dc:date", fmt.Sprintf("%04d-01-01", t.Year))
		}
		b.WriteString(`<res protocolInfo="` + xmlEsc(protocolInfo(t.Mime)) + `" size="` + strconv.FormatInt(t.Size, 10) + `"`)
		if t.DurationMs > 0 {
			b.WriteString(` duration="` + fmtDuration(t.DurationMs) + `"`)
		}
		if t.SampleRate > 0 {
			b.WriteString(` sampleFrequency="` + strconv.Itoa(t.SampleRate) + `"`)
		}
		if t.Bits > 0 {
			b.WriteString(` bitsPerSample="` + strconv.Itoa(t.Bits) + `"`)
		}
		if t.Channels > 0 {
			b.WriteString(` nrAudioChannels="` + strconv.Itoa(t.Channels) + `"`)
		}
		if t.DurationMs > 0 && t.Size > 0 {
			b.WriteString(` bitrate="` + strconv.FormatInt(t.Size*1000/t.DurationMs, 10) + `"`)
		}
		b.WriteString(`>` + xmlEsc(base+trackURL(t)) + `</res>`)
		b.WriteString(`</item>`)
	}
	b.WriteString(`</DIDL-Lite>`)
	return b.String()
}

func trackURL(t *Track) string {
	ext := strings.ToLower(path.Ext(t.Path))
	if ext == "" || len(ext) > 6 {
		ext = ".bin"
	}
	return "/f/" + t.ID + ext
}
