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
		Paths: []AppliedPath{{
			Source: "MobilityPool/demo/node/aws-router-a",
			Prefix: "10.77.60.11/32",
			Attrs:  AppliedPathAttrs{LocalPref: 200, Communities: []string{"64512:77"}},
			UUID:   EncodeUUID([]byte{1, 2, 3}),
		}},
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
	if len(got.Paths) != 2 {
		t.Fatalf("paths = %#v, want legacy static path plus mobility path", got.Paths)
	}
	if got.Advertisements[0] != "10.20.0.0/24" {
		t.Fatalf("legacy advertisements = %#v", got.Advertisements)
	}
	if Hash(got) == "" {
		t.Fatal("hash is empty")
	}
}

func TestNormalizeMigratesLegacyAdvertisementsToStaticPaths(t *testing.T) {
	got := Normalize(AppliedConfig{Advertisements: []string{"10.20.0.0/24", "10.20.0.0/24"}})
	if len(got.Paths) != 1 || got.Paths[0].Source != AppliedPathSourceStatic || got.Paths[0].Prefix != "10.20.0.0/24" {
		t.Fatalf("normalized paths = %#v, want one static path", got.Paths)
	}
	if len(got.Advertisements) != 1 || got.Advertisements[0] != "10.20.0.0/24" {
		t.Fatalf("advertisements = %#v, want normalized legacy view", got.Advertisements)
	}
}

func TestValidateRejectsInvalidAppliedPath(t *testing.T) {
	config := AppliedConfig{
		Version: AppliedVersion,
		Global:  AppliedGlobal{ASN: 64512, RouterID: "10.0.0.1"},
		Paths:   []AppliedPath{{Source: "MobilityPool/demo/node/a", Prefix: "10.0.0.1/32", Family: AppliedPathFamilyIPv6Unicast}},
	}
	if err := Validate(config); err == nil {
		t.Fatal("Validate accepted mismatched path family")
	}
}

func TestValidateRejectsIncompleteAppliedConfig(t *testing.T) {
	if err := Validate(AppliedConfig{Version: AppliedVersion}); err == nil {
		t.Fatal("Validate accepted missing global")
	}
}
