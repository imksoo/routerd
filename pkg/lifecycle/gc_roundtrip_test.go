// SPDX-License-Identifier: BSD-3-Clause

package lifecycle

import (
	"reflect"
	"sort"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resource"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type gcRoundTripCase struct {
	name      string
	features  platform.Features
	artifacts []resource.Artifact
	statuses  []routerstate.ObjectStatus
}

type fakeGCRoundTripStore struct {
	ledger   map[string]resource.Artifact
	host     map[string]resource.Artifact
	statuses map[string]routerstate.ObjectStatus
	ops      []string
}

func newFakeGCRoundTripStore() *fakeGCRoundTripStore {
	return &fakeGCRoundTripStore{
		ledger:   map[string]resource.Artifact{},
		host:     map[string]resource.Artifact{},
		statuses: map[string]routerstate.ObjectStatus{},
	}
}

func (s *fakeGCRoundTripStore) apply(artifacts []resource.Artifact, statuses []routerstate.ObjectStatus) {
	for _, artifact := range artifacts {
		s.ledger[artifact.Identity()] = artifact
		s.host[artifact.Identity()] = artifact
	}
	for _, status := range statuses {
		s.statuses[ObjectStatusID(status)] = status
	}
}

func (s *fakeGCRoundTripStore) plan(desiredArtifacts []resource.Artifact, desiredStatusIDs map[string]bool) GCPlan {
	return PlanGC(GCPlanInput{
		DesiredArtifacts:       desiredArtifacts,
		LedgerArtifacts:        s.ledgerArtifacts(),
		HostArtifacts:          s.hostArtifacts(),
		ObjectStatuses:         s.objectStatuses(),
		DesiredObjectStatusIDs: desiredStatusIDs,
	})
}

func (s *fakeGCRoundTripStore) execute(plan GCPlan, exec ArtifactTeardownExecutor, dryRun bool) error {
	cleaned := map[string]bool{}
	for _, action := range plan.Actions {
		switch action.Type {
		case GCActionBackupState:
			s.ops = append(s.ops, "backup")
		case GCActionRemoveArtifact:
			label, err := CleanupArtifact(exec, action.Artifact)
			if err != nil {
				return err
			}
			if label == "" {
				continue
			}
			s.ops = append(s.ops, "remove:"+label)
			cleaned[action.Artifact.Identity()] = true
			if !dryRun {
				delete(s.host, action.Artifact.Identity())
			}
		case GCActionForgetLedger:
			if !cleaned[action.Artifact.Identity()] {
				continue
			}
			s.ops = append(s.ops, "forget:"+LabelForArtifact(action.Artifact))
			if !dryRun {
				delete(s.ledger, action.Artifact.Identity())
			}
		case GCActionTeardownResource:
			s.ops = append(s.ops, "teardown-resource:"+ObjectStatusID(action.Status))
			if !dryRun {
				delete(s.statuses, ObjectStatusID(action.Status))
			}
		case GCActionDeleteStatus:
			s.ops = append(s.ops, "delete-status:"+ObjectStatusID(action.Status))
			if !dryRun {
				delete(s.statuses, ObjectStatusID(action.Status))
			}
		case GCActionRecordEvent:
			s.ops = append(s.ops, "event:"+action.Reason)
		}
	}
	return nil
}

func (s *fakeGCRoundTripStore) ledgerArtifacts() []resource.Artifact {
	return artifactMapValues(s.ledger)
}

func (s *fakeGCRoundTripStore) hostArtifacts() []resource.Artifact {
	return artifactMapValues(s.host)
}

func (s *fakeGCRoundTripStore) objectStatuses() []routerstate.ObjectStatus {
	out := make([]routerstate.ObjectStatus, 0, len(s.statuses))
	for _, status := range s.statuses {
		out = append(out, status)
	}
	sort.Slice(out, func(i, j int) bool { return ObjectStatusID(out[i]) < ObjectStatusID(out[j]) })
	return out
}

func artifactMapValues(values map[string]resource.Artifact) []resource.Artifact {
	out := make([]resource.Artifact, 0, len(values))
	for _, artifact := range values {
		out = append(out, artifact)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity() < out[j].Identity() })
	return out
}

func TestGCRoundTripApplyDeleteApplyLeavesNoResidue(t *testing.T) {
	for _, tc := range gcRoundTripCases() {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeGCRoundTripStore()
			store.apply(tc.artifacts, tc.statuses)
			plan := store.plan(nil, nil)
			if len(plan.Actions) == 0 {
				t.Fatal("GC plan is empty after deleting resource from desired config")
			}
			if err := store.execute(plan, &fakeArtifactTeardownExecutor{features: tc.features}, false); err != nil {
				t.Fatalf("execute GC plan: %v", err)
			}
			if len(store.ledger) != 0 || len(store.host) != 0 || len(store.statuses) != 0 {
				t.Fatalf("residue ledger=%+v host=%+v statuses=%+v", store.ledgerArtifacts(), store.hostArtifacts(), store.objectStatuses())
			}
		})
	}
}

func TestGCRoundTripDryRunProducesPlanWithoutMutation(t *testing.T) {
	tc := gcRoundTripCaseByName(t, "PPPoE #163 #176 generated service/file/runtime artifacts")
	store := newFakeGCRoundTripStore()
	store.apply(tc.artifacts, tc.statuses)
	beforeLedger := store.ledgerArtifacts()
	beforeHost := store.hostArtifacts()
	beforeStatuses := store.objectStatuses()
	plan := store.plan(nil, nil)
	if len(plan.Actions) == 0 {
		t.Fatal("GC plan is empty")
	}
	if err := store.execute(plan, &fakeArtifactTeardownExecutor{features: tc.features}, true); err != nil {
		t.Fatalf("dry-run execute GC plan: %v", err)
	}
	if !reflect.DeepEqual(store.ledgerArtifacts(), beforeLedger) || !reflect.DeepEqual(store.hostArtifacts(), beforeHost) || !reflect.DeepEqual(store.objectStatuses(), beforeStatuses) {
		t.Fatalf("dry-run mutated state ledger=%+v host=%+v statuses=%+v", store.ledgerArtifacts(), store.hostArtifacts(), store.objectStatuses())
	}
}

func TestGCRoundTripIssue212213TeardownIsIdempotent(t *testing.T) {
	tc := gcRoundTripCaseByName(t, "DSLite tunnel and stale inner IPv4 alias")
	store := newFakeGCRoundTripStore()
	store.apply(tc.artifacts, tc.statuses)
	plan := store.plan(nil, nil)
	exec := &fakeArtifactTeardownExecutor{features: tc.features}
	if err := store.execute(plan, exec, false); err != nil {
		t.Fatalf("first execute GC plan: %v", err)
	}
	if err := store.execute(plan, exec, false); err != nil {
		t.Fatalf("second execute GC plan: %v", err)
	}
	if len(store.ledger) != 0 || len(store.host) != 0 || len(store.statuses) != 0 {
		t.Fatalf("residue after idempotent teardown ledger=%+v host=%+v statuses=%+v", store.ledgerArtifacts(), store.hostArtifacts(), store.objectStatuses())
	}
}

func TestGCRoundTripUnsupportedOSDoesNotDestruct(t *testing.T) {
	owner := api.NetAPIVersion + "/DSLiteTunnel/unsupported"
	artifact := resource.Artifact{Kind: "linux.ipip6.tunnel", Name: "ds-unsupported", Owner: owner}
	store := newFakeGCRoundTripStore()
	store.apply([]resource.Artifact{artifact}, nil)
	plan := store.plan(nil, nil)
	exec := &fakeArtifactTeardownExecutor{}
	if err := store.execute(plan, exec, false); err != nil {
		t.Fatalf("execute unsupported GC plan: %v", err)
	}
	if len(exec.commands) != 0 || len(exec.removes) != 0 || len(exec.removeAlls) != 0 || len(exec.fwmarkRules) != 0 || len(exec.routeTables) != 0 {
		t.Fatalf("unsupported OS performed destructive operation: %+v %+v %+v %+v %+v", exec.commands, exec.removes, exec.removeAlls, exec.fwmarkRules, exec.routeTables)
	}
	if len(store.ledger) != 1 || len(store.host) != 1 {
		t.Fatalf("unsupported OS should keep artifacts ledger=%+v host=%+v", store.ledgerArtifacts(), store.hostArtifacts())
	}
}

func TestGCRoundTripIssue189BacksUpBeforeStatusDelete(t *testing.T) {
	tc := gcRoundTripCaseByName(t, "WireGuardInterface+Peer status cleanup")
	store := newFakeGCRoundTripStore()
	store.apply(tc.artifacts, tc.statuses)
	plan := store.plan(nil, nil)
	if !plan.BackupRequired {
		t.Fatal("BackupRequired = false, want true")
	}
	if err := store.execute(plan, &fakeArtifactTeardownExecutor{features: tc.features}, false); err != nil {
		t.Fatalf("execute GC plan: %v", err)
	}
	wantPrefix := []string{
		"backup",
		"teardown-resource:" + api.NetAPIVersion + "/WireGuardInterface/wg-old",
		"teardown-resource:" + api.NetAPIVersion + "/WireGuardPeer/peer-old",
		"event:ResourceTeardownCleanup",
	}
	if !reflect.DeepEqual(store.ops, wantPrefix) {
		t.Fatalf("ops = %#v, want %#v", store.ops, wantPrefix)
	}
}

func TestGCRoundTripResourceTeardownSkipsAdoptedExternal(t *testing.T) {
	store := newFakeGCRoundTripStore()
	store.apply(nil, []routerstate.ObjectStatus{
		{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface", Name: "wg-adopted", Management: "adopted", Status: map[string]any{"managedBy": "routerd"}},
		{APIVersion: api.NetAPIVersion, Kind: "WireGuardPeer", Name: "peer-external", ManagedBy: "external", Status: map[string]any{"interface": "wg-adopted"}},
	})
	plan := store.plan(nil, nil)
	if len(plan.Actions) != 0 {
		t.Fatalf("adopted/external statuses planned for teardown: %+v", plan.Actions)
	}
}

func gcRoundTripCaseByName(t *testing.T, name string) gcRoundTripCase {
	t.Helper()
	for _, tc := range gcRoundTripCases() {
		if tc.name == name {
			return tc
		}
	}
	t.Fatalf("missing round-trip case %q", name)
	return gcRoundTripCase{}
}

func gcRoundTripCases() []gcRoundTripCase {
	return []gcRoundTripCase{
		{
			name:     "TunnelInterface generated IPIP artifact",
			features: platform.Features{HasIproute2: true},
			artifacts: []resource.Artifact{
				{Kind: "linux.ipip6.tunnel", Name: "sam-edge", Owner: api.HybridAPIVersion + "/TunnelInterface/sam-edge"},
			},
			statuses: []routerstate.ObjectStatus{
				{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface", Name: "sam-edge", Status: map[string]any{"phase": "Applied"}},
			},
		},
		{
			name:     "IPv4StaticAddress stale DSLite source alias",
			features: platform.Features{HasIproute2: true},
			artifacts: []resource.Artifact{
				{Kind: "net.ipv4.address", Name: "ds-routerd:192.168.160.249/32", Owner: api.NetAPIVersion + "/IPv4StaticAddress/ds-lite-source"},
			},
			statuses: []routerstate.ObjectStatus{
				{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress", Name: "ds-lite-source", Status: map[string]any{"phase": "Applied"}},
			},
		},
		{
			name:     "IPv4Route #185 route table and policy artifacts",
			features: platform.Features{HasIproute2: true},
			artifacts: []resource.Artifact{
				{Kind: "linux.ipv4.fwmarkRule", Name: "priority=100,mark=0x100,table=100", Owner: api.NetAPIVersion + "/IPv4Route/old-route", Attributes: map[string]string{"priority": "100", "mark": "0x100", "table": "100"}},
				{Kind: "linux.ipv4.routeTable", Name: "table=100", Owner: api.NetAPIVersion + "/IPv4Route/old-route", Attributes: map[string]string{"table": "100"}},
			},
			statuses: []routerstate.ObjectStatus{
				{APIVersion: api.NetAPIVersion, Kind: "IPv4Route", Name: "old-route", Status: map[string]any{"phase": "Applied"}},
			},
		},
		{
			name:     "EgressRoutePolicy policy route artifacts",
			features: platform.Features{HasIproute2: true, HasNftables: true},
			artifacts: []resource.Artifact{
				{Kind: "linux.ipv4.fwmarkRule", Name: "priority=101,mark=0x101,table=101", Owner: api.NetAPIVersion + "/EgressRoutePolicy/wan", Attributes: map[string]string{"priority": "101", "mark": "0x101", "table": "101"}},
				{Kind: "linux.ipv4.routeTable", Name: "table=101", Owner: api.NetAPIVersion + "/EgressRoutePolicy/wan", Attributes: map[string]string{"table": "101"}},
				{Kind: "nft.table", Name: "ip/routerd_policy", Owner: api.NetAPIVersion + "/EgressRoutePolicy/wan", Attributes: map[string]string{"family": "ip", "name": "routerd_policy"}},
			},
			statuses: []routerstate.ObjectStatus{
				{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy", Name: "wan", Status: map[string]any{"phase": "Applied"}},
			},
		},
		{
			name:     "NAT44 nft table artifacts",
			features: platform.Features{HasNftables: true},
			artifacts: []resource.Artifact{
				{Kind: "nft.table", Name: "ip/routerd_nat", Owner: api.NetAPIVersion + "/NAT44Rule/lan", Attributes: map[string]string{"family": "ip", "name": "routerd_nat"}},
				{Kind: "nft.table", Name: "ip6/routerd_nat", Owner: api.NetAPIVersion + "/NAT44Rule/lan", Attributes: map[string]string{"family": "ip6", "name": "routerd_nat"}},
			},
			statuses: []routerstate.ObjectStatus{
				{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule", Name: "lan", Status: map[string]any{"phase": "Active"}},
			},
		},
		{
			name:     "SystemdService routerd unit artifact",
			features: platform.Features{HasSystemd: true},
			artifacts: []resource.Artifact{
				{Kind: "systemd.service", Name: "routerd-old.service", Owner: api.SystemAPIVersion + "/ServiceUnit/routerd-old"},
			},
			statuses: []routerstate.ObjectStatus{
				{APIVersion: api.SystemAPIVersion, Kind: "ServiceUnit", Name: "routerd-old", Status: map[string]any{"phase": "Applied"}},
			},
		},
		{
			name:     "PPPoE #163 #176 generated service/file/runtime artifacts",
			features: platform.Features{HasSystemd: true},
			artifacts: []resource.Artifact{
				{Kind: "systemd.service", Name: "routerd-pppoe-pppoe-flets.service", Owner: api.NetAPIVersion + "/PPPoESession/pppoe-flets"},
				{Kind: "file", Name: "/etc/ppp/peers/routerd-pppoe-flets", Owner: api.NetAPIVersion + "/PPPoESession/pppoe-flets"},
				{Kind: "unix.socket", Name: "/run/routerd/pppoe-client/pppoe-flets.sock", Owner: api.NetAPIVersion + "/PPPoESession/pppoe-flets"},
				{Kind: "directory", Name: "/run/routerd/pppoe-client/pppoe-flets", Owner: api.NetAPIVersion + "/PPPoESession/pppoe-flets"},
				{Kind: "directory", Name: "/var/lib/routerd/pppoe-client/pppoe-flets", Owner: api.NetAPIVersion + "/PPPoESession/pppoe-flets"},
			},
			statuses: []routerstate.ObjectStatus{
				{APIVersion: api.NetAPIVersion, Kind: "PPPoESession", Name: "pppoe-flets", Status: map[string]any{"phase": "Applied"}},
			},
		},
		{
			name:     "DSLite tunnel and stale inner IPv4 alias",
			features: platform.Features{HasIproute2: true},
			artifacts: []resource.Artifact{
				{Kind: "linux.ipip6.tunnel", Name: "ds-routerd", Owner: api.NetAPIVersion + "/DSLiteTunnel/ds-lite"},
				{Kind: "net.ipv4.address", Name: "ds-routerd:192.168.160.250/32", Owner: api.NetAPIVersion + "/DSLiteTunnel/ds-lite", Attributes: map[string]string{"peer": "192.0.0.1/32"}},
			},
			statuses: []routerstate.ObjectStatus{
				{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel", Name: "ds-lite", Status: map[string]any{"phase": "Applied"}},
			},
		},
		{
			name: "WireGuardInterface+Peer status cleanup",
			statuses: []routerstate.ObjectStatus{
				{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface", Name: "wg-old", Status: map[string]any{"phase": "Up", "managedBy": "routerd"}},
				{APIVersion: api.NetAPIVersion, Kind: "WireGuardPeer", Name: "peer-old", Status: map[string]any{"phase": "Connected", "interface": "wg-old", "managedBy": "routerd"}},
			},
		},
		{
			name: "RemoteAddressClaim SAM capture status cleanup",
			statuses: []routerstate.ObjectStatus{
				{APIVersion: api.HybridAPIVersion, Kind: "RemoteAddressClaim", Name: "client-a", Status: map[string]any{"phase": "Captured", "captureProxyNeighbor": map[string]any{"address": "10.77.60.9/32", "interface": "lan"}}},
			},
		},
		{
			name: "SAM proxy ARP sysctl status cleanup",
			statuses: []routerstate.ObjectStatus{
				{APIVersion: api.SystemAPIVersion, Kind: "Sysctl", Name: "sam-proxy-arp-lan", Status: map[string]any{"key": "net.ipv4.conf.lan.proxy_arp", "previousValue": "0", "changed": true}},
			},
		},
		{
			name:     "SAMTransportProfile derived resource chain",
			features: platform.Features{HasIproute2: true},
			artifacts: []resource.Artifact{
				{Kind: "linux.ipip6.tunnel", Name: "sam-core-a", Owner: api.HybridAPIVersion + "/TunnelInterface/sam-core-a"},
			},
			statuses: []routerstate.ObjectStatus{
				{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile", Name: "fabric", Status: map[string]any{"phase": "Applied"}},
				{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface", Name: "sam-core-a", Status: map[string]any{"phase": "Applied", "owner": api.MobilityAPIVersion + "/SAMTransportProfile/fabric"}},
			},
		},
	}
}
