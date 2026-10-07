package main

// Serveur de médias UPnP « Musique NAS » vu par les lecteurs OpenHome :
// description de l'appareil, icônes, services ContentDirectory et
// ConnectionManager (SOAP) et abonnements aux événements (GENA).
//
// Pourquoi un serveur à nous : l'application de contrôle n'affiche que l'arborescence de
// dossiers envoyée par le serveur. Pour obtenir l'accueil voulu par Bertrand
// (genres favoris, plus écoutés Last.fm, derniers ajouts, photos d'artistes),
// il faut construire cette arborescence nous-mêmes.

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed icons/*
var iconFS embed.FS

const (
	cdsType = "urn:schemas-upnp-org:service:ContentDirectory:1"
	cmType  = "urn:schemas-upnp-org:service:ConnectionManager:1"
	devType = "urn:schemas-upnp-org:device:MediaServer:1"
)

// MediaServer regroupe l'état du serveur UPnP.
type MediaServer struct {
	Name     string
	UDN      string // « uuid:… »
	Version  string
	lib      *LibraryService
	updateID atomic.Uint32
	subs     *eventSubs

	// 0.0.4 : interface Kazoo (mediaendpoint.go). Endpoint nil ou
	// Classic() vrai : description UPnP simple, comme en 0.0.3.
	Endpoint *MediaEndpoint
	Classic  func() bool
	PagePort int // port de la page de réglages (lien depuis « / »)
}

func NewMediaServer(name, udn, version string, lib *LibraryService) *MediaServer {
	ms := &MediaServer{Name: name, UDN: udn, Version: version, lib: lib, subs: newEventSubs()}
	ms.updateID.Store(uint32(time.Now().Unix() % 100000))
	return ms
}

// Routes ajoute les adresses UPnP et média au routeur HTTP.
func (ms *MediaServer) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /description.xml", ms.handleDescription)
	mux.HandleFunc("GET /upnp/cds.xml", func(w http.ResponseWriter, r *http.Request) { writeXML(w, cdsSCPD) })
	mux.HandleFunc("GET /upnp/cm.xml", func(w http.ResponseWriter, r *http.Request) { writeXML(w, cmSCPD) })
	mux.HandleFunc("POST /upnp/cds/control", ms.handleCDS)
	mux.HandleFunc("POST /upnp/cm/control", ms.handleCM)
	mux.HandleFunc("/upnp/cds/event", ms.subs.handler("cds", ms.cdsEventProps))
	mux.HandleFunc("/upnp/cm/event", ms.subs.handler("cm", ms.cmEventProps))
	mux.HandleFunc("GET /icons/{name}", ms.handleIcon)
	mux.HandleFunc("GET /{$}", ms.handlePresentation)
	ms.lib.Routes(mux)
	if ms.Endpoint != nil {
		ms.Endpoint.Routes(mux)
	}
}

func (ms *MediaServer) kazoo() bool {
	return ms.Endpoint != nil && (ms.Classic == nil || !ms.Classic())
}

// handlePresentation : la presentationURL (exigée par l'application de contrôle avec
// X_PATH) mène, dans un navigateur, à la page de réglages.
func (ms *MediaServer) handlePresentation(w http.ResponseWriter, r *http.Request) {
	if ms.PagePort == 0 {
		http.NotFound(w, r)
		return
	}
	host, _, err := net.SplitHostPort(presentationHost(r))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "http://"+net.JoinHostPort(host, strconv.Itoa(ms.PagePort))+"/", http.StatusFound)
}

// Changed : l'index a changé ; prévient les lecteurs abonnés.
func (ms *MediaServer) Changed() {
	ms.updateID.Add(1)
	ms.subs.notify("cds", ms.cdsEventProps())
	if ms.Endpoint != nil {
		ms.Endpoint.Changed()
	}
}

func (ms *MediaServer) cdsEventProps() map[string]string {
	return map[string]string{"SystemUpdateID": strconv.FormatUint(uint64(ms.updateID.Load()), 10),
		"ContainerUpdateIDs": "", "TransferIDs": ""}
}

func (ms *MediaServer) cmEventProps() map[string]string {
	return map[string]string{"SourceProtocolInfo": sourceProtocolInfo, "SinkProtocolInfo": "", "CurrentConnectionIDs": "0"}
}

func writeXML(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	io.WriteString(w, s)
}

func xmlEsc(s string) string {
	var b bytes.Buffer
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (ms *MediaServer) handleDescription(w http.ResponseWriter, r *http.Request) {
	icons := ""
	for _, s := range []int{120, 48, 32} {
		for _, f := range []struct{ ext, mime string }{{"png", "image/png"}, {"jpg", "image/jpeg"}} {
			icons += fmt.Sprintf("<icon><mimetype>%s</mimetype><width>%d</width><height>%d</height><depth>24</depth><url>/icons/icon-%d.%s</url></icon>",
				f.mime, s, s, s, f.ext)
		}
	}
	// 0.0.4 : X_PATH et presentationURL font de ce serveur un serveur
	// « Kazoo » pour l'application de contrôle (accueil en rangées, recherche). L'appli
	// exige une presentationURL absolue avec une adresse IPv4.
	kazoo := ""
	if ms.kazoo() {
		kazoo = "\n<openhome:X_PATH xmlns:openhome=\"http://www.openhome.org\">/ps</openhome:X_PATH>" +
			"\n<presentationURL>http://" + xmlEsc(presentationHost(r)) + "/</presentationURL>"
	}
	writeXML(w, `<?xml version="1.0" encoding="utf-8"?>
<root xmlns="urn:schemas-upnp-org:device-1-0" xmlns:dlna="urn:schemas-dlna-org:device-1-0">
<specVersion><major>1</major><minor>0</minor></specVersion>
<device>
<deviceType>`+devType+`</deviceType>
<friendlyName>`+xmlEsc(ms.Name)+`</friendlyName>
<manufacturer>Serveur Musique</manufacturer>
<modelDescription>Serveur de musique pour lecteurs OpenHome</modelDescription>
<modelName>Musique NAS</modelName>
<modelNumber>`+xmlEsc(ms.Version)+`</modelNumber>
<serialNumber>1</serialNumber>
<UDN>`+xmlEsc(ms.UDN)+`</UDN>
<dlna:X_DLNADOC>DMS-1.50</dlna:X_DLNADOC>`+kazoo+`
<iconList>`+icons+`</iconList>
<serviceList>
<service><serviceType>`+cdsType+`</serviceType><serviceId>urn:upnp-org:serviceId:ContentDirectory</serviceId><SCPDURL>/upnp/cds.xml</SCPDURL><controlURL>/upnp/cds/control</controlURL><eventSubURL>/upnp/cds/event</eventSubURL></service>
<service><serviceType>`+cmType+`</serviceType><serviceId>urn:upnp-org:serviceId:ConnectionManager</serviceId><SCPDURL>/upnp/cm.xml</SCPDURL><controlURL>/upnp/cm/control</controlURL><eventSubURL>/upnp/cm/event</eventSubURL></service>
</serviceList>
</device>
</root>`)
}

func (ms *MediaServer) handleIcon(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	b, err := iconFS.ReadFile("icons/" + name)
	if err != nil || strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(name, ".png") {
		w.Header().Set("Content-Type", "image/png")
	} else {
		w.Header().Set("Content-Type", "image/jpeg")
	}
	w.Header().Set("Cache-Control", "max-age=86400")
	w.Write(b)
}

// ---------------------------------------------------------------------------
// SOAP

type soapArgs map[string]string

// parseSOAP lit le nom de l'action (en-tête SOAPACTION) et ses arguments.
func parseSOAP(r *http.Request) (string, soapArgs, error) {
	sa := strings.Trim(r.Header.Get("SOAPACTION"), `" `)
	action := sa
	if i := strings.LastIndexByte(sa, '#'); i >= 0 {
		action = sa[i+1:]
	}
	args := soapArgs{}
	dec := xml.NewDecoder(io.LimitReader(r.Body, 256*1024))
	depth := 0
	var cur string
	var text strings.Builder
	inAction := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return action, args, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if !inAction && depth == 3 { // Envelope > Body > Action
				inAction = true
				if action == "" {
					action = t.Name.Local
				}
			} else if inAction && depth == 4 {
				cur = t.Name.Local
				text.Reset()
			}
		case xml.CharData:
			if cur != "" {
				text.Write(t)
			}
		case xml.EndElement:
			if inAction && depth == 4 && cur != "" {
				args[cur] = text.String()
				cur = ""
			}
			depth--
		}
	}
	return action, args, nil
}

func soapReply(w http.ResponseWriter, service, action string, out [][2]string) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	b.WriteString(`<u:` + action + `Response xmlns:u="` + service + `">`)
	for _, kv := range out {
		b.WriteString("<" + kv[0] + ">" + xmlEsc(kv[1]) + "</" + kv[0] + ">")
	}
	b.WriteString(`</u:` + action + `Response></s:Body></s:Envelope>`)
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.Header().Set("EXT", "")
	io.WriteString(w, b.String())
}

func soapFault(w http.ResponseWriter, code int, desc string) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.WriteHeader(http.StatusInternalServerError)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><s:Fault><faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring><detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0"><errorCode>%d</errorCode><errorDescription>%s</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`, code, xmlEsc(desc))
}

const sourceProtocolInfo = "http-get:*:audio/flac:*,http-get:*:audio/x-flac:*,http-get:*:audio/mpeg:*,http-get:*:audio/mp4:*,http-get:*:audio/x-m4a:*,http-get:*:audio/wav:*,http-get:*:audio/x-aiff:*,http-get:*:audio/x-dsf:*,http-get:*:audio/x-dff:*,http-get:*:audio/ogg:*,http-get:*:image/jpeg:*,http-get:*:image/png:*"

func (ms *MediaServer) handleCM(w http.ResponseWriter, r *http.Request) {
	action, args, err := parseSOAP(r)
	if err != nil {
		soapFault(w, 402, "Invalid Args")
		return
	}
	switch action {
	case "GetProtocolInfo":
		soapReply(w, cmType, action, [][2]string{{"Source", sourceProtocolInfo}, {"Sink", ""}})
	case "GetCurrentConnectionIDs":
		soapReply(w, cmType, action, [][2]string{{"ConnectionIDs", "0"}})
	case "GetCurrentConnectionInfo":
		if args["ConnectionID"] != "0" {
			soapFault(w, 706, "Invalid connection reference")
			return
		}
		soapReply(w, cmType, action, [][2]string{{"RcsID", "-1"}, {"AVTransportID", "-1"}, {"ProtocolInfo", ""},
			{"PeerConnectionManager", ""}, {"PeerConnectionID", "-1"}, {"Direction", "Output"}, {"Status", "OK"}})
	default:
		soapFault(w, 401, "Invalid Action")
	}
}

// ---------------------------------------------------------------------------
// Événements GENA (abonnements des lecteurs aux changements de la bibliothèque)

type subscription struct {
	sid      string
	callback []string
	expires  time.Time
	seq      uint32
}

type eventSubs struct {
	mu   sync.Mutex
	subs map[string]map[string]*subscription // service -> SID -> abonnement
}

func newEventSubs() *eventSubs {
	return &eventSubs{subs: map[string]map[string]*subscription{"cds": {}, "cm": {}}}
}

func newSID() string {
	b := make([]byte, 16)
	rand.Read(b)
	h := hex.EncodeToString(b)
	return "uuid:" + h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func parseTimeout(h string) time.Duration {
	if strings.HasPrefix(strings.ToLower(h), "second-") {
		if n, err := strconv.Atoi(h[7:]); err == nil && n > 0 {
			if n > 3600 {
				n = 3600
			}
			return time.Duration(n) * time.Second
		}
	}
	return 1800 * time.Second
}

// parseCallbacks lit « <http://a/>< http://b/> » ; seules les URL http vers
// une adresse locale sont acceptées (évite d'utiliser le serveur comme relais).
func parseCallbacks(h string) []string {
	var out []string
	for _, part := range strings.Split(h, "<") {
		part = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(part), ">"))
		if !strings.HasPrefix(part, "http://") {
			continue
		}
		host := strings.TrimPrefix(part, "http://")
		if i := strings.IndexAny(host, "/"); i >= 0 {
			host = host[:i]
		}
		if !isLocal(host) {
			continue
		}
		out = append(out, part)
	}
	return out
}

func (es *eventSubs) handler(service string, props func() map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		es.mu.Lock()
		defer es.mu.Unlock()
		now := time.Now()
		for sid, s := range es.subs[service] {
			if now.After(s.expires) {
				delete(es.subs[service], sid)
			}
		}
		switch r.Method {
		case "SUBSCRIBE":
			to := parseTimeout(r.Header.Get("TIMEOUT"))
			if sid := r.Header.Get("SID"); sid != "" {
				s := es.subs[service][sid]
				if s == nil {
					http.Error(w, "", http.StatusPreconditionFailed)
					return
				}
				s.expires = now.Add(to)
				w.Header().Set("SID", sid)
				w.Header().Set("TIMEOUT", "Second-"+strconv.Itoa(int(to.Seconds())))
				return
			}
			cbs := parseCallbacks(r.Header.Get("CALLBACK"))
			if len(cbs) == 0 || r.Header.Get("NT") != "upnp:event" {
				http.Error(w, "", http.StatusPreconditionFailed)
				return
			}
			if len(es.subs[service]) >= 64 {
				http.Error(w, "", http.StatusServiceUnavailable)
				return
			}
			s := &subscription{sid: newSID(), callback: cbs, expires: now.Add(to)}
			es.subs[service][s.sid] = s
			w.Header().Set("SID", s.sid)
			w.Header().Set("TIMEOUT", "Second-"+strconv.Itoa(int(to.Seconds())))
			w.Header().Set("Content-Length", "0")
			// Événement initial, envoyé après la réponse (spécification UPnP).
			p := props()
			go func() {
				time.Sleep(200 * time.Millisecond)
				sendEvent(s, 0, p)
			}()
		case "UNSUBSCRIBE":
			delete(es.subs[service], r.Header.Get("SID"))
		default:
			http.Error(w, "", http.StatusMethodNotAllowed)
		}
	}
}

func (es *eventSubs) notify(service string, props map[string]string) {
	es.mu.Lock()
	var list []*subscription
	var seqs []uint32
	for _, s := range es.subs[service] {
		s.seq++
		list = append(list, s)
		seqs = append(seqs, s.seq)
	}
	es.mu.Unlock()
	for i, s := range list {
		go sendEvent(s, seqs[i], props)
	}
}

var eventClient = &http.Client{Timeout: 5 * time.Second}

func sendEvent(s *subscription, seq uint32, props map[string]string) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?><e:propertyset xmlns:e="urn:schemas-upnp-org:event-1-0">`)
	for k, v := range props {
		b.WriteString("<e:property><" + k + ">" + xmlEsc(v) + "</" + k + "></e:property>")
	}
	b.WriteString("</e:propertyset>")
	for _, cb := range s.callback {
		req, err := http.NewRequest("NOTIFY", cb, strings.NewReader(b.String()))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
		req.Header.Set("NT", "upnp:event")
		req.Header.Set("NTS", "upnp:propchange")
		req.Header.Set("SID", s.sid)
		req.Header.Set("SEQ", strconv.FormatUint(uint64(seq), 10))
		resp, err := eventClient.Do(req)
		if err == nil {
			resp.Body.Close()
			return
		}
	}
}

// localIPv4 : adresse IPv4 de l'interface (pour LOCATION et les URL).
func localIPv4(iface *net.Interface) net.IP {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			if ip := n.IP.To4(); ip != nil && !ip.IsLoopback() {
				return ip
			}
		}
	}
	return nil
}

func logf(format string, args ...any) { log.Printf(format, args...) }

// lanOnly : le serveur de musique ne répond qu'au réseau local. Pas de
// contrôle d'origine ici (contrairement à la page 8790) : les lecteurs et
// l'application de contrôle envoient des requêtes SOAP sans en-tête Origin, et ce
// serveur ne modifie rien.
func lanOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocal(r.RemoteAddr) {
			http.Error(w, "accès réservé au réseau local", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

const cdsSCPD = `<?xml version="1.0" encoding="utf-8"?>
<scpd xmlns="urn:schemas-upnp-org:service-1-0"><specVersion><major>1</major><minor>0</minor></specVersion>
<actionList>
<action><name>GetSearchCapabilities</name><argumentList><argument><name>SearchCaps</name><direction>out</direction><relatedStateVariable>SearchCapabilities</relatedStateVariable></argument></argumentList></action>
<action><name>GetSortCapabilities</name><argumentList><argument><name>SortCaps</name><direction>out</direction><relatedStateVariable>SortCapabilities</relatedStateVariable></argument></argumentList></action>
<action><name>GetSystemUpdateID</name><argumentList><argument><name>Id</name><direction>out</direction><relatedStateVariable>SystemUpdateID</relatedStateVariable></argument></argumentList></action>
<action><name>Browse</name><argumentList>
<argument><name>ObjectID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_ObjectID</relatedStateVariable></argument>
<argument><name>BrowseFlag</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_BrowseFlag</relatedStateVariable></argument>
<argument><name>Filter</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Filter</relatedStateVariable></argument>
<argument><name>StartingIndex</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Index</relatedStateVariable></argument>
<argument><name>RequestedCount</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
<argument><name>SortCriteria</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_SortCriteria</relatedStateVariable></argument>
<argument><name>Result</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Result</relatedStateVariable></argument>
<argument><name>NumberReturned</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
<argument><name>TotalMatches</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
<argument><name>UpdateID</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_UpdateID</relatedStateVariable></argument>
</argumentList></action>
<action><name>Search</name><argumentList>
<argument><name>ContainerID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_ObjectID</relatedStateVariable></argument>
<argument><name>SearchCriteria</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_SearchCriteria</relatedStateVariable></argument>
<argument><name>Filter</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Filter</relatedStateVariable></argument>
<argument><name>StartingIndex</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Index</relatedStateVariable></argument>
<argument><name>RequestedCount</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
<argument><name>SortCriteria</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_SortCriteria</relatedStateVariable></argument>
<argument><name>Result</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Result</relatedStateVariable></argument>
<argument><name>NumberReturned</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
<argument><name>TotalMatches</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
<argument><name>UpdateID</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_UpdateID</relatedStateVariable></argument>
</argumentList></action>
</actionList>
<serviceStateTable>
<stateVariable sendEvents="no"><name>SearchCapabilities</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>SortCapabilities</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="yes"><name>SystemUpdateID</name><dataType>ui4</dataType></stateVariable>
<stateVariable sendEvents="yes"><name>ContainerUpdateIDs</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="yes"><name>TransferIDs</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_ObjectID</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_Result</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_SearchCriteria</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_BrowseFlag</name><dataType>string</dataType><allowedValueList><allowedValue>BrowseMetadata</allowedValue><allowedValue>BrowseDirectChildren</allowedValue></allowedValueList></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_Filter</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_SortCriteria</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_Index</name><dataType>ui4</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_Count</name><dataType>ui4</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_UpdateID</name><dataType>ui4</dataType></stateVariable>
</serviceStateTable></scpd>`

const cmSCPD = `<?xml version="1.0" encoding="utf-8"?>
<scpd xmlns="urn:schemas-upnp-org:service-1-0"><specVersion><major>1</major><minor>0</minor></specVersion>
<actionList>
<action><name>GetProtocolInfo</name><argumentList><argument><name>Source</name><direction>out</direction><relatedStateVariable>SourceProtocolInfo</relatedStateVariable></argument><argument><name>Sink</name><direction>out</direction><relatedStateVariable>SinkProtocolInfo</relatedStateVariable></argument></argumentList></action>
<action><name>GetCurrentConnectionIDs</name><argumentList><argument><name>ConnectionIDs</name><direction>out</direction><relatedStateVariable>CurrentConnectionIDs</relatedStateVariable></argument></argumentList></action>
<action><name>GetCurrentConnectionInfo</name><argumentList>
<argument><name>ConnectionID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_ConnectionID</relatedStateVariable></argument>
<argument><name>RcsID</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_RcsID</relatedStateVariable></argument>
<argument><name>AVTransportID</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_AVTransportID</relatedStateVariable></argument>
<argument><name>ProtocolInfo</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_ProtocolInfo</relatedStateVariable></argument>
<argument><name>PeerConnectionManager</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_ConnectionManager</relatedStateVariable></argument>
<argument><name>PeerConnectionID</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_ConnectionID</relatedStateVariable></argument>
<argument><name>Direction</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Direction</relatedStateVariable></argument>
<argument><name>Status</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_ConnectionStatus</relatedStateVariable></argument>
</argumentList></action>
</actionList>
<serviceStateTable>
<stateVariable sendEvents="yes"><name>SourceProtocolInfo</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="yes"><name>SinkProtocolInfo</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="yes"><name>CurrentConnectionIDs</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_ConnectionStatus</name><dataType>string</dataType><allowedValueList><allowedValue>OK</allowedValue><allowedValue>ContentFormatMismatch</allowedValue><allowedValue>InsufficientBandwidth</allowedValue><allowedValue>UnreliableChannel</allowedValue><allowedValue>Unknown</allowedValue></allowedValueList></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_ConnectionManager</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_Direction</name><dataType>string</dataType><allowedValueList><allowedValue>Input</allowedValue><allowedValue>Output</allowedValue></allowedValueList></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_ProtocolInfo</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_ConnectionID</name><dataType>i4</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_AVTransportID</name><dataType>i4</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_RcsID</name><dataType>i4</dataType></stateVariable>
</serviceStateTable></scpd>`
