package main

// Client OpenHome minimal : découverte SSDP des lecteurs (DS/DSM…) et
// appels SOAP aux services Info, Time et Product.
// Les lecteurs OpenHome exposent ces services OpenHome standard ; on lit ce qui
// est réellement joué plutôt que ce que le serveur de médias a envoyé
// (les lecteurs préchargent la piste suivante pour le gapless).

import (
	"bytes"
	"context"
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

// Renderer décrit un lecteur OpenHome et les URL de contrôle utiles.
type Renderer struct {
	Location   string
	UDN        string
	Name       string
	InfoURL    string
	InfoType   string
	TimeURL    string
	TimeType   string
	ProductURL string
	ProdType   string
}

// DiscoverRenderers envoie un M-SEARCH SSDP et renvoie les URL de description
// des appareils qui exposent le service OpenHome Info.
func DiscoverRenderers(ctx context.Context, wait time.Duration) ([]string, error) {
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
		"ST: " + infoServiceType + "\r\n\r\n"
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
	UDN          string       `xml:"UDN"`
	Services     []xmlService `xml:"serviceList>service"`
	Devices      []xmlDevice  `xml:"deviceList>device"`
}

type xmlRoot struct {
	URLBase string    `xml:"URLBase"`
	Device  xmlDevice `xml:"device"`
}

// LoadRenderer lit la description UPnP et repère les services OpenHome.
func LoadRenderer(location string) (*Renderer, error) {
	resp, err := httpClient.Get(location)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("description %s : HTTP %d", location, resp.StatusCode)
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
				}
			case strings.HasPrefix(s.ServiceType, "urn:av-openhome-org:service:Time:"):
				if r.TimeURL == "" {
					r.TimeURL, r.TimeType = ctrl.String(), s.ServiceType
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
		return nil, errors.New("services OpenHome Info/Time absents")
	}
	if r.UDN == "" {
		r.UDN = location
	}
	return r, nil
}

// soapCall exécute une action SOAP et renvoie les arguments de sortie.
func soapCall(ctrlURL, serviceType, action string, args map[string]string) (map[string]string, error) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	fmt.Fprintf(&b, `<u:%s xmlns:u="%s">`, action, serviceType)
	for k, v := range args {
		fmt.Fprintf(&b, "<%s>", k)
		xml.EscapeText(&b, []byte(v))
		fmt.Fprintf(&b, "</%s>", k)
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
		return nil, fmt.Errorf("%s : HTTP %d", action, resp.StatusCode)
	}
	return parseSoapResponse(body, action+"Response")
}

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
		return nil, errors.New("réponse SOAP inattendue")
	}
	return out, nil
}

// TimeState correspond à l'action Time de OpenHome.
type TimeState struct {
	TrackCount int
	Duration   int
	Seconds    int
}

func (r *Renderer) Time() (TimeState, error) {
	m, err := soapCall(r.TimeURL, r.TimeType, "Time", nil)
	if err != nil {
		return TimeState{}, err
	}
	tc, _ := strconv.Atoi(m["TrackCount"])
	d, _ := strconv.Atoi(m["Duration"])
	s, _ := strconv.Atoi(m["Seconds"])
	return TimeState{TrackCount: tc, Duration: d, Seconds: s}, nil
}

// Track renvoie l'URI et les métadonnées DIDL-Lite de la piste en cours.
func (r *Renderer) Track() (string, string, error) {
	m, err := soapCall(r.InfoURL, r.InfoType, "Track", nil)
	if err != nil {
		return "", "", err
	}
	return m["Uri"], m["Metadata"], nil
}

// SourceType renvoie le type de la source active (Playlist, Radio, Spotify…).
// Renvoie "" si le service Product n'est pas disponible.
func (r *Renderer) SourceType() string {
	if r.ProductURL == "" {
		return ""
	}
	m, err := soapCall(r.ProductURL, r.ProdType, "SourceIndex", nil)
	if err != nil {
		return ""
	}
	src, err := soapCall(r.ProductURL, r.ProdType, "Source", map[string]string{"Index": m["Value"]})
	if err != nil {
		return ""
	}
	return src["Type"]
}

// Room renvoie le nom de pièce configuré dans le lecteur.
func (r *Renderer) Room() string {
	if r.ProductURL == "" {
		return ""
	}
	m, err := soapCall(r.ProductURL, r.ProdType, "Product", nil)
	if err != nil {
		return ""
	}
	return m["Room"]
}

// TrackMeta : informations utiles pour Last.fm.
type TrackMeta struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	TrackNumber string
}

// ParseDIDL extrait titre / artiste / album d'un fragment DIDL-Lite.
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
	// Certaines vues de Gerbera (« All - full name ») titrent les pistes
	// « Artiste - Titre » : on retire le préfixe pour Last.fm.
	if m.Artist != "" {
		if rest, ok := strings.CutPrefix(m.Title, m.Artist+" - "); ok && rest != "" {
			m.Title = rest
		}
	}
	return m
}
