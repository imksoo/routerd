// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestSAMEnrollmentClientJoinsWhenRRSetMissing(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 1 || client.fetchCount != 1 {
		t.Fatalf("submit/fetch = %d/%d, want 1/1", client.submitCount, client.fetchCount)
	}
	if records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs"); err != nil || len(records) != 1 {
		t.Fatalf("SAMRRSet dynamic records = %#v err=%v, want one", records, err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Ready" || status["observedRRSet"] != "SAMRRSet/pve-rrs" || status["lastSuccess"] == "" {
		t.Fatalf("status = %#v", status)
	}
}

func TestSAMEnrollmentClientSkipsValidNonExpiringRRSet(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")

	if err := seedSAMEnrollmentClientRRSet(store, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("seed rrset: %v", err)
	}
	status := map[string]any{"claimDigest": samEnrollmentClientClaimDigest(testSAMEnrollmentClaimResource("nonce-a"))}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", status); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 0 || client.fetchCount != 0 {
		t.Fatalf("submit/fetch = %d/%d, want 0/0", client.submitCount, client.fetchCount)
	}
}

func TestSAMEnrollmentClientRefreshesNearExpiry(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")

	if err := seedSAMEnrollmentClientRRSet(store, now.Add(-4*time.Minute), now.Add(2*time.Minute)); err != nil {
		t.Fatalf("seed rrset: %v", err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 1 || client.fetchCount != 1 {
		t.Fatalf("submit/fetch = %d/%d, want refresh 1/1", client.submitCount, client.fetchCount)
	}
}

func TestSAMEnrollmentClientBacksOffAfterFailure(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now, submitErr: errors.New("rr unavailable")}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-a")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 1 {
		t.Fatalf("submitCount = %d, want 1", client.submitCount)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")
	if status["phase"] != "Degraded" || status["backoff"] != "10s" || status["nextAttempt"] == "" {
		t.Fatalf("failure status = %#v", status)
	}
	client.submitErr = nil
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if client.submitCount != 1 {
		t.Fatalf("submitCount after backoff-gated reconcile = %d, want still 1", client.submitCount)
	}
}

func TestSAMEnrollmentClientRefreshesWhenClaimChanges(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	client := &fakeSAMEnrollmentJoinClient{now: now}
	controller := testSAMEnrollmentClientController(store, client, now, "nonce-b")

	if err := seedSAMEnrollmentClientRRSet(store, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("seed rrset: %v", err)
	}
	status := map[string]any{"claimDigest": samEnrollmentClientClaimDigest(testSAMEnrollmentClaimResource("nonce-a"))}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a", status); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if client.submitCount != 1 || client.fetchCount != 1 {
		t.Fatalf("submit/fetch = %d/%d, want claim-change refresh 1/1", client.submitCount, client.fetchCount)
	}
	if got := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "pve-leaf-a")["reason"]; got != "claim-changed" {
		t.Fatalf("status reason = %#v, want claim-changed", got)
	}
}

func TestSAMEnrollmentClientSubmitsToAllBootstrapEndpoints(t *testing.T) {
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	store := newSAMEnrollmentClientTestStore()
	rr1 := &fakeSAMEnrollmentJoinClient{now: now}
	rr2 := &fakeSAMEnrollmentJoinClient{now: now}
	controller := SAMEnrollmentClientController{
		Router: testSAMEnrollmentClientRouter("nonce-a"),
		Store:  store,
		Now:    func() time.Time { return now },
		ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
			return []SAMEnrollmentJoinClient{rr1, rr2}
		},
	}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if rr1.submitCount != 1 || rr2.submitCount != 1 {
		t.Fatalf("submit counts rr1/rr2 = %d/%d, want 1/1", rr1.submitCount, rr2.submitCount)
	}
	if rr1.fetchCount+rr2.fetchCount != 1 {
		t.Fatalf("total fetch count = %d, want 1", rr1.fetchCount+rr2.fetchCount)
	}
	if records, err := store.GetDynamicConfigPartsBySource("SAMRRSet/pve-rrs"); err != nil || len(records) != 1 {
		t.Fatalf("SAMRRSet dynamic records = %#v err=%v, want one", records, err)
	}
}

type fakeSAMEnrollmentJoinClient struct {
	now         time.Time
	submitErr   error
	fetchErr    error
	submitCount int
	fetchCount  int
}

func (c *fakeSAMEnrollmentJoinClient) SubmitSAMEnrollmentClaim(context.Context, controlapi.SAMEnrollmentClaimSubmitRequest) (*controlapi.SAMEnrollmentClaimSubmitResult, error) {
	c.submitCount++
	if c.submitErr != nil {
		return nil, c.submitErr
	}
	result := controlapi.NewSAMEnrollmentClaimSubmitResult("SAMEnrollmentClaim/pve-leaf-a", "SAMEnrollmentClaim/pve-leaf-a", 1, c.now, c.now.Add(time.Hour))
	return &result, nil
}

func (c *fakeSAMEnrollmentJoinClient) GetSAMRRSet(context.Context, controlapi.SAMRRSetGetRequest) (*controlapi.SAMRRSetGetResult, error) {
	c.fetchCount++
	if c.fetchErr != nil {
		return nil, c.fetchErr
	}
	result := controlapi.NewSAMRRSetGetResult("pve-rrs", testSAMEnrollmentRRSetResource())
	return &result, nil
}

func testSAMEnrollmentClientController(store *samEnrollmentClientTestStore, client *fakeSAMEnrollmentJoinClient, now time.Time, nonce string) SAMEnrollmentClientController {
	return SAMEnrollmentClientController{
		Router: testSAMEnrollmentClientRouter(nonce),
		Store:  store,
		Now:    func() time.Time { return now },
		ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient {
			return []SAMEnrollmentJoinClient{client}
		},
	}
}

func testSAMEnrollmentClientRouter(nonce string) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: "routerd.net/v1alpha1", Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "pve-leaf-a"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			testSAMEnrollmentClaimResource(nonce),
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentClient"},
				Metadata: api.ObjectMeta{Name: "pve-leaf-a"},
				Spec: api.SAMEnrollmentClientSpec{
					ClaimRef:              "SAMEnrollmentClaim/pve-leaf-a",
					BootstrapEndpoints:    []string{"http://10.30.0.10:8080"},
					StateTTLRefreshBefore: "10m",
					RetryBackoff:          api.SAMEnrollmentRetryBackoffSpec{Min: "10s", Max: "15m"},
				},
			},
		}},
	}
}

func testSAMEnrollmentClaimResource(nonce string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentClaim"},
		Metadata: api.ObjectMeta{Name: "pve-leaf-a"},
		Spec: api.SAMEnrollmentClaimSpec{
			PolicyRef:     "SAMEnrollmentPolicy/pve-wg-leaves",
			RRSetRef:      "SAMRRSet/pve-rrs",
			LeafID:        "pve-leaf-a",
			JoinNonce:     nonce,
			TunnelAddress: "10.255.10.21/32",
		},
	}
}

func testSAMEnrollmentRRSetResource() api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMRRSet"},
		Metadata: api.ObjectMeta{Name: "pve-rrs"},
		Spec: api.SAMRRSetSpec{
			EnrollmentPolicyRef: "SAMEnrollmentPolicy/pve-wg-leaves",
			Members: []api.SAMRRSetMember{{
				NodeRef:       "pve-rr",
				Endpoint:      "10.30.0.10",
				TunnelAddress: "10.255.10.1/32",
			}},
		},
	}
}

func seedSAMEnrollmentClientRRSet(store *samEnrollmentClientTestStore, observedAt, expiresAt time.Time) error {
	record, err := samEnrollmentClientRRSetRecord(testSAMEnrollmentRRSetResource(), observedAt, expiresAt)
	if err != nil {
		return err
	}
	return store.UpsertDynamicConfigPart(record)
}

type samEnrollmentClientTestStore struct {
	status map[string]map[string]any
	parts  map[string][]routerstate.DynamicConfigPartRecord
}

func newSAMEnrollmentClientTestStore() *samEnrollmentClientTestStore {
	return &samEnrollmentClientTestStore{
		status: map[string]map[string]any{},
		parts:  map[string][]routerstate.DynamicConfigPartRecord{},
	}
}

func (s *samEnrollmentClientTestStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s.status[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s *samEnrollmentClientTestStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	status := s.status[apiVersion+"/"+kind+"/"+name]
	if status == nil {
		return map[string]any{}
	}
	return status
}

func (s *samEnrollmentClientTestStore) UpsertDynamicConfigPart(record routerstate.DynamicConfigPartRecord) error {
	records := s.parts[record.Source]
	for i, existing := range records {
		if existing.Generation == record.Generation {
			records[i] = record
			s.parts[record.Source] = records
			return nil
		}
	}
	s.parts[record.Source] = append(records, record)
	return nil
}

func (s *samEnrollmentClientTestStore) GetDynamicConfigPartsBySource(source string) ([]routerstate.DynamicConfigPartRecord, error) {
	return append([]routerstate.DynamicConfigPartRecord(nil), s.parts[source]...), nil
}

func assertSAMEnrollmentClientRecordContains(t *testing.T, record routerstate.DynamicConfigPartRecord, want string) {
	t.Helper()
	var resources []api.Resource
	if err := json.Unmarshal([]byte(record.ResourcesJSON), &resources); err != nil {
		t.Fatalf("resources json: %v", err)
	}
	if !strings.Contains(record.ResourcesJSON, want) {
		t.Fatalf("record resources = %#v, want %q", resources, want)
	}
}
