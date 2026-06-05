// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"testing"

	gobgpapi "github.com/osrg/gobgp/v3/api"

	"github.com/imksoo/routerd/pkg/bgpdaemon"
)

type fakePathServer struct {
	added   []*gobgpapi.AddPathRequest
	deleted [][]byte
	nextID  byte
}

func (s *fakePathServer) AddPath(_ context.Context, req *gobgpapi.AddPathRequest) (*gobgpapi.AddPathResponse, error) {
	s.nextID++
	uuid := []byte{s.nextID}
	s.added = append(s.added, req)
	return &gobgpapi.AddPathResponse{Uuid: uuid}, nil
}

func (s *fakePathServer) DeletePath(_ context.Context, req *gobgpapi.DeletePathRequest) error {
	s.deleted = append(s.deleted, append([]byte(nil), req.GetUuid()...))
	return nil
}

func TestAppliedPoliciesRestorePeerImportPolicyWithoutGlobalPolicy(t *testing.T) {
	peer := bgpdaemon.AppliedPeer{
		Address:          "192.168.1.38",
		ImportPolicyName: "routerd-lan-import",
		ImportPolicy: bgpdaemon.AppliedImportPolicy{
			AllowedPrefixes: []string{"10.250.0.0/24"},
			NextHopRewrite:  "peer-address",
		},
	}
	req, assignment := appliedPolicies(bgpdaemon.AppliedConfig{
		Peers: map[string]bgpdaemon.AppliedPeer{
			"192.168.1.38": peer,
		},
	})
	if len(req.GetPolicies()) != 1 || len(req.GetDefinedSets()) != 1 {
		t.Fatalf("restore policies = policies:%d definedSets:%d, want one peer policy and one prefix set", len(req.GetPolicies()), len(req.GetDefinedSets()))
	}
	policy := req.GetPolicies()[0]
	if policy.GetName() != "routerd-lan-import" {
		t.Fatalf("policy name = %q, want peer import policy name", policy.GetName())
	}
	action := policy.GetStatements()[0].GetActions().GetNexthop()
	if !action.GetPeerAddress() {
		t.Fatalf("next-hop action = %#v, want peer-address rewrite", action)
	}
	if assignment.GetName() != "global" || assignment.GetDirection() != gobgpapi.PolicyDirection_IMPORT ||
		assignment.GetDefaultAction() != gobgpapi.RouteAction_REJECT || len(assignment.GetPolicies()) != 1 ||
		assignment.GetPolicies()[0].GetName() != "routerd-lan-import" {
		t.Fatalf("global import policy assignment = %#v, want restored policy assigned to global import", assignment)
	}
	restoredPeer := appliedPeer(peer, bgpdaemon.AppliedGlobal{ASN: 64512})
	if applyPolicy := restoredPeer.GetApplyPolicy(); applyPolicy != nil && applyPolicy.GetImportPolicy() != nil {
		t.Fatalf("restored peer import policy = %#v, want no per-neighbor import policy for normal eBGP", applyPolicy.GetImportPolicy())
	}
}

func TestAppliedPoliciesRestorePeerExportPolicy(t *testing.T) {
	peer := bgpdaemon.AppliedPeer{
		Address:          "10.252.0.18",
		ASN:              64512,
		ImportPolicyName: "routerd-lan-import",
		ImportPolicy: bgpdaemon.AppliedImportPolicy{
			AllowedPrefixes: []string{"10.252.0.0/24"},
		},
		ExportPolicyName: "routerd-lan-export-10-252-0-2",
		ExportPolicy: bgpdaemon.AppliedExportPolicy{
			AllowedPrefixes: []string{"192.168.123.129/32", "192.168.123.132/32"},
		},
	}
	req, assignment := appliedPolicies(bgpdaemon.AppliedConfig{
		Peers: map[string]bgpdaemon.AppliedPeer{
			"10.252.0.18": peer,
		},
	})
	if !appliedPolicyRequestHasStatement(req, "routerd-lan-import", "routerd-lan-import-allow-import") {
		t.Fatalf("restore policies = %#v, want import policy", req)
	}
	if !appliedPolicyRequestHasStatement(req, "routerd-lan-export-10-252-0-2", "routerd-lan-export-10-252-0-2-allow-export") {
		t.Fatalf("restore policies = %#v, want peer export policy", req)
	}
	if len(assignment.GetPolicies()) != 1 || assignment.GetPolicies()[0].GetName() != "routerd-lan-import" {
		t.Fatalf("global import assignment = %#v, want only import policy", assignment)
	}
	restoredPeer := appliedPeer(peer, bgpdaemon.AppliedGlobal{ASN: 64512})
	exportAssignment := restoredPeer.GetApplyPolicy().GetExportPolicy()
	if exportAssignment.GetDirection() != gobgpapi.PolicyDirection_EXPORT ||
		exportAssignment.GetDefaultAction() != gobgpapi.RouteAction_REJECT ||
		len(exportAssignment.GetPolicies()) != 1 ||
		exportAssignment.GetPolicies()[0].GetName() != "routerd-lan-export-10-252-0-2" {
		t.Fatalf("restored peer export policy = %#v, want export assignment", exportAssignment)
	}
}

func TestAppliedPoliciesRestoreMultipleImportPoliciesWithUniqueStatements(t *testing.T) {
	req, _ := appliedPolicies(bgpdaemon.AppliedConfig{
		Peers: map[string]bgpdaemon.AppliedPeer{
			"192.168.1.38": {
				Address:          "192.168.1.38",
				ImportPolicyName: "routerd-lan-import-a",
				ImportPolicy: bgpdaemon.AppliedImportPolicy{
					AllowedPrefixes: []string{"10.250.0.0/24"},
				},
			},
			"192.168.1.53": {
				Address:          "192.168.1.53",
				ImportPolicyName: "routerd-lan-import-b",
				ImportPolicy: bgpdaemon.AppliedImportPolicy{
					AllowedPrefixes: []string{"10.250.0.0/24"},
				},
			},
		},
	})
	assertAppliedPolicyStatementNamesUnique(t, req)
	if !appliedPolicyRequestHasStatement(req, "routerd-lan-import-a", "routerd-lan-import-a-allow-import") {
		t.Fatalf("restore policies = %#v, want statement scoped to routerd-lan-import-a", req)
	}
	if !appliedPolicyRequestHasStatement(req, "routerd-lan-import-b", "routerd-lan-import-b-allow-import") {
		t.Fatalf("restore policies = %#v, want statement scoped to routerd-lan-import-b", req)
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
	prefixes := appliedPolicyPrefixes(bgpdaemon.AppliedImportPolicy{AllowedPrefixes: []string{"10.77.60.0/24", "2001:db8:77::/64"}})
	if !appliedPrefixSetAllows(prefixes, "10.77.60.0/24") || !appliedPrefixSetAllows(prefixes, "10.77.60.11/32") {
		t.Fatalf("applied prefixes = %#v, want IPv4 prefix and more-specific accepted", prefixes)
	}
	if appliedPrefixSetAllows(prefixes, "10.77.0.0/16") || appliedPrefixSetAllows(prefixes, "10.88.0.1/32") {
		t.Fatalf("applied prefixes = %#v, want less-specific and unrelated IPv4 rejected", prefixes)
	}
	if !appliedPrefixSetAllows(prefixes, "2001:db8:77::/64") || !appliedPrefixSetAllows(prefixes, "2001:db8:77::11/128") {
		t.Fatalf("applied prefixes = %#v, want IPv6 prefix and /128 accepted", prefixes)
	}
	if appliedPrefixSetAllows(prefixes, "2001:db8:88::1/128") {
		t.Fatalf("applied prefixes = %#v, want unrelated IPv6 rejected", prefixes)
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
	if peer.GetConf().GetType() != gobgpapi.PeerType_INTERNAL {
		t.Fatalf("peer type = %v, want internal", peer.GetConf().GetType())
	}
	rr := peer.GetRouteReflector()
	if !rr.GetRouteReflectorClient() || rr.GetRouteReflectorClusterId() != "10.99.0.1" {
		t.Fatalf("route reflector = %#v, want client cluster 10.99.0.1", rr)
	}
}

func TestRestoreAppliedRestoresStaticAndMobilityPathsWithFreshUUIDs(t *testing.T) {
	server := &fakePathServer{}
	applied := bgpdaemon.AppliedConfig{
		Global:         bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "10.0.0.1", ListenPort: 179},
		Advertisements: []string{"10.20.0.0/24"},
		Paths: []bgpdaemon.AppliedPath{{
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

func TestControlPathAPISourceScopedMobilityUpsertAndDelete(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "applied.json")
	initial := bgpdaemon.AppliedConfig{
		Global:         bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "10.0.0.1", ListenPort: 179},
		Advertisements: []string{"10.20.0.0/24"},
	}
	if err := bgpdaemon.WriteApplied(statePath, initial); err != nil {
		t.Fatalf("write initial applied: %v", err)
	}
	socketPath := filepath.Join(dir, "control.sock")
	paths := &fakePathServer{}
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
	if len(paths.deleted) != 1 || bgpdaemon.EncodeUUID(paths.deleted[0]) != got.UUID {
		t.Fatalf("deleted UUIDs = %#v, want %s", paths.deleted, got.UUID)
	}
	applied, _, err := bgpdaemon.ReadApplied(statePath)
	if err != nil {
		t.Fatalf("read applied after delete: %v", err)
	}
	if len(bgpdaemon.NonStaticPaths(applied.Paths)) != 0 {
		t.Fatalf("dynamic paths after delete = %#v", bgpdaemon.NonStaticPaths(applied.Paths))
	}
	if len(applied.Advertisements) != 1 || applied.Advertisements[0] != "10.20.0.0/24" {
		t.Fatalf("static advertisements changed: %#v", applied.Advertisements)
	}
}

func TestControlPathAPIRejectsNonMobilityAndNonHostPaths(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "applied.json")
	if err := bgpdaemon.WriteApplied(statePath, bgpdaemon.AppliedConfig{Global: bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "10.0.0.1"}}); err != nil {
		t.Fatalf("write applied: %v", err)
	}
	socketPath := filepath.Join(dir, "control.sock")
	server, err := serveControlSocket(socketPath, statePath, &fakePathServer{})
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
