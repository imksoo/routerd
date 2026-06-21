// SPDX-License-Identifier: BSD-3-Clause

package samlocal

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

type providerKind string

const (
	providerAWS   providerKind = "aws"
	providerAzure providerKind = "azure"
	providerOCI   providerKind = "oci"
)

type providerError struct {
	Code    string
	Message string
}

func (e providerError) Error() string {
	return e.Code + ": " + e.Message
}

type fakeNIC struct {
	ID           string
	Node         string
	Site         string
	Primary      string
	MaxSecondary int
	Secondaries  map[string]bool
}

type fakeProvider struct {
	Kind providerKind
	NICs map[string]*fakeNIC
}

type providerActionRequest struct {
	NICID   string `json:"nicId"`
	Address string `json:"address"`
}

type providerActionResponse struct {
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

func newFakeProvider(kind providerKind, nics ...fakeNIC) *fakeProvider {
	out := &fakeProvider{Kind: kind, NICs: map[string]*fakeNIC{}}
	for i := range nics {
		nic := nics[i]
		if nic.Secondaries == nil {
			nic.Secondaries = map[string]bool{}
		}
		out.NICs[nic.ID] = &nic
	}
	return out
}

func (p *fakeProvider) AssignSecondary(nicID, address string) error {
	nic, ok := p.NICs[nicID]
	if !ok {
		return providerError{Code: "NotFound", Message: "NIC not found"}
	}
	if owner := p.primaryOwner(address); owner != nil {
		return p.rejectPrimaryMove(address, owner.ID, nicID)
	}
	if owner := p.secondaryOwner(address); owner != nil {
		if owner.ID == nicID {
			return nil
		}
		return p.rejectSecondaryMove(address, owner.ID, nicID)
	}
	if nic.MaxSecondary > 0 && len(nic.Secondaries) >= nic.MaxSecondary {
		return providerError{Code: "SecondaryLimitExceeded", Message: fmt.Sprintf("NIC %s secondary limit reached", nicID)}
	}
	nic.Secondaries[address] = true
	return nil
}

func (p *fakeProvider) UnassignSecondary(nicID, address string) error {
	nic, ok := p.NICs[nicID]
	if !ok {
		return providerError{Code: "NotFound", Message: "NIC not found"}
	}
	if nic.Primary == address {
		return providerError{Code: "CannotUnassignPrimary", Message: "primary address cannot be unassigned as secondary"}
	}
	delete(nic.Secondaries, address)
	return nil
}

func (p *fakeProvider) Snapshot() map[string][]string {
	out := map[string][]string{}
	for id, nic := range p.NICs {
		for address := range nic.Secondaries {
			out[id] = append(out[id], address)
		}
		sort.Strings(out[id])
	}
	return out
}

func (p *fakeProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req providerActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	var err error
	switch r.URL.Path {
	case "/assign-secondary-ip":
		err = p.AssignSecondary(req.NICID, req.Address)
	case "/unassign-secondary-ip":
		err = p.UnassignSecondary(req.NICID, req.Address)
	default:
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	resp := providerActionResponse{Status: "succeeded"}
	if err != nil {
		resp.Status = "failed"
		resp.Code = providerErrorCode(err)
		resp.Message = err.Error()
		w.WriteHeader(http.StatusConflict)
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (p *fakeProvider) primaryOwner(address string) *fakeNIC {
	for _, nic := range p.NICs {
		if nic.Primary == address {
			return nic
		}
	}
	return nil
}

func (p *fakeProvider) secondaryOwner(address string) *fakeNIC {
	for _, nic := range p.NICs {
		if nic.Secondaries[address] {
			return nic
		}
	}
	return nil
}

func (p *fakeProvider) rejectPrimaryMove(address, fromNIC, toNIC string) error {
	switch p.Kind {
	case providerAWS:
		return providerError{Code: "InvalidParameterValue", Message: fmt.Sprintf("move not allowed for primary %s from %s to %s", address, fromNIC, toNIC)}
	case providerAzure:
		return providerError{Code: "PrivateIPAddressIsAllocated", Message: fmt.Sprintf("%s is already allocated as a primary private IP", address)}
	case providerOCI:
		return providerError{Code: "PrivateIpAlreadyAssigned", Message: fmt.Sprintf("%s is already assigned as a primary private IP", address)}
	default:
		return providerError{Code: "AddressAlreadyAllocated", Message: fmt.Sprintf("%s is already allocated", address)}
	}
}

func (p *fakeProvider) rejectSecondaryMove(address, fromNIC, toNIC string) error {
	switch p.Kind {
	case providerAWS:
		return providerError{Code: "InvalidParameterValue", Message: fmt.Sprintf("move not allowed for secondary %s from %s to %s", address, fromNIC, toNIC)}
	case providerAzure:
		return providerError{Code: "PrivateIPAddressIsAllocated", Message: fmt.Sprintf("%s is already allocated", address)}
	case providerOCI:
		return providerError{Code: "PrivateIpAlreadyAssigned", Message: fmt.Sprintf("%s is already assigned", address)}
	default:
		return providerError{Code: "AddressAlreadyAllocated", Message: fmt.Sprintf("%s is already allocated", address)}
	}
}

type captureNode struct {
	Name  string
	Site  string
	NICID string
	Live  bool
}

func assignDistributedCaptures(provider *fakeProvider, nodes []captureNode, captures []string, force bool) error {
	live := liveCaptureNodes(nodes)
	for _, address := range captures {
		holder := provider.secondaryOwner(address)
		if holder != nil && !force && nodeLive(live, holder.Node) {
			continue
		}
		target, ok := chooseCaptureNode(live, address)
		if !ok {
			continue
		}
		if holder != nil && holder.ID != target.NICID {
			if err := provider.UnassignSecondary(holder.ID, address); err != nil {
				return err
			}
		}
		if err := provider.AssignSecondary(target.NICID, address); err != nil {
			return err
		}
	}
	return nil
}

func liveCaptureNodes(nodes []captureNode) []captureNode {
	var out []captureNode
	for _, node := range nodes {
		if node.Live {
			out = append(out, node)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func nodeLive(nodes []captureNode, name string) bool {
	for _, node := range nodes {
		if node.Name == name && node.Live {
			return true
		}
	}
	return false
}

func chooseCaptureNode(nodes []captureNode, address string) (captureNode, bool) {
	if len(nodes) == 0 {
		return captureNode{}, false
	}
	best := nodes[0]
	bestScore := rendezvous(address, best.Name)
	for _, node := range nodes[1:] {
		score := rendezvous(address, node.Name)
		if score > bestScore {
			best = node
			bestScore = score
		}
	}
	return best, true
}

func rendezvous(address, node string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(address))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(node))
	return h.Sum64()
}

func providerErrorCode(err error) string {
	var perr providerError
	if errors.As(err, &perr) {
		return perr.Code
	}
	return ""
}

func TestFakeProviderRejectsCloudSecondaryIPContracts(t *testing.T) {
	for _, tc := range []struct {
		provider providerKind
		wantCode string
	}{
		{providerAWS, "InvalidParameterValue"},
		{providerAzure, "PrivateIPAddressIsAllocated"},
		{providerOCI, "PrivateIpAlreadyAssigned"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			p := newFakeProvider(tc.provider,
				fakeNIC{ID: "leaf-a", Node: "leaf-a", Site: "aws", Primary: "10.88.60.4/32", MaxSecondary: 2},
				fakeNIC{ID: "leaf-b", Node: "leaf-b", Site: "aws", Primary: "10.88.60.5/32", MaxSecondary: 2},
				fakeNIC{ID: "client", Node: "client", Site: "aws", Primary: "10.88.60.16/32"},
			)
			if err := p.AssignSecondary("leaf-a", "10.88.60.16/32"); providerErrorCode(err) != tc.wantCode {
				t.Fatalf("assign client primary error = %v, want code %s", err, tc.wantCode)
			}
			if err := p.AssignSecondary("leaf-a", "10.88.60.20/32"); err != nil {
				t.Fatalf("assign secondary: %v", err)
			}
			if err := p.AssignSecondary("leaf-b", "10.88.60.20/32"); providerErrorCode(err) != tc.wantCode {
				t.Fatalf("move secondary error = %v, want code %s", err, tc.wantCode)
			}
			if err := p.AssignSecondary("leaf-a", "10.88.60.21/32"); err != nil {
				t.Fatalf("assign second secondary: %v", err)
			}
			if err := p.AssignSecondary("leaf-a", "10.88.60.22/32"); providerErrorCode(err) != "SecondaryLimitExceeded" {
				t.Fatalf("secondary limit error = %v, want SecondaryLimitExceeded", err)
			}
		})
	}
}

func TestFakeProviderAPIReturnsProviderRejectSemantics(t *testing.T) {
	provider := newFakeProvider(providerAzure,
		fakeNIC{ID: "leaf-a", Node: "leaf-a", Site: "azure", Primary: "10.88.60.4/32", MaxSecondary: 4},
		fakeNIC{ID: "client-a", Node: "client-a", Site: "azure", Primary: "10.88.60.16/32"},
	)
	server := httptest.NewServer(provider)
	defer server.Close()

	resp, err := http.Post(server.URL+"/assign-secondary-ip", "application/json", strings.NewReader(`{"nicId":"leaf-a","address":"10.88.60.16/32"}`))
	if err != nil {
		t.Fatalf("POST assign-secondary-ip: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	var body providerActionResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "failed" || body.Code != "PrivateIPAddressIsAllocated" {
		t.Fatalf("response = %#v, want Azure allocated rejection", body)
	}
}

func TestCaptureDistributionTakeoverRejoinAndForceRebalancePoC(t *testing.T) {
	provider := newFakeProvider(providerAzure,
		fakeNIC{ID: "leaf-a", Node: "leaf-a", Site: "azure", Primary: "10.88.60.4/32", MaxSecondary: 16},
		fakeNIC{ID: "leaf-b", Node: "leaf-b", Site: "azure", Primary: "10.88.60.5/32", MaxSecondary: 16},
		fakeNIC{ID: "client-a", Node: "client-a", Site: "azure", Primary: "10.88.60.16/32"},
		fakeNIC{ID: "client-b", Node: "client-b", Site: "azure", Primary: "10.88.60.17/32"},
	)
	nodes := []captureNode{
		{Name: "leaf-a", Site: "azure", NICID: "leaf-a", Live: true},
		{Name: "leaf-b", Site: "azure", NICID: "leaf-b", Live: true},
	}
	captures := []string{
		"10.88.60.20/32", "10.88.60.21/32", "10.88.60.22/32", "10.88.60.23/32",
		"10.88.60.24/32", "10.88.60.25/32", "10.88.60.26/32", "10.88.60.27/32",
	}
	if err := assignDistributedCaptures(provider, nodes, captures, true); err != nil {
		t.Fatalf("baseline assign: %v", err)
	}
	assertBalanced(t, provider.Snapshot(), "leaf-a", "leaf-b")

	nodes[0].Live = false
	if err := assignDistributedCaptures(provider, nodes, captures, true); err != nil {
		t.Fatalf("failover assign: %v", err)
	}
	if got := len(provider.Snapshot()["leaf-b"]); got != len(captures) {
		t.Fatalf("survivor captures = %d, want %d", got, len(captures))
	}

	nodes[0].Live = true
	if err := assignDistributedCaptures(provider, nodes, captures, false); err != nil {
		t.Fatalf("rejoin no-preempt assign: %v", err)
	}
	if got := len(provider.Snapshot()["leaf-b"]); got != len(captures) {
		t.Fatalf("rejoin no-preempt leaf-b captures = %d, want %d", got, len(captures))
	}

	if err := assignDistributedCaptures(provider, nodes, captures, true); err != nil {
		t.Fatalf("force rebalance assign: %v", err)
	}
	assertBalanced(t, provider.Snapshot(), "leaf-a", "leaf-b")
	for _, primary := range []string{"10.88.60.16/32", "10.88.60.17/32"} {
		if owner := provider.secondaryOwner(primary); owner != nil {
			t.Fatalf("client primary %s was captured by %s", primary, owner.ID)
		}
	}
}

func assertBalanced(t *testing.T, snapshot map[string][]string, a, b string) {
	t.Helper()
	diff := len(snapshot[a]) - len(snapshot[b])
	if diff < 0 {
		diff = -diff
	}
	if diff > 1 {
		t.Fatalf("unbalanced captures: %s=%d %s=%d snapshot=%#v", a, len(snapshot[a]), b, len(snapshot[b]), snapshot)
	}
}
