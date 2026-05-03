package wireguard

import (
	"strings"
	"testing"
	"time"
)

func TestRenderSetConf(t *testing.T) {
	data, err := RenderSetConf(InterfaceConfig{
		Name:       "wg0",
		PrivateKey: "priv",
		ListenPort: 51820,
		FwMark:     100,
		Peers: []PeerConfig{{
			Name:                "peer-a",
			PublicKey:           "pub",
			PresharedKey:        "psk",
			AllowedIPs:          []string{"10.10.0.2/32", "fd00::2/128"},
			Endpoint:            "198.51.100.10:51820",
			PersistentKeepalive: 25,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		"[Interface]",
		"PrivateKey = priv",
		"ListenPort = 51820",
		"FwMark = 100",
		"[Peer]",
		"PublicKey = pub",
		"AllowedIPs = 10.10.0.2/32, fd00::2/128",
		"Endpoint = 198.51.100.10:51820",
		"PersistentKeepalive = 25",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered config missing %q:\n%s", want, got)
		}
	}
}

func TestParseDump(t *testing.T) {
	data := "priv\tpub\t51820\toff\npeerpub\tpsk\t10.0.0.2/32\t203.0.113.2:51820\t10.0.0.2/32\t1710000000\t100\t200\t25\n"
	peers, err := ParseDump([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 {
		t.Fatalf("peers = %+v, want one", peers)
	}
	if peers[0].PublicKey != "peerpub" || peers[0].LatestEndpoint != "203.0.113.2:51820" {
		t.Fatalf("peer = %+v", peers[0])
	}
	if want := time.Unix(1710000000, 0).UTC(); !peers[0].LatestHandshake.Equal(want) {
		t.Fatalf("handshake = %s, want %s", peers[0].LatestHandshake, want)
	}
	if peers[0].TransferRxBytes != 100 || peers[0].TransferTxBytes != 200 {
		t.Fatalf("transfer = %+v", peers[0])
	}
}
