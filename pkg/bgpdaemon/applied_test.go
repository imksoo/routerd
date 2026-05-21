// SPDX-License-Identifier: BSD-3-Clause

package bgpdaemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAppliedAtomicRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bgp", "applied.json")
	config := AppliedConfig{
		Global: AppliedGlobal{ASN: 64512, RouterID: "10.0.0.1", ListenPort: 179, Families: []string{"ipv4-unicast"}, UseMultiplePaths: true},
		Peers: map[string]AppliedPeer{
			"10.0.0.2": {Address: "10.0.0.2", ASN: 64513, TimersProfile: "fast"},
		},
		Advertisements: []string{"10.20.0.0/24"},
	}
	if err := WriteApplied(path, config); err != nil {
		t.Fatalf("write applied: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat applied: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Fatalf("applied file mode = %o, want 0600", mode)
	}
	got, ok, err := ReadApplied(path)
	if err != nil || !ok {
		t.Fatalf("read applied ok=%t err=%v", ok, err)
	}
	if got.Version != AppliedVersion || got.Global.ASN != 64512 || got.Peers["10.0.0.2"].TimersProfile != "fast" {
		t.Fatalf("applied config = %#v", got)
	}
	if Hash(got) == "" {
		t.Fatal("hash is empty")
	}
}

func TestValidateRejectsIncompleteAppliedConfig(t *testing.T) {
	if err := Validate(AppliedConfig{Version: AppliedVersion}); err == nil {
		t.Fatal("Validate accepted missing global")
	}
}
