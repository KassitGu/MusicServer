package main

// SSDP announcement of the "Musique NAS" server: replies to player searches
// (M-SEARCH) and periodic announcements (NOTIFY alive / byebye), on the one
// network interface that leads to the players.
// Annonce SSDP du serveur « Musique NAS » : réponses aux recherches
// (M-SEARCH) des lecteurs et annonces périodiques (NOTIFY alive / byebye),
// sur la seule carte réseau qui mène aux lecteurs.

import (
	"bufio"
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const ssdpMaxAge = 1800 // seconds / secondes

// SSDPAnnouncer advertises the media server on the local network.
// SSDPAnnouncer annonce le serveur de médias sur le réseau local.
type SSDPAnnouncer struct {
	UDN, Version string
	Port         int
	IfaceName    string // empty: default route interface / vide : carte de la route par défaut

	mu    sync.Mutex
	where string // announced interface and address (shown on the settings page) / carte et adresse annoncées (affichées sur la page de réglages)
}

// Where returns "eth1 (192.168.68.54:49494)", or the reason why nothing is announced.
// Where renvoie « eth1 (192.168.68.54:49494) », ou la raison de l'absence d'annonce.
func (s *SSDPAnnouncer) Where() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.where
}

func (s *SSDPAnnouncer) setWhere(v string) {
	s.mu.Lock()
	s.where = v
	s.mu.Unlock()
}

// defaultRouteIface reads /proc/net/route (same rule as musique.sh).
// defaultRouteIface lit /proc/net/route (même règle que musique.sh).
func defaultRouteIface() string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan()
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) >= 8 && fl[1] == "00000000" && fl[7] == "00000000" {
			return fl[0]
		}
	}
	return ""
}

// pickIface chooses the interface: the given name, otherwise the default
// route, otherwise the first active interface with an IPv4 address.
// pickIface choisit la carte : nom donné, sinon route par défaut, sinon la
// première carte active avec une adresse IPv4.
func pickIface(name string) (*net.Interface, net.IP, error) {
	if name == "" {
		name = defaultRouteIface()
	}
	if name != "" {
		ifc, err := net.InterfaceByName(name)
		if err == nil {
			if ip := localIPv4(ifc); ip != nil {
				return ifc, ip, nil
			}
		}
	}
	ifs, _ := net.Interfaces()
	for i := range ifs {
		ifc := &ifs[i]
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagMulticast == 0 {
			continue
		}
		if ip := localIPv4(ifc); ip != nil {
			return ifc, ip, nil
		}
	}
	return nil, nil, fmt.Errorf("no usable network interface")
}

func (s *SSDPAnnouncer) types() []string {
	return []string{"upnp:rootdevice", s.UDN, devType, cdsType, cmType}
}

func (s *SSDPAnnouncer) usn(nt string) string {
	if nt == s.UDN {
		return s.UDN
	}
	return s.UDN + "::" + nt
}

func (s *SSDPAnnouncer) server() string {
	return "Linux/5 UPnP/1.0 MusiqueNAS/" + s.Version
}

// setMulticastIf forces the outgoing interface of multicast announcements
// (TTL 2: the announcements stay on the local network).
// setMulticastIf force la carte de sortie des annonces multicast
// (TTL 2 : les annonces restent sur le réseau local).
func setMulticastIf(c *net.UDPConn, ip net.IP) {
	rc, err := c.SyscallConn()
	if err != nil {
		return
	}
	var a [4]byte
	copy(a[:], ip.To4())
	rc.Control(func(fd uintptr) {
		syscall.SetsockoptInet4Addr(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_IF, a)
		syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_TTL, 2)
	})
}

// Run announces the server until the context is cancelled.
// Run annonce le serveur jusqu'à l'annulation du contexte.
func (s *SSDPAnnouncer) Run(ctx context.Context) {
	for ctx.Err() == nil {
		ifc, ip, err := pickIface(s.IfaceName)
		if err != nil {
			logf("SSDP: %v", err)
			s.setWhere("aucune annonce : " + err.Error())
			sleepCtx(ctx, 30*time.Second)
			continue
		}
		logf("SSDP: announcing on %s (%s:%d)", ifc.Name, ip, s.Port)
		s.setWhere(fmt.Sprintf("%s (%s:%d)", ifc.Name, ip, s.Port))
		s.runOn(ctx, ifc, ip)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// runOn announces on one interface until the context is cancelled or the
// interface address changes.
// runOn annonce sur une carte jusqu'à l'annulation du contexte ou jusqu'au
// changement d'adresse de la carte.
func (s *SSDPAnnouncer) runOn(ctx context.Context, ifc *net.Interface, ip net.IP) {
	group := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}
	lc, err := net.ListenMulticastUDP("udp4", ifc, group)
	if err != nil {
		// Fallback: join the group without forcing the interface (the kernel
		// picks the one on the route to the group). Some environments refuse
		// the option that sets the receiving interface.
		// Repli : abonnement au groupe sans imposer la carte (le noyau
		// choisit celle de la route vers le groupe). Certains environnements
		// refusent l'option qui fixe la carte de réception.
		logf("SSDP: cannot listen on %s (%v), trying without a fixed interface", ifc.Name, err)
		lc, err = net.ListenMulticastUDP("udp4", nil, group)
	}
	if err != nil {
		// Without listening, player searches get no reply, but the periodic
		// announcements are enough for the control app to see the server:
		// carry on in "announcements only" mode.
		// Sans écoute, on ne répond pas aux recherches des lecteurs, mais
		// les annonces périodiques suffisent à l'application de contrôle pour
		// voir le serveur : on continue en mode « annonces seules ».
		logf("SSDP: cannot listen (%v): announcements only", err)
		s.setWhere(fmt.Sprintf("%s (%s:%d), annonces seules : %v", ifc.Name, ip, s.Port, err))
		lc = nil
	}
	out, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip, Port: 0})
	if err != nil {
		if lc != nil {
			lc.Close()
		}
		logf("SSDP: cannot send: %v", err)
		s.setWhere("aucune annonce : " + err.Error())
		sleepCtx(ctx, 30*time.Second)
		return
	}
	setMulticastIf(out, ip)
	location := fmt.Sprintf("http://%s:%d/description.xml", ip, s.Port)
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-sctx.Done()
		if lc != nil {
			lc.Close()
		}
	}()

	notify := func(nts string) {
		for _, nt := range s.types() {
			msg := "NOTIFY * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\n" +
				"CACHE-CONTROL: max-age=" + strconv.Itoa(ssdpMaxAge) + "\r\n" +
				"LOCATION: " + location + "\r\n" +
				"NT: " + nt + "\r\nNTS: " + nts + "\r\n" +
				"SERVER: " + s.server() + "\r\n" +
				"USN: " + s.usn(nt) + "\r\n\r\n"
			if nts == "ssdp:byebye" {
				msg = "NOTIFY * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nNT: " + nt + "\r\nNTS: ssdp:byebye\r\nUSN: " + s.usn(nt) + "\r\n\r\n"
			}
			out.WriteToUDP([]byte(msg), group)
		}
	}

	// Announcements: 3 times at start-up (UDP may lose packets), then every
	// 5 minutes; the address is also checked for changes.
	// Annonces : 3 fois au démarrage (UDP peut perdre des paquets), puis
	// toutes les 5 minutes ; on vérifie aussi que l'adresse n'a pas changé.
	go func() {
		for i := 0; i < 3 && sctx.Err() == nil; i++ {
			notify("ssdp:alive")
			sleepCtx(sctx, 500*time.Millisecond)
		}
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-sctx.Done():
				return
			case <-t.C:
				if _, nip, err := pickIface(s.IfaceName); err != nil || !nip.Equal(ip) {
					logf("SSDP: address changed, announcing again")
					cancel()
					return
				}
				notify("ssdp:alive")
			}
		}
	}()

	if lc == nil {
		// Try listening again in 5 minutes.
		// Nouvel essai d'écoute dans 5 minutes.
		select {
		case <-sctx.Done():
		case <-time.After(5 * time.Minute):
		}
	}
	buf := make([]byte, 2048)
	for lc != nil {
		n, from, err := lc.ReadFromUDP(buf)
		if err != nil {
			break
		}
		req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(string(buf[:n]))))
		if err != nil || req.Method != "M-SEARCH" || !strings.Contains(req.Header.Get("MAN"), "ssdp:discover") {
			continue
		}
		st := req.Header.Get("ST")
		var sts []string
		switch {
		case st == "ssdp:all":
			sts = s.types()
		default:
			for _, t := range s.types() {
				if t == st {
					sts = []string{t}
				}
			}
		}
		if len(sts) == 0 {
			continue
		}
		// Reply after a random delay of up to half of MX (MX limited to 1-3 s),
		// within the window SSDP allows, so that devices do not all answer at once.
		// Réponse après un délai aléatoire d'au plus la moitié de MX (MX limité
		// à 1-3 s), dans la fenêtre permise par SSDP, pour que les appareils ne
		// répondent pas tous ensemble.
		mx, _ := strconv.Atoi(req.Header.Get("MX"))
		if mx < 1 {
			mx = 1
		} else if mx > 3 {
			mx = 3
		}
		go func(to *net.UDPAddr, sts []string, delay time.Duration) {
			time.Sleep(delay)
			for _, t := range sts {
				msg := "HTTP/1.1 200 OK\r\n" +
					"CACHE-CONTROL: max-age=" + strconv.Itoa(ssdpMaxAge) + "\r\n" +
					"DATE: " + time.Now().UTC().Format(http.TimeFormat) + "\r\n" +
					"EXT:\r\nLOCATION: " + location + "\r\n" +
					"SERVER: " + s.server() + "\r\n" +
					"ST: " + t + "\r\nUSN: " + s.usn(t) + "\r\n\r\n"
				out.WriteToUDP([]byte(msg), to)
			}
		}(from, sts, time.Duration(rand.Int63n(int64(mx)*int64(time.Second)/2)))
	}
	if ctx.Err() != nil {
		notify("ssdp:byebye")
	}
	out.Close()
}
