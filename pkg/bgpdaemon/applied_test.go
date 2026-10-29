// SPDX-License-Identifier: BSD-3-Clause

package bgpdaemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWriteAppliedAtomicRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bgp", "applied.json")
	config := AppliedConfig{
		Global: AppliedGlobal{
			ASN:              64512,
			RouterID:         "10.0.0.1",
			ListenPort:       179,
			Families:         []string{"ipv4-unicast"},
			UseMultiplePaths: true,
			ImportPolicy:     AppliedImportPolicy{LocalPreference: 200},
		},
		Peers: map[string]AppliedPeer{
			"10.0.0.2": {
				Address:       "10.0.0.2",
				ASN:           64513,
				TimersProfile: "fast",
				ImportPolicy:  AppliedImportPolicy{LocalPreference: 201},
			},
		},
		Paths: []AppliedPath{StaticAppliedPath("10.20.0.0/24", nil), {
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
	if got.Global.ImportPolicy.LocalPreference != 200 || got.Peers["10.0.0.2"].ImportPolicy.LocalPreference != 201 {
		t.Fatalf("import local preferences = global=%d peer=%d, want 200/201", got.Global.ImportPolicy.LocalPreference, got.Peers["10.0.0.2"].ImportPolicy.LocalPreference)
	}
	if len(got.Paths) != 2 {
		t.Fatalf("paths = %#v, want static path plus mobility path", got.Paths)
	}
	if prefixes := StaticAdvertisements(got.Paths); len(prefixes) != 1 || prefixes[0] != "10.20.0.0/24" {
		t.Fatalf("static advertisements = %#v", prefixes)
	}
	if Hash(got) == "" {
		t.Fatal("hash is empty")
	}
}

func TestReadAppliedTreatsEmptyStateAsMissing(t *testing.T) {
	for _, data := range []string{"", " \n\t"} {
		path := filepath.Join(t.TempDir(), "applied.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		config, ok, err := ReadApplied(path)
		if err != nil || ok || !reflect.DeepEqual(config, AppliedConfig{}) {
			t.Fatalf("ReadApplied(empty) = config=%#v ok=%t err=%v", config, ok, err)
		}
	}
}

func TestReadAppliedRejectsNonEmptyMalformedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "applied.json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadApplied(path); err == nil {
		t.Fatal("ReadApplied accepted non-empty malformed state")
	}
}

func TestNormalizeDeduplicatesStaticPaths(t *testing.T) {
	got := Normalize(AppliedConfig{Paths: []AppliedPath{StaticAppliedPath("10.20.0.0/24", nil), StaticAppliedPath("10.20.0.0/24", nil)}})
	if len(got.Paths) != 1 || got.Paths[0].Source != AppliedPathSourceStatic || got.Paths[0].Prefix != "10.20.0.0/24" {
		t.Fatalf("normalized paths = %#v, want one static path", got.Paths)
	}
}

func TestMobilityPathSourceAcceptsOnlyCanonicalOwnershipProducer(t *testing.T) {
	for _, source := range []string{"MobilityPool/demo/node/aws-router-a", "MobilityPool/pool/node/router"} {
		if !IsMobilityPathSource(source) {
			t.Errorf("actual MobilityPool source %q was rejected", source)
		}
	}
	for _, source := range []string{"mobility/demo/node/router", "MobilityPool/", "MobilityPool/pool", "MobilityPool/pool/node/", "MobilityPool/pool/not-node/router", "MobilityPool/pool/node/router/arp-observer", "MobilityPool/pool/node/router/extra", AppliedPathSourceStatic} {
		if IsMobilityPathSource(source) {
			t.Errorf("non-owner source %q was accepted", source)
		}
		if source != AppliedPathSourceStatic {
			if err := ValidateAppliedPath(AppliedPath{Source: source, Prefix: "192.0.2.1/32"}); err == nil {
				t.Errorf("journal admitted unsupported owner %q", source)
			}
		}
	}
	if IsMobilityPathSource("MobilityPool/pool/node/router ") {
		t.Fatal("owner parser accepted a non-canonical source")
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

func TestValidateRejectsTransitionForNonDirectPeer(t *testing.T) {
	config := AppliedConfig{
		Version:                    AppliedVersion,
		Global:                     AppliedGlobal{ASN: 64512, RouterID: "10.0.0.1"},
		PendingDirectPeerAdditions: []string{"10.0.0.2"},
		PendingDirectPeerRemovals:  []string{"10.0.0.3"},
		PendingStaticPathRemovals:  nil,
		Peers:                      map[string]AppliedPeer{"10.0.0.2": {Address: "10.0.0.2", ASN: 64512}},
	}
	if err := Validate(config); err == nil {
		t.Fatal("Validate accepted pending direct transition for ordinary peer")
	}
}

func TestPendingDynamicPathIntentRoundTripAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "applied.json")
	intent := AppliedPath{Source: "MobilityPool/pool/node/a", Prefix: "10.77.60.11/32", Attrs: AppliedPathAttrs{Communities: []string{"64512:11"}}}
	config := AppliedConfig{Global: AppliedGlobal{ASN: 64512, RouterID: "192.0.2.1"}, PendingDynamicPathAdds: []AppliedPath{intent}}
	if err := Validate(config); err != nil {
		t.Fatal(err)
	}
	if err := WriteApplied(path, config); err != nil {
		t.Fatal(err)
	}
	got, found, err := ReadApplied(path)
	if err != nil || !found || len(got.PendingDynamicPathAdds) != 1 || len(got.Paths) != 0 {
		t.Fatalf("pending journal=%#v found=%t error=%v", got, found, err)
	}
	config.PendingDynamicPathAdds[0].UUID = EncodeUUID([]byte{1})
	if err := Validate(config); err == nil {
		t.Fatal("pending intent accepted an acknowledged UUID")
	}
}

func TestReadAppliedRejectsPreviousNativeIdentityFormat(t *testing.T) {
	for _, version := range []string{"1", "0"} {
		path := filepath.Join(t.TempDir(), "applied.json")
		data := `{"version":` + version + `,"global":{"asn":64512,"routerID":"192.0.2.1"},"paths":[{"source":"MobilityPool/pool/node/a","prefix":"10.77.60.11/32","uuid":"01"}]}`
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ReadApplied(path); err == nil {
			t.Fatal("old ID=0 journal was silently reinterpreted")
		}
	}
	if err := WriteApplied(filepath.Join(t.TempDir(), "applied.json"), AppliedConfig{Version: 1}); err == nil {
		t.Fatal("old explicit version was silently upgraded")
	}
}

func TestReadAppliedDoesNotDiscardIncompletePendingOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "applied.json")
	config := AppliedConfig{Version: AppliedVersion, Global: AppliedGlobal{ASN: 64512, RouterID: "192.0.2.1"}, PendingDynamicPathAdds: []AppliedPath{{Source: "MobilityPool/pool/node/a"}}}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadApplied(path); err == nil {
		t.Fatal("incomplete pending effect normalized into false absence")
	}
}
