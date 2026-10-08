package main

// "es" event server of the Media Endpoint interface.
//
// Why: the control app reads a server's library list (/ps/me) only after it
// has received the "ps.me" event from this server, and it removes the
// library when it receives no "es.alive" for more than 16 s. It also
// subscribes to every open session (me.<endpoint>.<session>) to know when
// to reload a screen.
//
// Protocol (observed in the control app and in Kazoo Server): raw TCP,
// UTF-8 lines terminated by a bare LF.
//   client -> server: "+id" subscribe, "-id" unsubscribe,
//                     "=id n" publish, empty line = end of session.
//   server -> client: "id n". A subscription immediately receives "id 0".
// "es.alive n" is sent every 5 s with a global counter that must never skip
// a value on a connection (otherwise the app drops everything).
//
// Serveur d'événements « es » de l'interface Media Endpoint.
//
// Pourquoi : l'application de contrôle ne lit la liste des bibliothèques
// d'un serveur (/ps/me) qu'après avoir reçu l'événement « ps.me » sur ce
// serveur, et elle retire la bibliothèque si elle ne reçoit plus
// « es.alive » pendant plus de 16 s. Elle s'y abonne aussi à chaque session
// ouverte (me.<endpoint>.<session>) pour savoir quand relire un écran.
//
// Protocole (relevé dans l'application de contrôle et dans Kazoo Server) :
// TCP brut, lignes UTF-8 terminées par LF seul.
//   client -> serveur : « +id » abonnement, « -id » désabonnement,
//                       « =id n » publication, ligne vide = fin de session.
//   serveur -> client : « id n ». Un abonnement reçoit aussitôt « id 0 ».
// « es.alive n » part toutes les 5 s avec un compteur global qui ne doit
// jamais sauter de valeur pour une connexion (sinon l'application coupe tout).

import (
	"bufio"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	esAliveID      = "es.alive"
	esAlivePeriod  = 5 * time.Second
	esMaxConns     = 200
	esQueue        = 256 // queued lines per connection / lignes en attente par connexion
	esMaxLine      = 4096
	esWriteTimeout = 10 * time.Second
)

type esConn struct {
	c      net.Conn
	out    chan string
	subs   map[string]bool
	ended  bool // empty line received: nothing more is processed / ligne vide reçue : plus rien n'est traité
	closed bool
}

// EventServer is the event server shared by all connections.
// EventServer est le serveur d'événements partagé par toutes les connexions.
type EventServer struct {
	ln net.Listener

	mu       sync.Mutex
	conns    map[*esConn]bool
	subs     map[string]map[*esConn]bool
	counters map[string]uint32
	alive    uint32

	// OnSubscribers is called (outside the lock) when the subscriber count
	// of an id changes: +1 on subscription, -1 on departure. Used for the
	// lifetime of the endpoint sessions.
	// OnSubscribers est appelé (hors verrou) quand le nombre d'abonnés
	// d'un id change : +1 à l'abonnement, -1 au départ. Sert à la durée de
	// vie des sessions de l'endpoint.
	OnSubscribers func(id string, delta int)
}

// NewEventServer listens on the given port, or on a free port if it is
// taken (the app reads the port from /ps/es, so it need not be fixed).
// NewEventServer écoute sur le port donné ; s'il est pris, sur un port
// libre (l'application lit le port dans /ps/es, il n'a pas besoin d'être fixe).
func NewEventServer(port int) (*EventServer, error) {
	ln, err := net.Listen("tcp4", ":"+strconv.Itoa(port))
	if err != nil && port != 0 {
		logf("event server: port %d unavailable (%v), using a free port", port, err)
		ln, err = net.Listen("tcp4", ":0")
	}
	if err != nil {
		return nil, err
	}
	return &EventServer{ln: ln, conns: map[*esConn]bool{}, subs: map[string]map[*esConn]bool{},
		counters: map[string]uint32{}}, nil
}

// Port returns the TCP port actually listened on.
// Port renvoie le port TCP réellement écouté.
func (es *EventServer) Port() int { return es.ln.Addr().(*net.TCPAddr).Port }

// Run accepts connections and sends "es.alive" until stopped.
// Run accepte les connexions et émet « es.alive » jusqu'à l'arrêt.
func (es *EventServer) Run(stop <-chan struct{}) {
	go func() {
		<-stop
		es.ln.Close()
		es.mu.Lock()
		for c := range es.conns {
			c.c.Close()
		}
		es.mu.Unlock()
	}()
	go es.aliveLoop(stop)
	for {
		c, err := es.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		// Same rule as the music server: local network only.
		// Même règle que le serveur de musique : réseau local seulement.
		if !isLocal(c.RemoteAddr().String()) {
			c.Close()
			continue
		}
		es.mu.Lock()
		full := len(es.conns) >= esMaxConns
		var ec *esConn
		if !full {
			ec = &esConn{c: c, out: make(chan string, esQueue), subs: map[string]bool{}}
			es.conns[ec] = true
		}
		es.mu.Unlock()
		if full {
			c.Close()
			continue
		}
		if tc, ok := c.(*net.TCPConn); ok {
			tc.SetNoDelay(true)
		}
		go es.writer(ec)
		go es.reader(ec)
	}
}

// aliveLoop sends the first value at once (1), then one every 5 s.
// aliveLoop envoie la première valeur tout de suite (1), puis toutes les 5 s.
func (es *EventServer) aliveLoop(stop <-chan struct{}) {
	t := time.NewTicker(esAlivePeriod)
	defer t.Stop()
	for {
		es.mu.Lock()
		es.alive++
		line := esAliveID + " " + strconv.FormatUint(uint64(es.alive), 10)
		for c := range es.subs[esAliveID] {
			es.sendLocked(c, line)
		}
		es.mu.Unlock()
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

// sendLocked queues a line. When the queue is full the connection is closed
// rather than silently losing a line (a missing "es.alive" would make the
// app drop the connection anyway, and it reconnects).
// sendLocked met une ligne en file. File pleine : la connexion est fermée
// plutôt que de perdre une ligne en silence (un « es.alive » manquant
// ferait couper l'application de toute façon, et elle se reconnecte).
func (es *EventServer) sendLocked(c *esConn, line string) {
	if c.closed {
		return
	}
	select {
	case c.out <- line:
	default:
		c.closed = true
		close(c.out)
		c.c.Close()
	}
}

// writer sends the queued lines of one connection.
// writer envoie les lignes en file d'une connexion.
func (es *EventServer) writer(c *esConn) {
	w := bufio.NewWriter(c.c)
	for line := range c.out {
		c.c.SetWriteDeadline(time.Now().Add(esWriteTimeout))
		w.WriteString(line)
		w.WriteByte('\n')
		// Flush the buffer once nothing more is waiting.
		// Vide le tampon quand il n'y a plus rien en attente.
		if len(c.out) == 0 {
			if err := w.Flush(); err != nil {
				c.c.Close()
				// Drain the queue until drop() closes it.
				// Draine la file jusqu'à sa fermeture par drop().
				for range c.out {
				}
				return
			}
		}
	}
}

// reader handles the incoming lines of one connection until it ends.
// reader traite les lignes reçues d'une connexion jusqu'à sa fin.
func (es *EventServer) reader(c *esConn) {
	defer es.drop(c)
	r := bufio.NewReaderSize(c.c, 1024)
	for {
		line, err := readLine(r)
		if err != nil {
			return
		}
		es.handleLine(c, line)
	}
}

// readLine reads up to the LF and strips a single trailing CR. A line that
// is too long closes the connection (no legitimate id comes near that size).
// readLine lit jusqu'au LF ; retire un seul CR final. Une ligne trop
// longue ferme la connexion (aucun id légitime n'approche cette taille).
func readLine(r *bufio.Reader) (string, error) {
	var b []byte
	for {
		frag, err := r.ReadSlice('\n')
		b = append(b, frag...)
		if len(b) > esMaxLine {
			return "", errors.New("line too long")
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return "", err
		}
		break
	}
	s := string(b[:len(b)-1])
	return strings.TrimSuffix(s, "\r"), nil
}

// handleLine applies one client line; subscriber changes are reported to
// OnSubscribers after the lock is released.
// handleLine applique une ligne du client ; les changements d'abonnés sont
// signalés à OnSubscribers après libération du verrou.
func (es *EventServer) handleLine(c *esConn, line string) {
	var deltas []string // ids whose subscriber count changes (+ or -) / ids dont le nombre d'abonnés change (+ ou -)
	var signs []int
	es.mu.Lock()
	if c.ended {
		es.mu.Unlock()
		return
	}
	switch {
	case line == "":
		// End of session: subscriptions removed, connection kept open
		// until the client closes it.
		// Fin de session : abonnements retirés, connexion gardée ouverte
		// jusqu'à ce que le client la ferme.
		c.ended = true
		for id := range c.subs {
			es.unsubLocked(c, id)
			deltas, signs = append(deltas, id), append(signs, -1)
		}
	case line[0] == '+':
		id := line[1:]
		if id != "" && !c.subs[id] {
			c.subs[id] = true
			if es.subs[id] == nil {
				es.subs[id] = map[*esConn]bool{}
			}
			es.subs[id][c] = true
			deltas, signs = append(deltas, id), append(signs, 1)
		}
		if id != "" {
			es.sendLocked(c, id+" 0")
		}
	case line[0] == '-':
		id := line[1:]
		if c.subs[id] {
			es.unsubLocked(c, id)
			deltas, signs = append(deltas, id), append(signs, -1)
		}
	case line[0] == '=':
		f := strings.Fields(line[1:])
		if len(f) == 2 {
			if n, err := strconv.ParseUint(f[1], 10, 32); err == nil && f[0] != esAliveID {
				for sc := range es.subs[f[0]] {
					es.sendLocked(sc, f[0]+" "+strconv.FormatUint(n, 10))
				}
			}
		}
	}
	cb := es.OnSubscribers
	es.mu.Unlock()
	if cb != nil {
		for i, id := range deltas {
			cb(id, signs[i])
		}
	}
}

func (es *EventServer) unsubLocked(c *esConn, id string) {
	delete(c.subs, id)
	if m := es.subs[id]; m != nil {
		delete(m, c)
		if len(m) == 0 {
			delete(es.subs, id)
		}
	}
}

// drop removes a finished connection and its subscriptions.
// drop retire une connexion terminée et ses abonnements.
func (es *EventServer) drop(c *esConn) {
	var gone []string
	es.mu.Lock()
	for id := range c.subs {
		es.unsubLocked(c, id)
		gone = append(gone, id)
	}
	delete(es.conns, c)
	if !c.closed {
		c.closed = true
		close(c.out)
	}
	cb := es.OnSubscribers
	es.mu.Unlock()
	c.c.Close()
	if cb != nil {
		for _, id := range gone {
			cb(id, -1)
		}
	}
}

// Notify publishes "id n" to the subscribers, n being the id's own counter
// (starting at 1). Without subscribers nothing is sent or counted.
// Notify publie « id n » aux abonnés, n = compteur propre à l'id (à partir
// de 1). Sans abonné, rien n'est envoyé ni compté.
func (es *EventServer) Notify(id string) {
	es.mu.Lock()
	defer es.mu.Unlock()
	if len(es.subs[id]) == 0 {
		return
	}
	es.counters[id]++
	line := id + " " + strconv.FormatUint(uint64(es.counters[id]), 10)
	for c := range es.subs[id] {
		es.sendLocked(c, line)
	}
}

// Forget clears the counter of an id that no longer exists (destroyed session).
// Forget efface le compteur d'un id qui n'existe plus (session détruite).
func (es *EventServer) Forget(id string) {
	es.mu.Lock()
	delete(es.counters, id)
	es.mu.Unlock()
}

// Subscribers returns the number of subscribers to an id.
// Subscribers renvoie le nombre d'abonnés à un id.
func (es *EventServer) Subscribers(id string) int {
	es.mu.Lock()
	defer es.mu.Unlock()
	return len(es.subs[id])
}
