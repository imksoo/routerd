// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type samJournalFaultStore struct {
	*routerstate.SQLiteStore
	failField   string
	failure     error
	failAtMerge int
	merges      int
}

func (s *samJournalFaultStore) MergeObjectStatus(version, kind, name string, update map[string]any) error {
	s.merges++
	if s.failAtMerge != 0 && s.merges == s.failAtMerge {
		return s.failure
	}
	if _, present := update[s.failField]; s.failField != "" && present {
		return s.failure
	}
	return s.SQLiteStore.MergeObjectStatus(version, kind, name, update)
}

type samBinarySettingApplier struct {
	fakeSAMApplier
	writes int
}

func (a *samBinarySettingApplier) SetProxyARP(_ context.Context, iface string, enabled, owned bool, beforeAcquire func() error) (samProxyARPApplyResult, error) {
	return applySAMOwnedBinarySetting(iface+".proxy_arp", enabled, owned,
		func() (string, error) { return "0", nil },
		func(string) error { a.writes++; return nil }, beforeAcquire)
}

func samJournalCapture() dynamicconfig.LocalCaptureIntent {
	return dynamicconfig.LocalCaptureIntent{
		ID: "capture-a", PoolRef: "cloudedge", PoolPrefix: "10.88.60.0/24", Address: "10.88.60.10/32",
		Disposition: dynamicconfig.CaptureDesired, CaptureType: "proxy-arp", CaptureInterface: "lan0", TunnelInterfaces: []string{"wg0"},
	}
}

func samJournalRequest() dynamicconfig.MobilityWithdrawalRequest {
	return dynamicconfig.MobilityWithdrawalRequest{
		ID: "transfer-a", GroupRef: "cloudedge", PoolRef: "cloudedge", PoolPrefix: "10.88.60.0/24", Address: "10.88.60.10/32",
		FromNodeRef: "source", ToNodeRef: "target", PlanDigest: dynamicconfig.MobilityNativePlanDigest(dynamicconfig.MobilityDataplanePlan{}),
	}
}

func samJournalStatus(t *testing.T, store *routerstate.SQLiteStore) map[string]any {
	t.Helper()
	status, err := store.ReadObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName)
	if err != nil {
		t.Fatal(err)
	}
	return status
}

func assertSAMJournalAcks(t *testing.T, status map[string]any, want []dynamicconfig.MobilityWithdrawalRequest) {
	t.Helper()
	var got []dynamicconfig.MobilityWithdrawalRequest
	if !decodeAppliedDataplaneStatus(status[dynamicconfig.MobilitySAMWithdrawalAcks], &got) || got == nil {
		t.Fatalf("missing typed SAM acknowledgement: %#v", status)
	}
	if len(got) != len(want) || len(got) > 0 && !reflect.DeepEqual(got, want) {
		t.Fatalf("SAM acknowledgements = %+v, want %+v", got, want)
	}
}

func TestSAMJournalPendingSurvivesEffectSuccessSaveFailureAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("final SAM ledger rejected")
	faults := &samJournalFaultStore{SQLiteStore: store, failField: "appliedProxyNeighbors", failure: failure}
	applier := &fakeSAMApplier{}
	controller := SAMController{Router: &api.Router{}, Store: faults, OS: platform.OSLinux, Applier: applier, LocalCaptureIntents: []dynamicconfig.LocalCaptureIntent{samJournalCapture()}}
	if err := controller.Reconcile(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("final-save failure = %v", err)
	}
	status := samJournalStatus(t, store)
	pending, err := samPendingProxyNeighbors(status["pendingProxyNeighbors"], true)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending neighbor was not durable: %+v %v", pending, err)
	}
	for _, row := range pending {
		if !row.ProxyEntryOwned {
			t.Fatal("successful native acquisition lost its ownership proof")
		}
	}
	for _, field := range []string{"pendingProxyARP", "pendingAcceptLocal"} {
		settings, err := samAppliedProxyARPSettings(status[field], true, platform.OSLinux)
		if err != nil || len(settings) == 0 {
			t.Fatalf("%s lost ownership: %+v %v", field, settings, err)
		}
	}
	assertSAMJournalAcks(t, status, nil)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cleanup := &fakeSAMApplier{deleteErr: errors.New("native deletion failed")}
	restarted := SAMController{Router: &api.Router{}, Store: store, OS: platform.OSLinux, Applier: cleanup, Withdrawals: []dynamicconfig.MobilityWithdrawalRequest{samJournalRequest()}}
	if err := restarted.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), cleanup.deleteErr.Error()) {
		t.Fatalf("failed removal was acknowledged: %v", err)
	}
	assertSAMJournalAcks(t, samJournalStatus(t, store), nil)
	pending, err = samPendingProxyNeighbors(samJournalStatus(t, store)["pendingProxyNeighbors"], true)
	if err != nil || len(pending) != 1 {
		t.Fatalf("failed removal lost pending ownership: %+v %v", pending, err)
	}
	cleanup.deleteErr = nil
	if err := restarted.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cleanup.proxyARP, []string{"lan0=0", "lan0=0"}) {
		t.Fatalf("pending proxy_arp cleanup = %+v", cleanup.proxyARP)
	}
	for _, iface := range []string{"lan0=0", "wg0=0"} {
		if !containsSAMSetting(cleanup.acceptLocal, iface) {
			t.Fatalf("pending accept_local %s was not recovered: %+v", iface, cleanup.acceptLocal)
		}
	}
	if !reflect.DeepEqual(cleanup.delete, []string{"10.88.60.10/32@lan0", "10.88.60.10/32@lan0"}) {
		t.Fatalf("pending neighbor cleanup = %+v", cleanup.delete)
	}
	status = samJournalStatus(t, store)
	assertSAMJournalAcks(t, status, restarted.Withdrawals)
	for _, field := range []string{"appliedProxyNeighbors", "appliedForwardPaths", "appliedProxyARP", "appliedAcceptLocal", "pendingProxyNeighbors", "pendingForwardPaths", "pendingProxyARP", "pendingAcceptLocal"} {
		var rows []any
		if !decodeAppliedDataplaneStatus(status[field], &rows) || rows == nil || len(rows) != 0 {
			t.Fatalf("%s was not retired: %#v", field, status[field])
		}
	}
}

func containsSAMSetting(settings []string, want string) bool {
	for _, setting := range settings {
		if setting == want {
			return true
		}
	}
	return false
}

func TestSAMJournalGARPIsRetriedUntilFinalCommit(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "native.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	capture := samJournalCapture()
	capture.GratuitousARP = true
	garp := &fakeSAMGARP{err: errors.New("GARP send failed")}
	faults := &samJournalFaultStore{SQLiteStore: store, failure: errors.New("final save failed")}
	controller := SAMController{Router: &api.Router{}, Store: faults, OS: platform.OSLinux, Applier: &fakeSAMApplier{}, GARP: garp, LocalCaptureIntents: []dynamicconfig.LocalCaptureIntent{capture}}
	if err := controller.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), garp.err.Error()) {
		t.Fatalf("GARP error = %v", err)
	}
	pending, err := samPendingProxyNeighbors(samJournalStatus(t, store)["pendingProxyNeighbors"], true)
	if err != nil || len(pending) != 1 {
		t.Fatalf("GARP failure lost pending acquisition: %+v %v", pending, err)
	}
	for _, row := range pending {
		if !row.ProxyEntryOwned || !row.GratuitousARP {
			t.Fatalf("GARP retry proof = %+v", row)
		}
	}
	garp.err = nil
	faults.failField = "appliedProxyNeighbors"
	if err := controller.Reconcile(t.Context()); !errors.Is(err, faults.failure) {
		t.Fatalf("post-GARP save error = %v", err)
	}
	faults.failField = ""
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(garp.calls) != 3 {
		t.Fatalf("GARP sends = %+v, want failed send plus both durable retries", garp.calls)
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(garp.calls) != 3 {
		t.Fatalf("committed GARP was sent again: %+v", garp.calls)
	}
}

func TestSAMJournalPendingSaveOrStatusReadFailurePreventsEffects(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "native.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	failure := errors.New("pending save rejected")
	applier := &fakeSAMApplier{}
	controller := SAMController{Router: &api.Router{}, Store: &samJournalFaultStore{SQLiteStore: store, failField: "pendingProxyNeighbors", failure: failure}, OS: platform.OSLinux, Applier: applier, LocalCaptureIntents: []dynamicconfig.LocalCaptureIntent{samJournalCapture()}}
	if err := controller.Reconcile(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("pending-save error = %v", err)
	}
	if len(applier.calls) != 0 || len(applier.acceptLocal) != 0 {
		t.Fatalf("unrecorded acquisition reached native effects: %+v", applier)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	controller.Store = store
	if err := controller.Reconcile(t.Context()); err == nil {
		t.Fatal("closed ownership store was treated as empty")
	}
	if len(applier.calls) != 0 || len(applier.acceptLocal) != 0 {
		t.Fatalf("failed ownership read reached native effects: %+v", applier)
	}
}

func TestSAMJournalAcquisitionCommitFailurePreventsSysctlWrite(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "native.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	failure := errors.New("sysctl acquisition commit failed")
	// The scope save succeeds. The next commit records the observed 0 -> 1
	// ownership immediately before the actual shared binary-setting write.
	faults := &samJournalFaultStore{SQLiteStore: store, failAtMerge: 2, failure: failure}
	applier := &samBinarySettingApplier{}
	controller := SAMController{Router: &api.Router{}, Store: faults, OS: platform.OSLinux, Applier: applier, LocalCaptureIntents: []dynamicconfig.LocalCaptureIntent{samJournalCapture()}}
	if err := controller.Reconcile(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("acquisition commit error = %v", err)
	}
	if applier.writes != 0 || len(applier.calls) != 0 || len(applier.acceptLocal) != 0 {
		t.Fatalf("uncommitted ownership reached native writes: %+v", applier)
	}
	pending, err := samPendingProxyNeighbors(samJournalStatus(t, store)["pendingProxyNeighbors"], true)
	if err != nil || len(pending) != 1 {
		t.Fatalf("scope must remain recoverable: %+v %v", pending, err)
	}
	for _, row := range pending {
		if row.ProxyEntryOwned {
			t.Fatal("failed pre-write commit fabricated neighbor ownership")
		}
	}
}

func TestSAMJournalUnownedPendingNeighborDoesNotAuthorizeDeletion(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "native.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	capture := samJournalCapture()
	row := samPendingProxyNeighbor{samAppliedProxyNeighbor: samAppliedProxyNeighbor{ID: capture.ID, PoolRef: capture.PoolRef, PoolPrefix: capture.PoolPrefix, Address: capture.Address, Interface: capture.CaptureInterface}}
	if err := store.MergeObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName, map[string]any{"pendingProxyNeighbors": []samPendingProxyNeighbor{row}}); err != nil {
		t.Fatal(err)
	}
	applier := &fakeSAMApplier{}
	controller := SAMController{Router: &api.Router{}, Store: store, OS: platform.OSLinux, Applier: applier, Withdrawals: []dynamicconfig.MobilityWithdrawalRequest{samJournalRequest()}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(applier.delete) != 0 {
		t.Fatalf("scope-only pending row deleted an unowned neighbor: %+v", applier.delete)
	}
	assertSAMJournalAcks(t, samJournalStatus(t, store), controller.Withdrawals)
}

func TestSAMJournalWithdrawalAckIsScopedToExactAddress(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "native.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	request := samJournalRequest()
	other := request
	other.ID, other.Address = "transfer-b", "10.88.60.11/32"
	controller := SAMController{Router: &api.Router{}, Store: store, OS: platform.OSLinux, Applier: &fakeSAMApplier{}, LocalCaptureIntents: []dynamicconfig.LocalCaptureIntent{samJournalCapture()}, Withdrawals: []dynamicconfig.MobilityWithdrawalRequest{request, other}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertSAMJournalAcks(t, samJournalStatus(t, store), []dynamicconfig.MobilityWithdrawalRequest{other})
}

func TestSAMOwnedBinarySettingRecordsBeforeWritingAndRejectsForeign(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		calls := []string{}
		_, err := applySAMOwnedBinarySetting("proxy_arp", enabled, false, func() (string, error) { return "1", nil }, func(string) error { calls = append(calls, "write"); return nil }, func() error { calls = append(calls, "record"); return nil })
		if err == nil || !strings.Contains(err.Error(), "foreign") || len(calls) != 0 {
			t.Fatalf("foreign setting enabled=%t: calls=%+v err=%v", enabled, calls, err)
		}
	}
	failure := errors.New("ownership persistence failed")
	writeCalled := false
	_, err := applySAMOwnedBinarySetting("accept_local", true, false, func() (string, error) { return "0", nil }, func(string) error { writeCalled = true; return nil }, func() error { return failure })
	if !errors.Is(err, failure) || writeCalled {
		t.Fatalf("failed ownership commit permitted write: write=%t err=%v", writeCalled, err)
	}
	calls := []string{}
	result, err := applySAMOwnedBinarySetting("accept_local", true, false, func() (string, error) { calls = append(calls, "read"); return "0", nil }, func(value string) error { calls = append(calls, "write="+value); return nil }, func() error { calls = append(calls, "record"); return nil })
	if err != nil || !result.changedByRouterd || !reflect.DeepEqual(calls, []string{"read", "record", "write=1"}) {
		t.Fatalf("ownership write ordering: calls=%+v result=%+v err=%v", calls, result, err)
	}
}
