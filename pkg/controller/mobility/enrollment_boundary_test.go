// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestEnrollmentRejectsCanceledOrExpiredResultBeforeDesiredPublication(t *testing.T) {
	for _, mode := range []string{"canceled", "expired", "missing-receipt"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
			store := testStore(t, now)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := &fakeSAMEnrollmentJoinClient{now: now}
			switch mode {
			case "canceled":
				client.afterFetch = cancel
			case "expired":
				client.afterFetch = func() { now = now.Add(time.Hour) }
			case "missing-receipt":
				result := controlapi.NewSAMEnrollmentClaimSubmitResult("SAMEnrollmentClaim/pve-leaf-a", "SAMEnrollmentClaim/pve-leaf-a", 1, now, time.Time{})
				client.submitResultSet = true
				client.submitResult = &result
			}
			controller := SAMEnrollmentClientController{Router: testSAMEnrollmentClientRouter("nonce"), Store: store, Now: func() time.Time { return now }, ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient { return []SAMEnrollmentJoinClient{client} }}
			if err := controller.Reconcile(ctx); err == nil {
				t.Fatal("unconfirmed enrollment result reported success")
			}
			records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs")
			if err != nil || len(records) != 0 {
				t.Fatalf("invalid result published desired topology: %+v err=%v", records, err)
			}
			status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
			if status["phase"] == "Ready" || status["lastSuccess"] != nil {
				t.Fatalf("invalid result marked ready: %+v", status)
			}
		})
	}
}

func TestDirectEnrollmentUsesEveryRequiredAdmissionLease(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "long-first", true: "short-first"}[reverse], func(t *testing.T) {
			now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
			store := testStore(t, now)
			peerGroup := testSAMEnrollmentDirectPeerGroupResource()
			long := controlapi.NewSAMEnrollmentClaimSubmitResult("SAMEnrollmentClaim/pve-leaf-a", "SAMEnrollmentClaim/pve-leaf-a", 1, now, now.Add(2*time.Hour))
			short := controlapi.NewSAMEnrollmentClaimSubmitResult("SAMEnrollmentClaim/pve-leaf-a", "SAMEnrollmentClaim/pve-leaf-a", 1, now, now.Add(5*time.Minute))
			clients := []SAMEnrollmentJoinClient{&fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup, submitResultSet: true, submitResult: &long}, &fakeSAMEnrollmentJoinClient{now: now, peerGroup: &peerGroup, submitResultSet: true, submitResult: &short}}
			if reverse {
				clients[0], clients[1] = clients[1], clients[0]
			}
			controller := SAMEnrollmentClientController{Router: testSAMEnrollmentClientRouter("nonce"), Store: store, Now: func() time.Time { return now }, ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient { return clients }}
			setSAMEnrollmentClientDirectMesh(t, controller.Router, true)
			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs")
			if err != nil || len(records) != 1 || !records[0].ExpiresAt.Equal(short.ExpiresAt) {
				t.Fatalf("direct topology outlived required admission: %+v err=%v", records, err)
			}
		})
	}
}

func TestEnrollmentDoesNotReadmitOnMalformedPersistedContinuationOrTopology(t *testing.T) {
	for _, corrupt := range []string{"continuation", "topology"} {
		t.Run(corrupt, func(t *testing.T) {
			now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
			store := testStore(t, now)
			if corrupt == "continuation" {
				if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", map[string]any{"nextAttempt": "not-a-time"}); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{Source: "SAMRRSet/pve-rrs", Generation: 1, ObservedAt: now, ExpiresAt: now.Add(time.Hour), ResourcesJSON: "{", Status: "active"}); err != nil {
					t.Fatal(err)
				}
			}
			client := &fakeSAMEnrollmentJoinClient{now: now}
			controller := SAMEnrollmentClientController{Router: testSAMEnrollmentClientRouter("nonce"), Store: store, Now: func() time.Time { return now }, ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient { return []SAMEnrollmentJoinClient{client} }}
			err := controller.Reconcile(context.Background())
			if err == nil || !strings.Contains(err.Error(), "decode") || client.submitCount != 0 || client.fetchCount != 0 {
				t.Fatalf("corruption became absence/readmission: err=%v submits=%d fetches=%d", err, client.submitCount, client.fetchCount)
			}
		})
	}
}
