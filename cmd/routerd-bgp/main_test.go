// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	gobgpapi "github.com/osrg/gobgp/v4/api"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
)

type fakePathServer struct {
	policyOwner       *localPathServer
	added             []*gobgpapi.AddPathRequest
	deleted           []*gobgpapi.DeletePathRequest
	policyRequests    []*gobgpapi.SetPoliciesRequest
	policyAssignments []*gobgpapi.PolicyAssignment
	resetRequests     []*gobgpapi.ResetPeerRequest
	nextID            byte
	deleteErr         error
	addStarted        chan struct{}
	releaseAdd        <-chan struct{}
	observed          []bgpdaemon.ObservedPath
	observedErr       error
}

func newFakePathServer(t *testing.T) *fakePathServer {
	t.Helper()
	return &fakePathServer{policyOwner: realDynamicPathServer(t)}
}

func (s *fakePathServer) AddPolicy(ctx context.Context, req *gobgpapi.AddPolicyRequest) error {
	if s.policyOwner == nil {
		return fmt.Errorf("test native policy owner is unavailable")
	}
	return s.policyOwner.AddPolicy(ctx, req)
}

func (s *fakePathServer) ListPolicy(ctx context.Context, req *gobgpapi.ListPolicyRequest, fn func(*gobgpapi.Policy)) error {
	if s.policyOwner == nil {
		return fmt.Errorf("test native policy owner is unavailable")
	}
	return s.policyOwner.ListPolicy(ctx, req, fn)
}

func (s *fakePathServer) ListDefinedSet(ctx context.Context, req *gobgpapi.ListDefinedSetRequest, fn func(*gobgpapi.DefinedSet)) error {
	if s.policyOwner == nil {
		return fmt.Errorf("test native policy owner is unavailable")
	}
	return s.policyOwner.ListDefinedSet(ctx, req, fn)
}

func (s *fakePathServer) ListPolicyAssignment(ctx context.Context, req *gobgpapi.ListPolicyAssignmentRequest, fn func(*gobgpapi.PolicyAssignment)) error {
	if s.policyOwner == nil {
		return fmt.Errorf("test native policy owner is unavailable")
	}
	return s.policyOwner.ListPolicyAssignment(ctx, req, fn)
}

func (s *fakePathServer) ListPeer(ctx context.Context, req *gobgpapi.ListPeerRequest, fn func(*gobgpapi.Peer)) error {
	if s.policyOwner == nil {
		return fmt.Errorf("test native policy owner is unavailable")
	}
	return s.policyOwner.ListPeer(ctx, req, fn)
}

type blockingControlShutdowner struct {
	closed chan struct{}
}

func (s *blockingControlShutdowner) Shutdown(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (s *blockingControlShutdowner) Close() error {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return nil
}

func TestStopBGPDaemonBoundsBlockingControlAndGoBGP(t *testing.T) {
	control := &blockingControlShutdowner{closed: make(chan struct{})}
	stopStarted := make(chan struct{})
	releaseStop := make(chan struct{})
	stopDone := make(chan struct{})
	start := time.Now()
	acknowledged := stopBGPDaemon(control, func() {
		close(stopStarted)
		<-releaseStop
		close(stopDone)
	}, nil, 15*time.Millisecond, 15*time.Millisecond)
	if acknowledged {
		t.Fatal("blocked native stop acknowledged process ownership release")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("bounded shutdown took %s, want < 1s", elapsed)
	}
	select {
	case <-control.closed:
	default:
		t.Fatal("control server was not force-closed after its drain deadline")
	}
	select {
	case <-stopStarted:
	default:
		t.Fatal("GoBGP stop was not started")
	}
	close(releaseStop)
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("blocked GoBGP stop did not finish after release")
	}
}

func TestStopBGPDaemonAcknowledgesCompletedStop(t *testing.T) {
	stopped := false
	if !stopBGPDaemon(nil, func() { stopped = true }, nil, time.Second, time.Second) || !stopped {
		t.Fatal("completed native stop was not acknowledged")
	}
}

func (s *fakePathServer) AddPath(_ context.Context, req *gobgpapi.AddPathRequest) (*gobgpapi.AddPathResponse, error) {
	if s.addStarted != nil {
		select {
		case s.addStarted <- struct{}{}:
		default:
		}
	}
	if s.releaseAdd != nil {
		<-s.releaseAdd
	}
	s.nextID++
	uuid := []byte{s.nextID}
	s.added = append(s.added, req)
	return &gobgpapi.AddPathResponse{Uuid: uuid}, nil
}

func (s *fakePathServer) DeletePath(_ context.Context, req *gobgpapi.DeletePathRequest) error {
	s.deleted = append(s.deleted, req)
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return nil
}

func (s *fakePathServer) SetPolicies(ctx context.Context, req *gobgpapi.SetPoliciesRequest) error {
	s.policyRequests = append(s.policyRequests, req)
	if s.policyOwner == nil {
		return fmt.Errorf("test native policy owner is unavailable")
	}
	return s.policyOwner.SetPolicies(ctx, req)
}

func (s *fakePathServer) SetPolicyAssignment(ctx context.Context, req *gobgpapi.SetPolicyAssignmentRequest) error {
	s.policyAssignments = append(s.policyAssignments, req.GetAssignment())
	if s.policyOwner == nil {
		return fmt.Errorf("test native policy owner is unavailable")
	}
	return s.policyOwner.SetPolicyAssignment(ctx, req)
}

func (s *fakePathServer) ResetPeer(ctx context.Context, req *gobgpapi.ResetPeerRequest) error {
	s.resetRequests = append(s.resetRequests, req)
	if s.policyOwner == nil {
		return fmt.Errorf("test native policy owner is unavailable")
	}
	return s.policyOwner.ResetPeer(ctx, req)
}

func (s *fakePathServer) ListObservedPaths(context.Context) ([]bgpdaemon.ObservedPath, error) {
	if s.observedErr != nil {
		return nil, s.observedErr
	}
	return append([]bgpdaemon.ObservedPath(nil), s.observed...), nil
}

func TestAppliedPoliciesRestoreNeighborScopedGlobalImportPolicy(t *testing.T) {
	peer := bgpdaemon.AppliedPeer{
		Address: "192.168.1.38",
	}
	plan := bgpdaemon.BuildPolicyPlan(bgpdaemon.PolicyIntent{RouterName: "restore", Scopes: []bgpdaemon.PolicyScope{{
		Name: "routerd-restore-import-effective-peer-192-168-1-38", Neighbors: []string{"192.168.1.38/32"}, ExportPolicyName: "peer-export",
		Spec: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/24"}, NextHopRewrite: "peer-address"},
	}}}, nil)
	req, assignment := plan.SetPolicies, plan.GlobalImportAssignment
	if len(req.GetPolicies()) != 3 || len(req.GetDefinedSets()) != 2 {
		t.Fatalf("restore policies = policies:%d definedSets:%d, want guard/export/global policies and neighbor/prefix sets", len(req.GetPolicies()), len(req.GetDefinedSets()))
	}
	policy := policyRequestNamed(t, req, "routerd-restore-import-effective")
	if policy.GetName() != "routerd-restore-import-effective" {
		t.Fatalf("policy name = %q, want effective global import policy", policy.GetName())
	}
	action := policy.GetStatements()[0].GetActions().GetNexthop()
	if !action.GetPeerAddress() {
		t.Fatalf("next-hop action = %#v, want peer-address rewrite", action)
	}
	if assignment.GetName() != "global" || assignment.GetDirection() != gobgpapi.PolicyDirection_POLICY_DIRECTION_IMPORT ||
		assignment.GetDefaultAction() != gobgpapi.RouteAction_ROUTE_ACTION_REJECT || len(assignment.GetPolicies()) != 1 ||
		assignment.GetPolicies()[0].GetName() != "routerd-restore-import-effective" {
		t.Fatalf("global import policy assignment = %#v, want effective policy", assignment)
	}
	restoredPeer := appliedPeer(peer, bgpdaemon.AppliedGlobal{ASN: 64512})
	if restoredPeer.GetApplyPolicy().GetImportPolicy() != nil {
		t.Fatalf("restored peer import policy = %#v, want no ineffective peer assignment", restoredPeer.GetApplyPolicy().GetImportPolicy())
	}
}

func TestAppliedPeerRestoresPassiveMode(t *testing.T) {
	peer := appliedPeer(
		bgpdaemon.AppliedPeer{Address: "192.0.2.2", ASN: 64512, PassiveMode: true},
		bgpdaemon.AppliedGlobal{ASN: 64512},
	)
	if !peer.GetTransport().GetPassiveMode() {
		t.Fatal("restored peer passive mode = false, want true")
	}
}

func TestAppliedPeerRestoresDefaultActiveMode(t *testing.T) {
	peer := appliedPeer(
		bgpdaemon.AppliedPeer{Address: "192.0.2.2", ASN: 64512},
		bgpdaemon.AppliedGlobal{ASN: 64512},
	)
	if peer.Transport != nil {
		t.Fatalf("restored default active peer transport = %#v, want normal active-mode default", peer.Transport)
	}
}

func TestAppliedPoliciesRestorePeerImportPolicyWithCommunities(t *testing.T) {
	scopeName := "routerd-restore-import-effective-peer-10-99-0-2"
	plan := bgpdaemon.BuildPolicyPlan(bgpdaemon.PolicyIntent{RouterName: "restore", Scopes: []bgpdaemon.PolicyScope{{
		Name: scopeName, Neighbors: []string{"10.99.0.2/32"}, ExportPolicyName: "peer-export",
		Spec: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}, RequiredCommunities: []string{"64512:301"}, ForbiddenCommunities: []string{"64512:302"}},
	}}}, nil)
	req := plan.SetPolicies
	if !appliedPolicyRequestHasDefinedSet(req, gobgpapi.DefinedType_DEFINED_TYPE_NEIGHBOR, scopeName+"-neighbors", "10.99.0.2/32") {
		t.Fatalf("defined sets = %#v, want neighbor set", req.GetDefinedSets())
	}
	if !appliedPolicyRequestHasDefinedSet(req, gobgpapi.DefinedType_DEFINED_TYPE_COMMUNITY, scopeName+"-required-communities", "^64512:301$") {
		t.Fatalf("defined sets = %#v, want required community set", req.GetDefinedSets())
	}
	if !appliedPolicyRequestHasDefinedSet(req, gobgpapi.DefinedType_DEFINED_TYPE_COMMUNITY, scopeName+"-forbidden-communities", "^64512:302$") {
		t.Fatalf("defined sets = %#v, want forbidden community set", req.GetDefinedSets())
	}
	if len(req.GetPolicies()) != 3 || len(policyRequestNamed(t, req, "routerd-restore-import-effective").GetStatements()) != 4 {
		t.Fatalf("policies = %#v, want guard and scoped reject/allow/reject plus explicit local allow", req.GetPolicies())
	}
	reject := policyRequestNamed(t, req, "routerd-restore-import-effective").GetStatements()[0]
	if reject.GetActions().GetRouteAction() != gobgpapi.RouteAction_ROUTE_ACTION_REJECT ||
		reject.GetConditions().GetCommunitySet().GetType() != gobgpapi.MatchSet_TYPE_ANY {
		t.Fatalf("reject statement = %#v, want forbidden community reject", reject)
	}
	allow := policyRequestNamed(t, req, "routerd-restore-import-effective").GetStatements()[1]
	if allow.GetActions().GetRouteAction() != gobgpapi.RouteAction_ROUTE_ACTION_ACCEPT ||
		allow.GetConditions().GetNeighborSet().GetName() != scopeName+"-neighbors" ||
		allow.GetConditions().GetPrefixSet().GetName() == "" ||
		allow.GetConditions().GetCommunitySet().GetType() != gobgpapi.MatchSet_TYPE_ALL {
		t.Fatalf("allow statement = %#v, want prefix and required-community accept", allow)
	}
}

func TestAppliedPoliciesPreservePrefixlessPeerImportPreference(t *testing.T) {
	plan := bgpdaemon.BuildPolicyPlan(bgpdaemon.PolicyIntent{RouterName: "restore", Scopes: []bgpdaemon.PolicyScope{{
		Name: "peer-import", Neighbors: []string{"10.99.0.2/32"}, ExportPolicyName: "peer-export",
		Spec: api.BGPImportPolicySpec{NextHopRewrite: "peer-address", LocalPreference: 200},
	}}}, nil)
	req := plan.SetPolicies
	if len(req.GetPolicies()) != 3 || len(policyRequestNamed(t, req, "routerd-restore-import-effective").GetStatements()) != 3 {
		t.Fatalf("restore policies = %#v, want prefixless peer policy with allow and terminal reject", req)
	}
	allow := policyRequestNamed(t, req, "routerd-restore-import-effective").GetStatements()[0]
	if allow.GetConditions().GetPrefixSet() != nil ||
		allow.GetActions().GetLocalPref().GetValue() != 200 ||
		!allow.GetActions().GetNexthop().GetPeerAddress() {
		t.Fatalf("prefixless peer rule = %#v, want direct peer preference 200", allow)
	}
}

func TestAppliedPoliciesRestoreRejectsRoutesFromEmptyOwnershipDirectPeer(t *testing.T) {
	plan := bgpdaemon.BuildPolicyPlan(bgpdaemon.PolicyIntent{RouterName: "restore", Scopes: []bgpdaemon.PolicyScope{{
		Name: "routerd-restore-import-effective-peer-10-99-0-3", Neighbors: []string{"10.99.0.3/32"}, ExportPolicyName: "peer-export",
		PreserveImportPrefixes: true, RejectImportAll: true,
		Spec: api.BGPImportPolicySpec{NextHopRewrite: "peer-address", LocalPreference: 200},
	}}}, nil)
	req, assignment := plan.SetPolicies, plan.GlobalImportAssignment
	if len(req.GetPolicies()) != 3 || len(req.GetDefinedSets()) != 1 || len(policyRequestNamed(t, req, "routerd-restore-import-effective").GetStatements()) != 2 {
		t.Fatalf("restored empty-ownership direct policy = %#v, want scoped reject plus explicit local allow", req)
	}
	statement := policyRequestNamed(t, req, "routerd-restore-import-effective").GetStatements()[0]
	if statement.GetName() != "routerd-restore-import-effective-peer-10-99-0-3-reject-all-import" ||
		statement.GetActions().GetRouteAction() != gobgpapi.RouteAction_ROUTE_ACTION_REJECT ||
		statement.GetConditions().GetNeighborSet().GetName() != "routerd-restore-import-effective-peer-10-99-0-3-neighbors" {
		t.Fatalf("restored empty-ownership direct statement = %#v", statement)
	}
	if assignment.GetDefaultAction() != gobgpapi.RouteAction_ROUTE_ACTION_REJECT {
		t.Fatalf("global assignment = %#v, want unknown peers rejected", assignment)
	}
	local := policyRequestNamed(t, req, "routerd-restore-import-effective").GetStatements()[1]
	if local.GetConditions().GetRouteType() != gobgpapi.Conditions_ROUTE_TYPE_LOCAL || local.GetActions().GetRouteAction() != gobgpapi.RouteAction_ROUTE_ACTION_ACCEPT {
		t.Fatalf("local route acceptance must be explicit: %#v", local)
	}
}

func TestAppliedPoliciesRestorePeerExportPolicy(t *testing.T) {
	peer := bgpdaemon.AppliedPeer{
		Address: "10.252.0.18",
		ASN:     64512,
	}
	plan := bgpdaemon.BuildPolicyPlan(bgpdaemon.PolicyIntent{RouterName: "restore", Scopes: []bgpdaemon.PolicyScope{{
		Name: "routerd-restore-import-effective-peer-10-252-0-18", Neighbors: []string{"10.252.0.18/32"},
		Spec:             api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.252.0.0/24"}},
		ExportPolicyName: "routerd-lan-export-10-252-0-2", ExportPolicy: api.BGPExportPolicySpec{AllowedPrefixes: []string{"192.168.123.129/32"}},
	}}}, []bgpdaemon.AppliedPath{{
		Source: "MobilityPool/svnet1/node/pve-rt08",
		Prefix: "192.168.123.132/32",
	}})
	req, assignment := plan.SetPolicies, plan.GlobalImportAssignment
	if !appliedPolicyRequestHasStatement(req, "routerd-restore-import-effective", "routerd-restore-import-effective-peer-10-252-0-18-allow-import") {
		t.Fatalf("restore policies = %#v, want import policy", req)
	}
	if !appliedPolicyRequestHasStatement(req, "routerd-lan-export-10-252-0-2", "routerd-lan-export-10-252-0-2-allow-export") {
		t.Fatalf("restore policies = %#v, want peer export policy", req)
	}
	if len(assignment.GetPolicies()) != 1 || assignment.GetPolicies()[0].GetName() != "routerd-restore-import-effective" {
		t.Fatalf("global import assignment = %#v, want effective import policy", assignment)
	}
	if !appliedPolicyRequestHasPrefix(req, "routerd-restore-import-effective-peer-10-252-0-18-prefixes", "192.168.123.132/32") {
		t.Fatalf("restore policies = %#v, want dynamic mobility prefix in import policy", req)
	}
	if !appliedPolicyRequestHasPrefix(req, "routerd-lan-export-10-252-0-2-prefixes", "192.168.123.132/32") {
		t.Fatalf("restore policies = %#v, want dynamic mobility prefix in export policy", req)
	}
	restoredPeer := appliedPeer(peer, bgpdaemon.AppliedGlobal{ASN: 64512})
	if restoredPeer.GetApplyPolicy().GetImportPolicy() != nil {
		t.Fatalf("restored peer import policy = %#v, want no ineffective import assignment", restoredPeer.GetApplyPolicy().GetImportPolicy())
	}
	if restoredPeer.GetApplyPolicy().GetExportPolicy() != nil {
		t.Fatal("restored peer attached ineffective export policy")
	}
	exportAssignment := plan.GlobalExportAssignment
	if exportAssignment.GetDirection() != gobgpapi.PolicyDirection_POLICY_DIRECTION_EXPORT ||
		exportAssignment.GetDefaultAction() != gobgpapi.RouteAction_ROUTE_ACTION_REJECT ||
		len(exportAssignment.GetPolicies()) != 1 ||
		exportAssignment.GetPolicies()[0].GetName() != "routerd-lan-export-10-252-0-2" {
		t.Fatalf("restored peer export policy = %#v, want export assignment", exportAssignment)
	}
}

func policyRequestNamed(t *testing.T, req *gobgpapi.SetPoliciesRequest, name string) *gobgpapi.Policy {
	t.Helper()
	for _, policy := range req.GetPolicies() {
		if policy.GetName() == name {
			return policy
		}
	}
	t.Fatalf("missing named policy %q in %#v", name, req)
	return nil
}

func appliedPolicyRequestHasPrefix(req *gobgpapi.SetPoliciesRequest, setName, prefix string) bool {
	for _, set := range req.GetDefinedSets() {
		if set.GetName() != setName {
			continue
		}
		for _, got := range set.GetPrefixes() {
			if got.GetIpPrefix() == prefix {
				return true
			}
		}
	}
	return false
}

func appliedPolicyRequestHasDefinedSet(req *gobgpapi.SetPoliciesRequest, typ gobgpapi.DefinedType, setName, value string) bool {
	for _, set := range req.GetDefinedSets() {
		if set.GetDefinedType() != typ || set.GetName() != setName {
			continue
		}
		for _, got := range set.GetList() {
			if got == value {
				return true
			}
		}
	}
	return false
}

func TestAppliedPoliciesRestoreMultipleImportPoliciesWithUniqueStatements(t *testing.T) {
	plan := bgpdaemon.BuildPolicyPlan(bgpdaemon.PolicyIntent{RouterName: "restore", Scopes: []bgpdaemon.PolicyScope{
		{Name: "routerd-restore-import-effective-peer-192-168-1-38", Neighbors: []string{"192.168.1.38/32"}, ExportPolicyName: "peer-a-export", Spec: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/24"}}},
		{Name: "routerd-restore-import-effective-peer-192-168-1-53", Neighbors: []string{"192.168.1.53/32"}, ExportPolicyName: "peer-b-export", Spec: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/24"}}},
	}}, nil)
	req := plan.SetPolicies
	assertAppliedPolicyStatementNamesUnique(t, req)
	if !appliedPolicyRequestHasStatement(req, "routerd-restore-import-effective", "routerd-restore-import-effective-peer-192-168-1-38-allow-import") {
		t.Fatalf("restore policies = %#v, want statement scoped to 192.168.1.38", req)
	}
	if !appliedPolicyRequestHasStatement(req, "routerd-restore-import-effective", "routerd-restore-import-effective-peer-192-168-1-53-allow-import") {
		t.Fatalf("restore policies = %#v, want statement scoped to 192.168.1.53", req)
	}
}

func TestAppliedPeerEbgpMultihop(t *testing.T) {
	direct := appliedPeer(bgpdaemon.AppliedPeer{Address: "192.0.2.2", ASN: 64513}, bgpdaemon.AppliedGlobal{ASN: 64512})
	if direct.GetEbgpMultihop() != nil {
		t.Fatalf("direct peer eBGP multihop = %#v, want nil", direct.GetEbgpMultihop())
	}
	multihop := appliedPeer(bgpdaemon.AppliedPeer{Address: "192.0.2.2", ASN: 64513, EbgpMultihop: 16}, bgpdaemon.AppliedGlobal{ASN: 64512})
	if got := multihop.GetEbgpMultihop(); !got.GetEnabled() || got.GetMultihopTtl() != 16 {
		t.Fatalf("restored eBGP multihop = %#v, want enabled ttl=16", got)
	}
}

func TestAppliedPolicyPrefixesAllowMoreSpecifics(t *testing.T) {
	plan := bgpdaemon.BuildPolicyPlan(bgpdaemon.PolicyIntent{RouterName: "prefixes", Scopes: []bgpdaemon.PolicyScope{{Name: "peer", Neighbors: []string{"192.0.2.1/32"}, ExportPolicyName: "peer-export", Spec: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24", "2001:db8:77::/64"}}}}}, nil)
	var ipv4, ipv6 []*gobgpapi.Prefix
	for _, set := range plan.SetPolicies.GetDefinedSets() {
		switch set.GetName() {
		case "peer-prefixes-ipv4":
			ipv4 = set.GetPrefixes()
		case "peer-prefixes-ipv6":
			ipv6 = set.GetPrefixes()
		}
	}
	if len(ipv4) != 1 || ipv4[0].GetIpPrefix() != "10.77.60.0/24" || ipv4[0].GetMaskLengthMin() != 24 || ipv4[0].GetMaskLengthMax() != 32 {
		t.Fatalf("IPv4 prefix set lost its prefix or mask bounds: %v", ipv4)
	}
	if len(ipv6) != 1 || ipv6[0].GetIpPrefix() != "2001:db8:77::/64" || ipv6[0].GetMaskLengthMin() != 64 || ipv6[0].GetMaskLengthMax() != 128 {
		t.Fatalf("IPv6 prefix set lost its prefix or mask bounds: %v", ipv6)
	}
	if !appliedPrefixSetAllows(ipv4, "10.77.60.0/24") || !appliedPrefixSetAllows(ipv4, "10.77.60.11/32") {
		t.Fatalf("applied prefixes = %#v, want IPv4 prefix and more-specific accepted", ipv4)
	}
	if appliedPrefixSetAllows(ipv4, "10.77.0.0/16") || appliedPrefixSetAllows(ipv4, "10.88.0.1/32") {
		t.Fatalf("applied prefixes = %#v, want less-specific and unrelated IPv4 rejected", ipv4)
	}
	if !appliedPrefixSetAllows(ipv6, "2001:db8:77::/64") || !appliedPrefixSetAllows(ipv6, "2001:db8:77::11/128") {
		t.Fatalf("applied prefixes = %#v, want IPv6 prefix and /128 accepted", ipv6)
	}
	if appliedPrefixSetAllows(ipv6, "2001:db8:77::/48") || appliedPrefixSetAllows(ipv6, "2001:db8:88::1/128") {
		t.Fatalf("applied prefixes = %#v, want less-specific and unrelated IPv6 rejected", ipv6)
	}
}

func appliedPrefixSetAllows(prefixes []*gobgpapi.Prefix, candidate string) bool {
	parsed, err := netip.ParsePrefix(candidate)
	if err != nil {
		return false
	}
	parsed = parsed.Masked()
	for _, allowed := range prefixes {
		parent, err := netip.ParsePrefix(allowed.GetIpPrefix())
		if err != nil {
			continue
		}
		parent = parent.Masked()
		if parent.Addr().Is4() != parsed.Addr().Is4() {
			continue
		}
		if parent.Contains(parsed.Addr()) && uint32(parsed.Bits()) >= allowed.GetMaskLengthMin() && uint32(parsed.Bits()) <= allowed.GetMaskLengthMax() {
			return true
		}
	}
	return false
}

func appliedPolicyRequestHasStatement(req *gobgpapi.SetPoliciesRequest, policyName, statementName string) bool {
	for _, policy := range req.GetPolicies() {
		if policy.GetName() != policyName {
			continue
		}
		for _, statement := range policy.GetStatements() {
			if statement.GetName() == statementName {
				return true
			}
		}
	}
	return false
}

func assertAppliedPolicyStatementNamesUnique(t *testing.T, req *gobgpapi.SetPoliciesRequest) {
	t.Helper()
	seen := map[string]string{}
	for _, policy := range req.GetPolicies() {
		for _, statement := range policy.GetStatements() {
			name := statement.GetName()
			if previous := seen[name]; previous != "" {
				t.Fatalf("statement name %q reused by policies %q and %q", name, previous, policy.GetName())
			}
			seen[name] = policy.GetName()
		}
	}
}

func TestAppliedPeerRestoresInternalRouteReflectorClient(t *testing.T) {
	peer := appliedPeer(bgpdaemon.AppliedPeer{
		Address:                 "10.99.0.2",
		ASN:                     64577,
		RouteReflectorClient:    true,
		RouteReflectorClusterID: "10.99.0.1",
	}, bgpdaemon.AppliedGlobal{ASN: 64577})
	if peer.GetConf().GetType() != gobgpapi.PeerType_PEER_TYPE_INTERNAL {
		t.Fatalf("peer type = %v, want internal", peer.GetConf().GetType())
	}
	rr := peer.GetRouteReflector()
	if !rr.GetRouteReflectorClient() || rr.GetRouteReflectorClusterId() != "10.99.0.1" {
		t.Fatalf("route reflector = %#v, want client cluster 10.99.0.1", rr)
	}
}

func TestPendingDirectPeerTransitionsWithholdOnlyDirectPeers(t *testing.T) {
	applied := bgpdaemon.AppliedConfig{
		PendingPolicy:              &bgpdaemon.PolicyIntent{RouterName: "pending"},
		PendingDirectPeerAdditions: []string{"10.0.0.3"},
		PendingDirectPeerRemovals:  []string{"10.0.0.4"},
		Peers: map[string]bgpdaemon.AppliedPeer{
			"10.0.0.2": {Address: "10.0.0.2", PreserveImportPrefixes: true},
			"10.0.0.3": {Address: "10.0.0.3", PreserveImportPrefixes: true},
			"10.0.0.4": {Address: "10.0.0.4", PreserveImportPrefixes: true},
			"10.0.0.5": {Address: "10.0.0.5"},
		},
	}
	got := pendingDirectPeerTransitions(applied)
	for _, address := range []string{"10.0.0.2", "10.0.0.3", "10.0.0.4"} {
		if !got[address] {
			t.Fatalf("withheld direct peers = %#v, missing %s", got, address)
		}
	}
	if got["10.0.0.5"] {
		t.Fatalf("withheld direct peers = %#v, regular peer was withheld", got)
	}
}

func TestDynamicExportPolicyUsesTypedScopesInsteadOfPendingPeerHistory(t *testing.T) {
	applied := bgpdaemon.AppliedConfig{
		Global: bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "192.0.2.1", ListenPort: -1},
		Policy: bgpdaemon.PolicyIntent{RouterName: "typed", Scopes: []bgpdaemon.PolicyScope{{
			Name: "rr", Neighbors: []string{"10.0.0.3/32"}, ExportPolicyName: "rr-export",
			ExportPolicy: api.BGPExportPolicySpec{AllowedPrefixes: []string{"10.77.60.22/32"}},
		}}},
		PendingDirectPeerRemovals: []string{"10.0.0.2"},
		Peers: map[string]bgpdaemon.AppliedPeer{
			"10.0.0.2": {
				Address:                "10.0.0.2",
				ASN:                    64513,
				PreserveImportPrefixes: true,
			},
			"10.0.0.3": {
				Address: "10.0.0.3",
				ASN:     64513,
			},
		},
		Paths: []bgpdaemon.AppliedPath{{
			Source: "MobilityPool/demo/node/leaf-a",
			Prefix: "10.77.60.22/32",
		}},
	}
	server := realDynamicPathServer(t)
	applied, err := reconcilePolicyConfig(context.Background(), server, filepath.Join(t.TempDir(), "applied.json"), applied, applied.Policy)
	if err != nil {
		t.Fatal(err)
	}
	if matches, err := policyPlanMatches(context.Background(), server, bgpdaemon.BuildPolicyPlan(applied.Policy, applied.Paths)); err != nil || !matches {
		t.Fatalf("native plan did not match current typed scopes: %t %v", matches, err)
	}
	var retired *gobgpapi.Policy
	if err := server.ListPolicy(context.Background(), &gobgpapi.ListPolicyRequest{Name: "direct-export"}, func(policy *gobgpapi.Policy) { retired = policy }); err != nil || retired != nil {
		t.Fatalf("pending retired peer history revived an export policy: policy=%v err=%v", retired, err)
	}
}

func TestRestoreAppliedRestoresStaticAndMobilityPathsWithFreshUUIDs(t *testing.T) {
	server := newFakePathServer(t)
	applied := bgpdaemon.AppliedConfig{
		Global: bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "10.0.0.1", ListenPort: 179},
		Paths: []bgpdaemon.AppliedPath{bgpdaemon.StaticAppliedPath("10.20.0.0/24", nil), {
			Source: "MobilityPool/demo/node/aws-router-a",
			Prefix: "10.77.60.11/32",
			Attrs:  bgpdaemon.AppliedPathAttrs{LocalPref: 200},
		}},
	}
	if err := restoreAppliedPaths(context.Background(), server, &applied); err != nil {
		t.Fatalf("restore paths: %v", err)
	}
	if len(server.added) != 2 {
		t.Fatalf("AddPath calls = %d, want static + mobility", len(server.added))
	}
	bySource := map[string]bgpdaemon.AppliedPath{}
	for _, path := range applied.Paths {
		bySource[path.Source] = path
		if path.UUID == "" {
			t.Fatalf("path missing restored UUID: %#v", path)
		}
	}
	if bySource[bgpdaemon.AppliedPathSourceStatic].Prefix != "10.20.0.0/24" {
		t.Fatalf("static restored path = %#v", bySource[bgpdaemon.AppliedPathSourceStatic])
	}
	if bySource["MobilityPool/demo/node/aws-router-a"].Prefix != "10.77.60.11/32" {
		t.Fatalf("mobility restored path = %#v", bySource["MobilityPool/demo/node/aws-router-a"])
	}
}

func TestRestoreAppliedRefreshesDynamicExportPolicy(t *testing.T) {
	server := newFakePathServer(t)
	statePath := filepath.Join(t.TempDir(), "applied.json")
	applied := bgpdaemon.AppliedConfig{
		Global: bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "10.0.0.1", ListenPort: 179},
		Policy: bgpdaemon.PolicyIntent{RouterName: "restore", Scopes: []bgpdaemon.PolicyScope{{
			Name: "lan-peer", Neighbors: []string{"10.252.0.2/32"}, ExportPolicyName: "routerd-lan-export-10-252-0-2",
			ExportPolicy: api.BGPExportPolicySpec{AllowedPrefixes: []string{"192.168.123.208/32"}},
		}}},
		Peers: map[string]bgpdaemon.AppliedPeer{
			"10.252.0.2": {
				Address: "10.252.0.2",
				ASN:     64512,
			},
		},
		Paths: []bgpdaemon.AppliedPath{{
			Source: "MobilityPool/svnet1/node/pve-rt08",
			Prefix: "192.168.123.132/32",
			Attrs:  bgpdaemon.AppliedPathAttrs{LocalPref: 200},
		}},
	}
	if err := restoreAppliedPaths(context.Background(), server, &applied); err != nil {
		t.Fatalf("restore paths: %v", err)
	}
	if err := server.policyOwner.AddPeer(context.Background(), &gobgpapi.AddPeerRequest{Peer: &gobgpapi.Peer{
		Conf: &gobgpapi.PeerConf{NeighborAddress: "10.252.0.2", PeerAsn: 64512}, Transport: &gobgpapi.Transport{PassiveMode: true},
	}}); err != nil {
		t.Fatal(err)
	}
	var err error
	if applied, err = refreshOwnedPathPolicies(context.Background(), server, statePath, applied); err != nil {
		t.Fatalf("refresh dynamic policies: %v", err)
	}
	if matches, err := policyPlanMatches(context.Background(), server, bgpdaemon.BuildPolicyPlan(applied.Policy, applied.Paths)); err != nil || !matches {
		t.Fatalf("native policy owner did not publish restored complete plan: matches=%t err=%v", matches, err)
	}
	if len(server.policyRequests) != 1 {
		t.Fatalf("SetPolicies calls = %d, want restore policy refresh", len(server.policyRequests))
	}
	if !appliedPolicyRequestHasPrefix(server.policyRequests[0], "routerd-lan-export-10-252-0-2-prefixes", "192.168.123.132/32") {
		t.Fatalf("restored policies = %#v, want dynamic mobility prefix in export policy", server.policyRequests[0])
	}
	if len(server.resetRequests) != 1 {
		t.Fatalf("ResetPeer calls = %d, want soft inbound/outbound reset for restored policy", len(server.resetRequests))
	}
	reset := server.resetRequests[0]
	if reset.GetAddress() != "10.252.0.2" || !reset.GetSoft() || reset.GetDirection() != gobgpapi.ResetPeerRequest_DIRECTION_BOTH {
		t.Fatalf("ResetPeer request = %#v, want soft inbound/outbound reset for 10.252.0.2", reset)
	}
}

func TestControlSocketSerializesAppliedStateTransactions(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "applied.json")
	if err := bgpdaemon.WriteApplied(statePath, bgpdaemon.AppliedConfig{
		Policy: bgpdaemon.PolicyIntent{RouterName: "test"},
		Global: bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "10.0.0.1", ListenPort: 179},
	}); err != nil {
		t.Fatalf("write initial applied: %v", err)
	}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	paths := newFakePathServer(t)
	paths.addStarted, paths.releaseAdd = started, release
	socketPath := filepath.Join(dir, "control.sock")
	stateLock := &notifyingStateMutex{attempts: make(chan struct{}, 2)}
	server, err := serveControlSocketWithStateLock(socketPath, statePath, paths, stateLock)
	if err != nil {
		t.Fatalf("serve control socket: %v", err)
	}
	defer server.Shutdown(context.Background())
	var releaseOnce sync.Once
	releaseTransaction := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseTransaction()
	client := unixHTTPClient(socketPath)
	defer client.CloseIdleConnections()

	body, err := json.Marshal(bgpdaemon.AppliedPath{
		Source: "MobilityPool/demo/node/aws-router-a",
		Prefix: "10.77.60.11/32",
	})
	if err != nil {
		t.Fatalf("marshal path: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, "http://routerd-bgp/v1/paths", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new path request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	postDone := make(chan error, 1)
	go func() {
		response, err := client.Do(request)
		if err != nil {
			postDone <- err
			return
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			postDone <- fmt.Errorf("POST status = %d", response.StatusCode)
			return
		}
		postDone <- nil
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("path transaction did not reach AddPath")
	}
	<-stateLock.attempts // POST reached the same state lock before AddPath.
	getDone := make(chan error, 1)
	go func() {
		response, err := client.Get("http://routerd-bgp/v1/applied")
		if err == nil {
			response.Body.Close()
		}
		getDone <- err
	}()
	select {
	case err := <-getDone:
		t.Fatalf("GET completed during in-flight state transaction: %v", err)
	case <-stateLock.attempts: // GET reached the lock held by the transaction.
	case <-time.After(10 * time.Second):
		t.Fatal("GET did not reach applied-state lock")
	}
	releaseTransaction()
	if err := <-postDone; err != nil {
		t.Fatalf("POST dynamic path: %v", err)
	}
	if err := <-getDone; err != nil {
		t.Fatalf("GET applied after transaction: %v", err)
	}
}

func TestControlPathAPISourceScopedMobilityUpsertAndDelete(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "applied.json")
	initial := bgpdaemon.AppliedConfig{
		Policy: bgpdaemon.PolicyIntent{RouterName: "test"},
		Global: bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "10.0.0.1", ListenPort: 179},
		Paths:  []bgpdaemon.AppliedPath{bgpdaemon.StaticAppliedPath("10.20.0.0/24", nil)},
	}
	if err := bgpdaemon.WriteApplied(statePath, initial); err != nil {
		t.Fatalf("write initial applied: %v", err)
	}
	socketPath := filepath.Join(dir, "control.sock")
	paths := newFakePathServer(t)
	server, err := serveControlSocket(socketPath, statePath, paths)
	if err != nil {
		t.Fatalf("serve control socket: %v", err)
	}
	defer server.Shutdown(context.Background())
	client := unixHTTPClient(socketPath)
	defer client.CloseIdleConnections()

	body := bgpdaemon.AppliedPath{
		Source: "MobilityPool/demo/node/aws-router-a",
		Prefix: "10.77.60.11/32",
		Attrs:  bgpdaemon.AppliedPathAttrs{LocalPref: 200, Communities: []string{"64512:77"}},
	}
	resp := doJSON(t, client, http.MethodPost, "/v1/paths", body)
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("POST /v1/paths status = %d body=%s", resp.StatusCode, bytes.TrimSpace(data))
	}
	var got bgpdaemon.AppliedPath
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode path response: %v", err)
	}
	resp.Body.Close()
	if got.Source != body.Source || got.Prefix != body.Prefix || got.UUID == "" {
		t.Fatalf("upsert response = %#v", got)
	}
	if len(paths.added) != 1 {
		t.Fatalf("AddPath calls = %d, want 1", len(paths.added))
	}

	resp = doJSON(t, client, http.MethodDelete, "/v1/paths?source=MobilityPool/demo/node/aws-router-a&prefix=10.77.60.11/32", nil)
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("DELETE /v1/paths status = %d body=%s", resp.StatusCode, bytes.TrimSpace(data))
	}
	resp.Body.Close()
	if len(paths.deleted) != 1 || paths.deleted[0].GetPath().GetIdentifier() != bgpdaemon.NativePathIdentifier(body.Source) || len(paths.deleted[0].GetUuid()) != 0 {
		t.Fatalf("dynamic withdrawal did not use exact native source identity: %#v", paths.deleted)
	}
	applied, _, err := bgpdaemon.ReadApplied(statePath)
	if err != nil {
		t.Fatalf("read applied after delete: %v", err)
	}
	if len(bgpdaemon.NonStaticPaths(applied.Paths)) != 0 {
		t.Fatalf("dynamic paths after delete = %#v", bgpdaemon.NonStaticPaths(applied.Paths))
	}
	if prefixes := bgpdaemon.StaticAdvertisements(applied.Paths); len(prefixes) != 1 || prefixes[0] != "10.20.0.0/24" {
		t.Fatalf("static advertisements changed: %#v", prefixes)
	}
}

func TestControlObservedPathAPIReadsLiveRIBNotAppliedPathJournal(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "applied.json")
	if err := bgpdaemon.WriteApplied(statePath, bgpdaemon.AppliedConfig{
		Policy: bgpdaemon.PolicyIntent{RouterName: "test"},
		Global: bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "10.0.0.1", ListenPort: 179},
		Paths: []bgpdaemon.AppliedPath{{
			Source: "MobilityPool/demo/node/router-a",
			Prefix: "10.77.60.11/32",
		}},
	}); err != nil {
		t.Fatalf("write applied state: %v", err)
	}
	socketPath := filepath.Join(dir, "control.sock")
	paths := &fakePathServer{policyOwner: realDynamicPathServer(t), observed: []bgpdaemon.ObservedPath{{
		Prefix:      "10.77.60.11/32",
		PeerAddress: "10.99.0.2",
		Best:        true,
		Valid:       true,
		Communities: []string{"64512:100", "64512:121", "64512:22222"},
	}}}
	server, err := serveControlSocket(socketPath, statePath, paths)
	if err != nil {
		t.Fatalf("serve control socket: %v", err)
	}
	defer server.Shutdown(context.Background())
	client := unixHTTPClient(socketPath)
	defer client.CloseIdleConnections()

	resp := doJSON(t, client, http.MethodGet, "/v1/observed-paths", nil)
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("GET /v1/observed-paths status = %d body=%s", resp.StatusCode, bytes.TrimSpace(data))
	}
	var got []bgpdaemon.ObservedPath
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		resp.Body.Close()
		t.Fatalf("decode observed paths: %v", err)
	}
	resp.Body.Close()
	if len(got) != 1 || got[0].PeerAddress != "10.99.0.2" || !got[0].Best || !got[0].Valid {
		t.Fatalf("observed paths = %#v", got)
	}

	resp = doJSON(t, client, http.MethodGet, "/v1/paths", nil)
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("GET /v1/paths status = %d body=%s", resp.StatusCode, bytes.TrimSpace(data))
	}
	var applied []bgpdaemon.AppliedPath
	if err := json.NewDecoder(resp.Body).Decode(&applied); err != nil {
		resp.Body.Close()
		t.Fatalf("decode applied paths: %v", err)
	}
	resp.Body.Close()
	if len(applied) != 1 || applied[0].Source != "MobilityPool/demo/node/router-a" {
		t.Fatalf("applied paths = %#v", applied)
	}
}

func TestControlPathAPIUpsertRefreshesDynamicExportPolicy(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "applied.json")
	initial := bgpdaemon.AppliedConfig{
		Policy: bgpdaemon.PolicyIntent{RouterName: "test", Scopes: []bgpdaemon.PolicyScope{{
			Name: "lan-peer", Neighbors: []string{"10.252.0.2/32"}, Spec: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.252.0.0/24"}},
			ExportPolicyName: "routerd-lan-export-10-252-0-2", ExportPolicy: api.BGPExportPolicySpec{AllowedPrefixes: []string{"192.168.123.208/32"}},
		}}},
		Global: bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "10.0.0.1", ListenPort: 179},
		Peers: map[string]bgpdaemon.AppliedPeer{
			"10.252.0.2": {
				Address: "10.252.0.2",
				ASN:     64512,
			},
		},
	}
	if err := bgpdaemon.WriteApplied(statePath, initial); err != nil {
		t.Fatalf("write initial applied: %v", err)
	}
	paths := newFakePathServer(t)
	if err := paths.policyOwner.AddPeer(context.Background(), &gobgpapi.AddPeerRequest{Peer: &gobgpapi.Peer{
		Conf: &gobgpapi.PeerConf{NeighborAddress: "10.252.0.2", PeerAsn: 64512}, Transport: &gobgpapi.Transport{PassiveMode: true},
	}}); err != nil {
		t.Fatal(err)
	}

	body := bgpdaemon.AppliedPath{
		Source: "MobilityPool/svnet1/node/pve-rt08",
		Prefix: "192.168.123.132/32",
		Attrs:  bgpdaemon.AppliedPathAttrs{LocalPref: 200},
	}
	wire, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	controlHandler(statePath, paths, &sync.Mutex{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/paths", bytes.NewReader(wire)))
	if response.Code != http.StatusOK {
		t.Fatalf("POST /v1/paths status=%d body=%s", response.Code, response.Body.String())
	}
	if len(paths.policyRequests) == 0 {
		t.Fatalf("SetPolicies calls = 0, want dynamic policy refresh")
	}
	lastPolicyRequest := paths.policyRequests[len(paths.policyRequests)-1]
	if !appliedPolicyRequestHasPrefix(lastPolicyRequest, "routerd-lan-export-10-252-0-2-prefixes", "192.168.123.132/32") {
		t.Fatalf("refreshed policies = %#v, want dynamic mobility prefix in export policy", lastPolicyRequest)
	}
	if len(paths.resetRequests) != 1 {
		t.Fatalf("ResetPeer calls = %d, want one inbound/outbound soft reset", len(paths.resetRequests))
	}
	reset := paths.resetRequests[0]
	if reset.GetAddress() != "10.252.0.2" || !reset.GetSoft() || reset.GetDirection() != gobgpapi.ResetPeerRequest_DIRECTION_BOTH {
		t.Fatalf("ResetPeer request = %#v, want soft inbound/outbound reset for 10.252.0.2", reset)
	}
	stored, _, err := bgpdaemon.ReadApplied(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if matches, err := policyPlanMatches(context.Background(), paths, bgpdaemon.BuildPolicyPlan(stored.Policy, stored.Paths)); err != nil || !matches {
		t.Fatalf("published state lacks its matching native policy plan: %t %v", matches, err)
	}
}

func TestControlPathAPIRejectsNonMobilityAndNonHostPaths(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "applied.json")
	if err := bgpdaemon.WriteApplied(statePath, bgpdaemon.AppliedConfig{Policy: bgpdaemon.PolicyIntent{RouterName: "test"}, Global: bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "10.0.0.1"}}); err != nil {
		t.Fatalf("write applied: %v", err)
	}
	socketPath := filepath.Join(dir, "control.sock")
	server, err := serveControlSocket(socketPath, statePath, newFakePathServer(t))
	if err != nil {
		t.Fatalf("serve control socket: %v", err)
	}
	defer server.Shutdown(context.Background())
	client := unixHTTPClient(socketPath)
	defer client.CloseIdleConnections()
	for _, body := range []bgpdaemon.AppliedPath{
		{Source: bgpdaemon.AppliedPathSourceStatic, Prefix: "10.77.60.11/32"},
		{Source: "MobilityPool/demo/node/aws-router-a", Prefix: "10.77.60.0/24"},
	} {
		resp := doJSON(t, client, http.MethodPost, "/v1/paths", body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Fatalf("POST accepted invalid path %#v", body)
		}
	}
}

func TestUpsertDynamicPathReplacesNativeIdentityDespiteStaleUUID(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "applied.json")
	source := "MobilityPool/demo/node/aws-router-a"
	initial := bgpdaemon.AppliedConfig{
		Policy:  bgpdaemon.PolicyIntent{RouterName: "test"},
		Version: bgpdaemon.AppliedVersion,
		Global:  bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "10.0.0.1", ListenPort: 179},
		Paths: []bgpdaemon.AppliedPath{{
			Source: source,
			Prefix: "10.77.60.11/32",
			Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
			UUID:   bgpdaemon.EncodeUUID([]byte{9}),
		}},
	}
	if err := bgpdaemon.WriteApplied(statePath, initial); err != nil {
		t.Fatalf("write initial applied: %v", err)
	}
	server := newFakePathServer(t)
	_, got, err := upsertDynamicPath(context.Background(), server, statePath, bgpdaemon.AppliedPath{
		Source: source,
		Prefix: "10.77.60.11/32",
		Attrs:  bgpdaemon.AppliedPathAttrs{LocalPref: 201},
	})
	if err != nil {
		t.Fatalf("upsert stale UUID path: %v", err)
	}
	if got == nil || got.UUID == "" || got.UUID == bgpdaemon.EncodeUUID([]byte{9}) {
		t.Fatalf("upserted path = %#v, want fresh UUID", got)
	}
	if len(server.deleted) != 0 || len(server.added) != 1 || server.added[0].GetPath().GetIdentifier() != bgpdaemon.NativePathIdentifier(source) {
		t.Fatalf("native replacement: delete/add = %d/%d, request=%#v", len(server.deleted), len(server.added), server.added)
	}
}

func TestDynamicPathUpdateCompletesPendingTypedPolicyBeforePublication(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "applied.json")
	target := bgpdaemon.PolicyIntent{RouterName: "pending", Scopes: []bgpdaemon.PolicyScope{{
		Name: "current-peer", Neighbors: []string{"192.0.2.2/32"}, ExportPolicyName: "current-export",
		Spec: api.BGPImportPolicySpec{AllowedPrefixes: []string{"192.0.2.0/24"}},
	}}}
	initial := bgpdaemon.AppliedConfig{
		Version: bgpdaemon.AppliedVersion,
		Policy:  bgpdaemon.PolicyIntent{RouterName: "old"}, PendingPolicy: &target,
		Global: bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "10.0.0.1", ListenPort: -1},
	}
	if err := bgpdaemon.WriteApplied(statePath, initial); err != nil {
		t.Fatal(err)
	}
	server := newFakePathServer(t)
	path := bgpdaemon.AppliedPath{Source: "MobilityPool/demo/node/aws-router-a", Prefix: "10.77.60.11/32"}
	applied, updated, err := upsertDynamicPath(context.Background(), server, statePath, path)
	if err != nil || updated == nil {
		t.Fatalf("upsert pending policy: updated=%#v err=%v", updated, err)
	}
	if applied.PendingPolicy != nil || applied.Policy.RouterName != "pending" {
		t.Fatalf("upsert acknowledged an incomplete/old policy: %#v", applied)
	}
	if len(server.policyRequests) != 2 || appliedPolicyRequestHasPrefix(server.policyRequests[0], "current-export-prefixes", path.Prefix) || !appliedPolicyRequestHasPrefix(server.policyRequests[1], "current-export-prefixes", path.Prefix) {
		t.Fatalf("current policy did not include new path: %#v", server.policyRequests)
	}
	if matches, err := policyPlanMatches(context.Background(), server, bgpdaemon.BuildPolicyPlan(target, applied.Paths)); err != nil || !matches {
		t.Fatalf("upsert published without exact native policy: %t %v", matches, err)
	}
	applied, err = deleteDynamicPath(context.Background(), server, statePath, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(server.policyRequests) != 3 || len(bgpdaemon.NonStaticPaths(applied.Paths)) != 0 {
		t.Fatalf("delete did not refresh the typed policy: %#v", applied)
	}
	if matches, err := policyPlanMatches(context.Background(), server, bgpdaemon.BuildPolicyPlan(target, applied.Paths)); err != nil || !matches {
		t.Fatalf("delete published without exact native policy: %t %v", matches, err)
	}
}

func unixHTTPClient(socketPath string) *http.Client {
	return &http.Client{Transport: &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}}
}

func doJSON(t *testing.T, client *http.Client, method, path string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, "http://routerd-bgp"+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// Observe actual lock entry, while preserving its real serialization behavior.
type notifyingStateMutex struct {
	sync.Mutex
	attempts chan struct{}
}

func (m *notifyingStateMutex) Lock() {
	m.attempts <- struct{}{}
	m.Mutex.Lock()
}

func TestRestoreAppliedDoesNotRestorePendingRemoval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "applied.json")
	if err := bgpdaemon.BeginRemoval(path); err != nil {
		t.Fatal(err)
	}
	// The pending intent must fence restore even before reading stale or
	// malformed daemon-owned applied state.
	if err := os.WriteFile(path, []byte("interrupted old state"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := restoreApplied(context.Background(), nil, &fakePathServer{}, path, nil); err != nil {
		t.Fatal(err)
	}
}
