// SPDX-License-Identifier: BSD-3-Clause

package eventd

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestMarshalConfigJSONRoundTrip(t *testing.T) {
	orig := Config{
		NodeName:     "router06",
		Group:        "edge",
		Listen:       Listen{Address: "10.99.0.6", Port: 8787},
		SecretFile:   "/var/lib/routerd/eventd/edge/secret",
		ReplayWindow: 5 * time.Minute,
		Peers: []PeerConfig{
			{NodeName: "cloud01", Endpoint: "http://10.99.0.7:8787", Types: []string{"observed"}, SubjectPrefixes: []string{"arp."}},
			{NodeName: "cloud02", Endpoint: "http://10.99.0.8:8787"},
		},
		Retention:     Retention{MaxEvents: 1000, MaxAge: 24 * time.Hour},
		PushRetry:     PushRetry{MaxAttempts: 3, BaseBackoff: 200 * time.Millisecond, MaxBackoff: 5 * time.Second},
		PruneInterval: time.Minute,
		StatePath:     "/var/lib/routerd/routerd.db",
	}

	data, err := MarshalConfigJSON(orig)
	if err != nil {
		t.Fatalf("MarshalConfigJSON: %v", err)
	}

	got, err := DecodeConfig(func(v any) error { return json.Unmarshal(data, v) })
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}

	if !reflect.DeepEqual(orig, got) {
		t.Fatalf("round-trip mismatch:\n orig=%+v\n got =%+v", orig, got)
	}
}

func TestMarshalConfigJSONOmitsZeroDurations(t *testing.T) {
	data, err := MarshalConfigJSON(Config{NodeName: "n", Group: "g", SecretFile: "s", StatePath: "p"})
	if err != nil {
		t.Fatalf("MarshalConfigJSON: %v", err)
	}
	var w map[string]any
	if err := json.Unmarshal(data, &w); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := w["replayWindow"]; ok {
		t.Fatalf("expected replayWindow to be omitted, got %v", w["replayWindow"])
	}
	if _, ok := w["pruneInterval"]; ok {
		t.Fatalf("expected pruneInterval to be omitted")
	}
}
