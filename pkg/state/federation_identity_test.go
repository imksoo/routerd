// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"errors"
	"testing"
	"time"
)

func TestFederationEventIDBindsEveryImmutableFactField(t *testing.T) {
	for _, field := range []string{"group", "source", "type", "subject", "dedupe", "payload"} {
		t.Run(field, func(t *testing.T) {
			store := mustOpenStore(t)
			defer store.Close()
			now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
			original := EventRecord{ID: "event-id", Group: "group", SourceNode: "source", Type: "routerd.fact", Subject: "192.0.2.1/32", DedupeKey: "dedupe", Payload: map[string]string{"owner": "first"}, ObservedAt: now, ExpiresAt: now.Add(time.Minute)}
			if err := store.RecordFederationEvent(original); err != nil {
				t.Fatal(err)
			}
			conflict := original
			conflict.ExpiresAt = now.Add(time.Hour)
			switch field {
			case "group":
				conflict.Group = "other-group"
			case "source":
				conflict.SourceNode = "other-source"
			case "type":
				conflict.Type = "routerd.other"
			case "subject":
				conflict.Subject = "192.0.2.2/32"
			case "dedupe":
				conflict.DedupeKey = "other-dedupe"
			case "payload":
				conflict.Payload = map[string]string{"owner": "other"}
			}
			if err := store.RecordFederationEvent(conflict); !errors.Is(err, ErrFederationEventIdentityConflict) {
				t.Fatalf("conflicting %s was admitted: %v", field, err)
			}
			rows, err := store.ListFederationEvents("", true, now.Unix())
			if err != nil || len(rows) != 1 || rows[0].Group != original.Group || rows[0].SourceNode != original.SourceNode || rows[0].Payload["owner"] != "first" || !rows[0].ExpiresAt.Equal(original.ExpiresAt) {
				t.Fatalf("conflicting refresh changed original: %+v %v", rows, err)
			}
			original.ObservedAt = now.Add(time.Second)
			original.ExpiresAt = now.Add(2 * time.Minute)
			if err := store.RecordFederationEvent(original); err != nil {
				t.Fatalf("same-fact lease refresh failed: %v", err)
			}
			rows, err = store.ListFederationEvents("group", true, now.Unix())
			if err != nil || len(rows) != 1 || !rows[0].ExpiresAt.Equal(original.ExpiresAt) {
				t.Fatalf("same-fact lease did not refresh: %+v %v", rows, err)
			}
		})
	}
}
