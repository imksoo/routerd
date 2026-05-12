// SPDX-License-Identifier: BSD-3-Clause

package wireguard

import (
	"os"
	"path/filepath"
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

func TestResolveKeyFiles(t *testing.T) {
	dir := t.TempDir()
	privateKey := filepath.Join(dir, "private.key")
	presharedKey := filepath.Join(dir, "peer.psk")
	if err := os.WriteFile(privateKey, []byte("priv-from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(presharedKey, []byte("psk-from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := ResolveKeyFiles(InterfaceConfig{
		Name:           "wg0",
		PrivateKeyFile: privateKey,
		Peers: []PeerConfig{{
			Name:             "peer-a",
			PublicKey:        "pub",
			AllowedIPs:       []string{"10.10.0.2/32"},
			PresharedKeyFile: presharedKey,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PrivateKey != "priv-from-file" || cfg.Peers[0].PresharedKey != "psk-from-file" {
		t.Fatalf("resolved keys = %+v", cfg)
	}
}

func TestParseDump(t *testing.T) {
	data := "priv\tpub\t51820\toff\npeerpub\tpsk\t203.0.113.2:51820\t10.0.0.2/32\t1710000000\t100\t200\t25\n"
	status, err := ParseInterfaceDump("wg0", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if status.Name != "wg0" || status.PublicKey != "pub" || status.ListenPort != 51820 {
		t.Fatalf("interface status = %+v", status)
	}
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

func TestParseAllDump(t *testing.T) {
	data := "wg0\tpriv\tpub\t51820\toff\nwg0\tpeerpub\tpsk\t203.0.113.2:51820\t10.0.0.2/32\t1710000000\t100\t200\t25\n"
	interfaces, err := ParseAllDump([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(interfaces) != 1 {
		t.Fatalf("interfaces = %+v, want one", interfaces)
	}
	if interfaces[0].Name != "wg0" || interfaces[0].PublicKey != "pub" || len(interfaces[0].Peers) != 1 {
		t.Fatalf("interface = %+v", interfaces[0])
	}
}
