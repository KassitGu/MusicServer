package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOtherPlayers: a standard UPnP player is listed with its brand; an
// OpenHome device is not.
// TestOtherPlayers : un lecteur UPnP standard est listé avec sa marque ; un
// appareil OpenHome ne l'est pas.
func TestOtherPlayers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dlna.xml":
			w.Write([]byte(`<root><device><friendlyName>Living room</friendlyName><manufacturer>Acme Audio</manufacturer>` +
				`<modelName>Streamer 2</modelName><serviceList><service><serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType>` +
				`<controlURL>/av</controlURL></service></serviceList></device></root>`))
		case "/oh.xml":
			w.Write([]byte(`<root><device><friendlyName>Main Room</friendlyName><manufacturer>Linn Products Ltd</manufacturer>` +
				`<modelName>Klimax DSM</modelName><serviceList><service><serviceType>urn:av-openhome-org:service:Product:4</serviceType>` +
				`<controlURL>/p</controlURL></service></serviceList></device></root>`))
		}
	}))
	defer srv.Close()
	p, err := LoadOtherPlayer(srv.URL + "/dlna.xml")
	if err != nil || p == nil || p.Name != "Living room" || p.Brand != "Acme Audio Streamer 2" || p.Host != "127.0.0.1" {
		t.Fatalf("UPnP player: %+v, %v", p, err)
	}
	if p, err := LoadOtherPlayer(srv.URL + "/oh.xml"); err != nil || p != nil {
		t.Fatalf("OpenHome device listed: %+v, %v", p, err)
	}
	for in, want := range map[[2]string]string{
		{"Linn Products Ltd", "Klimax DSM"}: "Linn Products Ltd Klimax DSM",
		{"NAD", "NAD C 658"}:                "NAD C 658",
		{"", "Streamer"}:                    "Streamer",
		{"Acme", ""}:                        "Acme",
	} {
		if got := brand(xmlDevice{Manufacturer: in[0], ModelName: in[1]}); got != want {
			t.Errorf("brand(%q) = %q, want %q", in, got, want)
		}
	}
}
