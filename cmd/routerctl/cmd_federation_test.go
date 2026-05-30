// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestFederationEventEmitThenList(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "fed.db")

	var emitOut bytes.Buffer
	emitArgs := []string{
		"federation", "event", "emit",
		"--state-file", statePath,
		"--group", "cloudedge",
		"--type", "routerd.client.ipv4.observed",
		"--subject", "10.88.60.9/32",
		"--source-node", "onprem",
		"--id", "evt-test-1",
		"--payload", "mac=aa:bb:cc:dd:ee:ff",
		"--ttl", "30m",
		"-o", "json",
	}
	if err := run(emitArgs, &emitOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("emit: %v\n%s", err, emitOut.String())
	}

	var emitted routerstate.EventRecord
	if err := json.Unmarshal(emitOut.Bytes(), &emitted); err != nil {
		t.Fatalf("decode emit output: %v\n%s", err, emitOut.String())
	}
	if emitted.ID != "evt-test-1" {
		t.Fatalf("emitted id = %q, want evt-test-1", emitted.ID)
	}
	// DedupeKey defaults to ID when not provided.
	if emitted.DedupeKey != "evt-test-1" {
		t.Fatalf("emitted dedupeKey = %q, want it to default to id", emitted.DedupeKey)
	}
	if emitted.Payload["mac"] != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("emitted payload mac = %q, want round-trip", emitted.Payload["mac"])
	}
	if emitted.ExpiresAt.IsZero() {
		t.Fatalf("emitted expiresAt is zero, want ttl-derived value")
	}

	// List back, filtered by group.
	var listOut bytes.Buffer
	listArgs := []string{
		"federation", "event", "list",
		"--state-file", statePath,
		"--group", "cloudedge",
		"-o", "json",
	}
	if err := run(listArgs, &listOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("list: %v\n%s", err, listOut.String())
	}
	var listed []routerstate.EventRecord
	if err := json.Unmarshal(listOut.Bytes(), &listed); err != nil {
		t.Fatalf("decode list output: %v\n%s", err, listOut.String())
	}
	if len(listed) != 1 {
		t.Fatalf("listed %d events, want 1: %+v", len(listed), listed)
	}
	got := listed[0]
	if got.ID != "evt-test-1" || got.Group != "cloudedge" || got.Subject != "10.88.60.9/32" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.SourceNode != "onprem" {
		t.Fatalf("round-trip sourceNode = %q, want onprem", got.SourceNode)
	}
	if got.DedupeKey != "evt-test-1" {
		t.Fatalf("round-trip dedupeKey = %q, want evt-test-1", got.DedupeKey)
	}
	if got.Payload["mac"] != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("round-trip payload mac = %q", got.Payload["mac"])
	}

	// Group filter should exclude other groups.
	var otherOut bytes.Buffer
	if err := run([]string{
		"federation", "event", "list",
		"--state-file", statePath,
		"--group", "no-such-group",
		"-o", "json",
	}, &otherOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("list other group: %v", err)
	}
	var other []routerstate.EventRecord
	if err := json.Unmarshal(otherOut.Bytes(), &other); err != nil {
		t.Fatalf("decode other list: %v\n%s", err, otherOut.String())
	}
	if len(other) != 0 {
		t.Fatalf("group filter leaked %d events: %+v", len(other), other)
	}
}
