// SPDX-License-Identifier: BSD-3-Clause

package wireguard

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWireGuardDumpPresharedKeyComparisonDoesNotSerializeSecret(t *testing.T) {
	const psk = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	status, err := ParseInterfaceDump("wg0", []byte("private\tpublic\t51820\toff\npeer\t"+psk+"\t(none)\t10.0.0.1/32\t0\t0\t0\t0\n"))
	if err != nil || len(status.Peers) != 1 || status.Peers[0].PresharedKeyDigest != sha256.Sum256([]byte(psk)) {
		t.Fatalf("native PSK comparison observation missing: err=%v", err)
	}
	encoded, err := json.Marshal(status)
	if err != nil || strings.Contains(string(encoded), psk) || strings.Contains(string(encoded), "PresharedKey") {
		t.Fatalf("native PSK material serialized: %s err=%v", encoded, err)
	}
}

func TestInterfaceDumpWithoutPeersRetainsInterfaceObservation(t *testing.T) {
	status, err := ParseInterfaceDump("wg0", []byte("private\tpublic\t51820\t0x100\n"))
	if err != nil || status.Name != "wg0" || status.PublicKey != "public" || status.ListenPort != 51820 || status.FwMark != "0x100" || len(status.Peers) != 0 {
		t.Fatalf("one-line native dump lost interface state: %+v err=%v", status, err)
	}
}

func TestResolveKeyFilesDoesNotMutateCallerPeers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preshared.key")
	if err := os.WriteFile(path, []byte("preshared-value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := InterfaceConfig{Peers: []PeerConfig{{Name: "peer", PresharedKeyFile: path}}}
	resolved, err := ResolveKeyFiles(cfg)
	if err != nil || resolved.Peers[0].PresharedKey != "preshared-value" {
		t.Fatalf("resolve key: %+v err=%v", resolved, err)
	}
	if cfg.Peers[0].PresharedKey != "" {
		t.Fatalf("key resolution mutated caller-owned configuration: %+v", cfg.Peers)
	}
}
