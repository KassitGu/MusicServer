package main

// Minimal OpenHome client: SSDP discovery of players (DS/DSM…) and SOAP calls
// to the Info, Time, Product and Playlist services.
// OpenHome players expose these standard services; we read what is actually
// playing rather than what the media server sent (players preload the next
// track for gapless playback).
// Client OpenHome minimal : découverte SSDP des lecteurs (DS/DSM…) et
// appels SOAP aux services Info, Time, Product et Playlist.
// Les lecteurs OpenHome exposent ces services standard ; on lit ce qui
// est réellement joué plutôt que ce que le serveur de médias a envoyé
// (les lecteurs préchargent la piste suivante pour le gapless).

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const ssdpAddr = "239.255.255.250:1900"
const infoServiceType = "urn:av-openhome-org:service:Info:1"

var httpClient = &http.Client{Timeout: 8 * time.Second}

// Renderer describes an OpenHome player and its useful control URLs.
// Renderer décrit un lecteur OpenHome et les URL de contrôle utiles.
type Renderer struct {
	Location   string
	UDN        string
	Name       string
	Brand      string // manufacturer and model / fabricant et modèle
	InfoURL    string
	InfoType   string
	TimeURL    string
	TimeType   string
	ProductURL string
	ProdType   string
	// Playlist service (play queue), used to save the queue as a server
	// playlist from the settings page. Missing on some players.
	// Service Playlist (file d'attente), pour l'enregistrer en playlist du
	// serveur depuis la page de réglages. Absent sur certains lecteurs.
	PlaylistURL  string
	PlaylistType string
}

// DiscoverRenderers sends an SSDP M-SEARCH (twice, as UDP may lose packets)
// and returns the description URLs of devices exposing the OpenHome Info service.
// DiscoverRenderers envoie un M-SEARCH SSDP (deux fois, UDP pouvant perdre des
// paquets) et renvoie les URL de description des appareils qui exposent le
// service OpenHome Info.
func DiscoverRenderers(ctx context.Context, wait time.Duration) ([]string, error) {
	return discover(ctx, infoServiceType, wait)
}

// mediaRendererType is the standard UPnP/DLNA player device, OpenHome or not.
// mediaRendererType est l'appareil lecteur standard UPnP/DLNA, OpenHome ou non.
const mediaRendererType = "urn:schemas-upnp-org:device:MediaRenderer:1"

// DiscoverMediaRenderers returns the description URLs of the standard UPnP
// players (to list those that are not OpenHome).
// DiscoverMediaRenderers renvoie les URL de description des lecteurs UPnP
// standard (pour lister ceux qui ne sont pas OpenHome).
func DiscoverMediaRenderers(ctx context.Context, wait time.Duration) ([]string, error) {
	return discover(ctx, mediaRendererType, wait)
}

// discover sends an SSDP M-SEARCH for st and collects the LOCATION headers.
// discover envoie un M-SEARCH SSDP pour st et recueille les en-têtes LOCATION.
func discover(ctx context.Context, st string, wait time.Duration) ([]string, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	dst, _ := net.ResolveUDPAddr("udp4", ssdpAddr)
	msg := "M-SEARCH * HTTP/1.1\r\n" +
		"HOST: 239.255.255.250:1900\r\n" +
		"MAN: \"ssdp:discover\"\r\n" +
		"MX: 3\r\n" +
		"ST: " + st + "\r\n\r\n"
	for i := 0; i < 2; i++ {
		if _, err := conn.WriteToUDP([]byte(msg), dst); err != nil {
			return nil, err
		}
	}
	deadline := time.Now().Add(wait)
	seen := map[string]bool{}
	var out []string
	buf := make([]byte, 4096)
	for {
		if ctx.Err() != nil {
			break
		}
		conn.SetReadDeadline(deadline)
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}
		loc := headerValue(string(buf[:n]), "location")
		if loc != "" && !seen[loc] {
			seen[loc] = true
			out = append(out, loc)
		}
	}
	return out, nil
}

// headerValue returns a header of a raw SSDP reply (case-insensitive name).
// headerValue renvoie un en-tête d'une réponse SSDP brute (nom sans casse).
func headerValue(resp, name string) string {
	for _, line := range strings.Split(resp, "\r\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(k), name) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

type xmlService struct {
	ServiceType string `xml:"serviceType"`
	ControlURL  string `xml:"controlURL"`
}

type xmlDevice struct {
	FriendlyName string       `xml:"friendlyName"`
	Manufacturer string       `xml:"manufacturer"`
	ModelName    string       `xml:"modelName"`
	UDN          string       `xml:"UDN"`
	Services     []xmlService `xml:"serviceList>service"`
	Devices      []xmlDevice  `xml:"deviceList>device"`
}

type xmlRoot struct {
	URLBase string    `xml:"URLBase"`
	Device  xmlDevice `xml:"device"`
}

// LoadRenderer reads the UPnP description and finds the OpenHome services,
// including those of embedded devices.
// LoadRenderer lit la description UPnP et repère les services OpenHome,
// y compris ceux des appareils imbriqués.
func LoadRenderer(location string) (*Renderer, error) {
	resp, err := httpClient.Get(location)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("description %s: HTTP %d", location, resp.StatusCode)
	}
	var root xmlRoot
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&root); err != nil {
		return nil, err
	}
	base := location
	if root.URLBase != "" {
		base = root.URLBase
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	r := &Renderer{Location: location}
	var walk func(d xmlDevice)
	walk = func(d xmlDevice) {
		for _, s := range d.Services {
			ctrl, err := baseURL.Parse(s.ControlURL)
			if err != nil {
				continue
			}
			switch {
			case strings.HasPrefix(s.ServiceType, "urn:av-openhome-org:service:Info:"):
				if r.InfoURL == "" {
					r.InfoURL, r.InfoType = ctrl.String(), s.ServiceType
					r.Name, r.UDN = d.FriendlyName, d.UDN
					r.Brand = brand(d)
				}
			case strings.HasPrefix(s.ServiceType, "urn:av-openhome-org:service:Time:"):
				if r.TimeURL == "" {
					r.TimeURL, r.TimeType = ctrl.String(), s.ServiceType
				}
			case strings.HasPrefix(s.ServiceType, "urn:av-openhome-org:service:Playlist:"):
				if r.PlaylistURL == "" {
					r.PlaylistURL, r.PlaylistType = ctrl.String(), s.ServiceType
				}
			case strings.HasPrefix(s.ServiceType, "urn:av-openhome-org:service:Product:"):
				if r.ProductURL == "" {
					r.ProductURL, r.ProdType = ctrl.String(), s.ServiceType
				}
			}
		}
		for _, c := range d.Devices {
			walk(c)
		}
	}
	walk(root.Device)
	if r.InfoURL == "" || r.TimeURL == "" {
		return nil, errors.New("OpenHome Info/Time services missing")
	}
	if r.UDN == "" {
		r.UDN = location
	}
	return r, nil
}

// brand joins the manufacturer and the model of a device, without repeating
// a model name that already starts with the manufacturer.
// brand réunit le fabricant et le modèle d'un appareil, sans répéter un nom
// de modèle qui commence déjà par le fabricant.
func brand(d xmlDevice) string {
	m, mo := strings.TrimSpace(d.Manufacturer), strings.TrimSpace(d.ModelName)
	switch {
	case m == "":
		return mo
	case mo == "" || strings.HasPrefix(strings.ToLower(mo), strings.ToLower(strings.Fields(m)[0])):
		return firstNonEmpty(mo, m)
	}
	return m + " " + mo
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// OtherPlayer is a UPnP/DLNA player that is not OpenHome: it can play music
// from the server through its own app, but cannot be followed.
// OtherPlayer est un lecteur UPnP/DLNA qui n'est pas OpenHome : il peut jouer
// la musique du serveur avec sa propre application, mais ne peut pas être suivi.
type OtherPlayer struct {
	Name, Brand, Host string
}

// LoadOtherPlayer reads a player description and returns it when it has no
// OpenHome service (nil otherwise).
// LoadOtherPlayer lit la description d'un lecteur et la renvoie s'il n'a
// aucun service OpenHome (nil sinon).
func LoadOtherPlayer(location string) (*OtherPlayer, error) {
	resp, err := httpClient.Get(location)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("description %s: HTTP %d", location, resp.StatusCode)
	}
	var root xmlRoot
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&root); err != nil {
		return nil, err
	}
	openHome := false
	var walk func(d xmlDevice)
	walk = func(d xmlDevice) {
		for _, s := range d.Services {
			if strings.HasPrefix(s.ServiceType, "urn:av-openhome-org:") {
				openHome = true
			}
		}
		for _, c := range d.Devices {
			walk(c)
		}
	}
	walk(root.Device)
	if openHome {
		return nil, nil
	}
	u, _ := url.Parse(location)
	host := ""
	if u != nil {
		host = u.Hostname()
	}
	return &OtherPlayer{Name: firstNonEmpty(root.Device.FriendlyName, host), Brand: brand(root.Device), Host: host}, nil
}

// soapCall runs a SOAP action and returns the output arguments. args are
// name/value pairs, sent in that order: UPnP stacks may expect the order of
// the service description (Playlist Insert: AfterId, Uri, Metadata).
// soapCall exécute une action SOAP et renvoie les arguments de sortie. args
// est une suite de paires nom/valeur, envoyées dans cet ordre : les piles
// UPnP peuvent exiger l'ordre de la description du service (Insert de
// Playlist : AfterId, Uri, Metadata).
func soapCall(ctrlURL, serviceType, action string, args ...string) (map[string]string, error) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	fmt.Fprintf(&b, `<u:%s xmlns:u="%s">`, action, serviceType)
	for i := 0; i+1 < len(args); i += 2 {
		fmt.Fprintf(&b, "<%s>", args[i])
		xml.EscapeText(&b, []byte(args[i+1]))
		fmt.Fprintf(&b, "</%s>", args[i])
	}
	fmt.Fprintf(&b, `</u:%s></s:Body></s:Envelope>`, action)
	req, err := http.NewRequest(http.MethodPost, ctrlURL, strings.NewReader(b.String()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPACTION", fmt.Sprintf(`"%s#%s"`, serviceType, action))
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", action, resp.StatusCode)
	}
	return parseSoapResponse(body, action+"Response")
}

// parseSoapResponse collects the child elements of <ActionResponse>.
// parseSoapResponse récupère les éléments enfants de <ActionResponse>.
func parseSoapResponse(body []byte, respName string) (map[string]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	out := map[string]string{}
	inResp := false
	var cur string
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == respName {
				inResp = true
			} else if inResp && cur == "" {
				cur = t.Name.Local
				text.Reset()
			}
		case xml.CharData:
			if cur != "" {
				text.Write(t)
			}
		case xml.EndElement:
			if inResp && t.Name.Local == cur {
				out[cur] = text.String()
				cur = ""
			} else if t.Name.Local == respName {
				return out, nil
			}
		}
	}
	if !inResp {
		return nil, errors.New("unexpected SOAP response")
	}
	return out, nil
}

// QueueEntry is one track of a player's play queue.
// QueueEntry est un morceau de la file d'attente d'un lecteur.
type QueueEntry struct {
	URI  string
	Meta TrackMeta
}

const maxQueueRead = 5000 // tracks read at most / morceaux lus au plus

// errNoPlayQueue: the player has no OpenHome Playlist service.
// errNoPlayQueue : le lecteur n'a pas de service OpenHome Playlist.
var errNoPlayQueue = errors.New("this player does not expose an OpenHome play queue")

// Queue reads the player's play queue (OpenHome Playlist service: IdArray,
// then ReadList in batches of 100 IDs), in order.
// Queue lit la file d'attente du lecteur (service OpenHome Playlist :
// IdArray, puis ReadList par paquets de 100 identifiants), dans l'ordre.
func (r *Renderer) Queue() ([]QueueEntry, error) {
	if r.PlaylistURL == "" {
		return nil, errNoPlayQueue
	}
	m, err := soapCall(r.PlaylistURL, r.PlaylistType, "IdArray")
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(m["Array"]))
	if err != nil {
		return nil, fmt.Errorf("unreadable IdArray: %v", err)
	}
	// IdArray is a sequence of big-endian 32-bit IDs.
	// IdArray est une suite d'identifiants 32 bits gros-boutistes.
	var ids []string
	for i := 0; i+4 <= len(raw) && len(ids) < maxQueueRead; i += 4 {
		ids = append(ids, strconv.FormatUint(uint64(binary.BigEndian.Uint32(raw[i:])), 10))
	}
	var out []QueueEntry
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		res, err := soapCall(r.PlaylistURL, r.PlaylistType, "ReadList", "IdList", strings.Join(ids[start:end], " "))
		if err != nil {
			return nil, err
		}
		entries, err := parseTrackList(res["TrackList"])
		if err != nil {
			return nil, err
		}
		// ReadList may return the entries in another order: restore
		// the IdArray order.
		// ReadList peut renvoyer les entrées dans un autre ordre : on
		// remet celui de IdArray.
		for _, id := range ids[start:end] {
			if e, ok := entries[id]; ok {
				out = append(out, e)
			}
		}
	}
	return out, nil
}

// parseTrackList reads <TrackList><Entry><Id/><Uri/><Metadata/></Entry>…
// parseTrackList lit <TrackList><Entry><Id/><Uri/><Metadata/></Entry>…
func parseTrackList(s string) (map[string]QueueEntry, error) {
	var tl struct {
		Entries []struct {
			ID       string `xml:"Id"`
			URI      string `xml:"Uri"`
			Metadata string `xml:"Metadata"`
		} `xml:"Entry"`
	}
	if err := xml.Unmarshal([]byte(s), &tl); err != nil {
		return nil, fmt.Errorf("unreadable TrackList: %v", err)
	}
	out := map[string]QueueEntry{}
	for _, e := range tl.Entries {
		out[strings.TrimSpace(e.ID)] = QueueEntry{URI: strings.TrimSpace(e.URI), Meta: ParseDIDL(e.Metadata)}
	}
	return out, nil
}

// PlaylistState is what the endless radio needs to know about the play
// queue (OpenHome Playlist service).
// PlaylistState : ce que la radio infinie doit savoir de la file d'attente
// (service OpenHome Playlist).
type PlaylistState struct {
	IDs     []uint32 // queue order / ordre de la file
	Current uint32   // track being played (0: none) / morceau en cours (0 : aucun)
	Playing bool     // Playing or Buffering
	Repeat  bool
	Shuffle bool
}

// PlaylistState reads the queue IDs, the current track and the play modes.
// PlaylistState lit les identifiants de la file, le morceau en cours et les
// modes de lecture.
func (r *Renderer) PlaylistState() (PlaylistState, error) {
	var st PlaylistState
	if r.PlaylistURL == "" {
		return st, errNoPlayQueue
	}
	m, err := soapCall(r.PlaylistURL, r.PlaylistType, "IdArray")
	if err != nil {
		return st, err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(m["Array"]))
	if err != nil {
		return st, fmt.Errorf("unreadable IdArray: %v", err)
	}
	for i := 0; i+4 <= len(raw); i += 4 {
		st.IDs = append(st.IDs, binary.BigEndian.Uint32(raw[i:]))
	}
	if m, err = soapCall(r.PlaylistURL, r.PlaylistType, "Id"); err != nil {
		return st, err
	}
	cur, _ := strconv.ParseUint(m["Value"], 10, 32)
	st.Current = uint32(cur)
	if m, err = soapCall(r.PlaylistURL, r.PlaylistType, "TransportState"); err != nil {
		return st, err
	}
	st.Playing = m["Value"] == "Playing" || m["Value"] == "Buffering"
	if m, err = soapCall(r.PlaylistURL, r.PlaylistType, "Repeat"); err != nil {
		return st, err
	}
	st.Repeat = m["Value"] == "true" || m["Value"] == "1"
	if m, err = soapCall(r.PlaylistURL, r.PlaylistType, "Shuffle"); err != nil {
		return st, err
	}
	st.Shuffle = m["Value"] == "true" || m["Value"] == "1"
	return st, nil
}

// Entries reads the URI and metadata of a few queue tracks, in the order of ids.
// Entries lit l'URI et les métadonnées de quelques morceaux de la file, dans
// l'ordre de ids.
func (r *Renderer) Entries(ids []uint32) ([]QueueEntry, error) {
	list := make([]string, len(ids))
	for i, id := range ids {
		list[i] = strconv.FormatUint(uint64(id), 10)
	}
	res, err := soapCall(r.PlaylistURL, r.PlaylistType, "ReadList", "IdList", strings.Join(list, " "))
	if err != nil {
		return nil, err
	}
	entries, err := parseTrackList(res["TrackList"])
	if err != nil {
		return nil, err
	}
	var out []QueueEntry
	for _, id := range list {
		if e, ok := entries[id]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}

// Insert adds a track after afterID (0: at the start) and returns its ID.
// Insert ajoute un morceau après afterID (0 : au début) et renvoie son identifiant.
func (r *Renderer) Insert(afterID uint32, uri, didl string) (uint32, error) {
	if r.PlaylistURL == "" {
		return 0, errNoPlayQueue
	}
	m, err := soapCall(r.PlaylistURL, r.PlaylistType, "Insert",
		"AfterId", strconv.FormatUint(uint64(afterID), 10), "Uri", uri, "Metadata", didl)
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseUint(m["NewId"], 10, 32)
	if err != nil {
		return 0, errors.New("Insert: no new ID")
	}
	return uint32(id), nil
}

// TimeState matches the OpenHome Time action.
// TimeState correspond à l'action Time de OpenHome.
type TimeState struct {
	TrackCount int
	Duration   int
	Seconds    int
}

// Time returns the track counter, duration and position of the player.
// Time renvoie le compteur de pistes, la durée et la position du lecteur.
func (r *Renderer) Time() (TimeState, error) {
	m, err := soapCall(r.TimeURL, r.TimeType, "Time")
	if err != nil {
		return TimeState{}, err
	}
	tc, _ := strconv.Atoi(m["TrackCount"])
	d, _ := strconv.Atoi(m["Duration"])
	s, _ := strconv.Atoi(m["Seconds"])
	return TimeState{TrackCount: tc, Duration: d, Seconds: s}, nil
}

// Track returns the URI and DIDL-Lite metadata of the current track.
// Track renvoie l'URI et les métadonnées DIDL-Lite de la piste en cours.
func (r *Renderer) Track() (string, string, error) {
	m, err := soapCall(r.InfoURL, r.InfoType, "Track")
	if err != nil {
		return "", "", err
	}
	return m["Uri"], m["Metadata"], nil
}

// SourceType returns the type of the active source (Playlist, Radio, Spotify…).
// It returns "" when the Product service is not available.
// SourceType renvoie le type de la source active (Playlist, Radio, Spotify…).
// Renvoie "" si le service Product n'est pas disponible.
func (r *Renderer) SourceType() string {
	if r.ProductURL == "" {
		return ""
	}
	m, err := soapCall(r.ProductURL, r.ProdType, "SourceIndex")
	if err != nil {
		return ""
	}
	src, err := soapCall(r.ProductURL, r.ProdType, "Source", "Index", m["Value"])
	if err != nil {
		return ""
	}
	return src["Type"]
}

// Room returns the room name configured in the player.
// Room renvoie le nom de pièce configuré dans le lecteur.
func (r *Renderer) Room() string {
	if r.ProductURL == "" {
		return ""
	}
	m, err := soapCall(r.ProductURL, r.ProdType, "Product")
	if err != nil {
		return ""
	}
	return m["Room"]
}

// TrackMeta holds the information useful for Last.fm.
// TrackMeta regroupe les informations utiles pour Last.fm.
type TrackMeta struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	TrackNumber string
}

// ParseDIDL extracts title / artist / album from a DIDL-Lite fragment.
// The artist is the performer, otherwise the creator, otherwise the album artist.
// ParseDIDL extrait titre / artiste / album d'un fragment DIDL-Lite.
// L'artiste est l'interprète, sinon le créateur, sinon l'artiste de l'album.
func ParseDIDL(didl string) TrackMeta {
	var m TrackMeta
	if strings.TrimSpace(didl) == "" {
		return m
	}
	dec := xml.NewDecoder(strings.NewReader(didl))
	dec.Strict = false
	var cur, role string
	var text strings.Builder
	var performer, creator string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			cur = t.Name.Local
			role = ""
			for _, a := range t.Attr {
				if a.Name.Local == "role" {
					role = a.Value
				}
			}
			text.Reset()
		case xml.CharData:
			if cur != "" {
				text.Write(t)
			}
		case xml.EndElement:
			v := strings.TrimSpace(text.String())
			switch t.Name.Local {
			case "title":
				if m.Title == "" {
					m.Title = v
				}
			case "album":
				if m.Album == "" {
					m.Album = v
				}
			case "originalTrackNumber":
				m.TrackNumber = v
			case "creator":
				if creator == "" {
					creator = v
				}
			case "artist":
				switch strings.ToLower(role) {
				case "albumartist":
					if m.AlbumArtist == "" {
						m.AlbumArtist = v
					}
				case "", "performer", "artist":
					if performer == "" {
						performer = v
					}
				}
			}
			cur = ""
		}
	}
	m.Artist = performer
	if m.Artist == "" {
		m.Artist = creator
	}
	if m.Artist == "" {
		m.Artist = m.AlbumArtist
	}
	// Some media servers title tracks "Artist - Title": strip the prefix
	// for Last.fm.
	// Certains serveurs de médias titrent les pistes « Artiste - Titre » :
	// on retire le préfixe pour Last.fm.
	if m.Artist != "" {
		if rest, ok := strings.CutPrefix(m.Title, m.Artist+" - "); ok && rest != "" {
			m.Title = rest
		}
	}
	return m
}
