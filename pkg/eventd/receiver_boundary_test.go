// SPDX-License-Identifier: BSD-3-Clause

package eventd_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/eventd"
	"github.com/imksoo/routerd/pkg/federation"
)

func TestReceiverBindsAuthenticatedGroupAndEventIdentity(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	store := openStore(t, "receiver.db")
	receiver := eventd.NewReceiver(store, testSecret, "group-a", "node-a", time.Minute, fixedClock(now))
	server := httptest.NewServer(receiver.Handler())
	defer server.Close()
	event := federation.Event{ID: "same-id", Group: "group-b", SourceNode: "node-b", Type: "routerd.fact", Payload: map[string]string{"owner": "original"}, ObservedAt: now, ExpiresAt: now.Add(time.Minute)}
	body := mustMarshal(t, event)
	if code := postRaw(t, server.URL+"/v1/events", now.Unix(), federation.Sign(testSecret, now.Unix(), body), body); code != http.StatusForbidden {
		t.Fatalf("cross-group signed event status = %d", code)
	}
	event.Group = "group-a"
	body = mustMarshal(t, event)
	if code := postRaw(t, server.URL+"/v1/events", now.Unix(), federation.Sign(testSecret, now.Unix(), body), body); code != http.StatusOK {
		t.Fatalf("valid signed event status = %d", code)
	}
	event.Payload["owner"] = "retagged"
	event.ExpiresAt = now.Add(time.Hour)
	body = mustMarshal(t, event)
	if code := postRaw(t, server.URL+"/v1/events", now.Unix(), federation.Sign(testSecret, now.Unix(), body), body); code != http.StatusConflict {
		t.Fatalf("conflicting signed identity status = %d", code)
	}
	stored, err := store.ListFederationEvents("", true, now.Unix())
	if err != nil || len(stored) != 1 || stored[0].Group != "group-a" || stored[0].Payload["owner"] != "original" || !stored[0].ExpiresAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("rejected request changed durable fact: %+v %v", stored, err)
	}
}

func TestReceiverRejectsOversizedSignedBodyAndUnknownStorage(t *testing.T) {
	now := time.Now().UTC()
	store := openStore(t, "receiver.db")
	receiver := eventd.NewReceiver(store, testSecret, "group", "node", time.Minute, fixedClock(now))
	server := httptest.NewServer(receiver.Handler())
	defer server.Close()
	body := mustMarshal(t, federation.Event{ID: "event", Group: "group", Type: "routerd.fact"})
	body = append(body, bytes.Repeat([]byte{' '}, (1<<20)+1-len(body))...)
	if code := postRaw(t, server.URL+"/v1/events", now.Unix(), federation.Sign(testSecret, now.Unix(), body), body); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized signed body status = %d", code)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Get(server.URL + "/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("unknown event storage was reported as healthy status %d", response.StatusCode)
	}
}
