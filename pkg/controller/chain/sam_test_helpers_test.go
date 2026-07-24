// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/sam"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// These fakes are shared with dynamic route tests and intentionally compile on
// every target OS. Linux-only SAM behavior tests remain in sam_test.go.
type fakeSAMApplier struct {
	ensure         []string
	delete         []string
	deassign       []string
	proxyARP       []string
	calls          []string
	forwardSets    [][]sam.CaptureAction
	deassignResult samOSAddressDeassignResult
	ensureErr      error
	deleteErr      error
	deassignErr    error
	forwardErr     error
}

type fakeSAMGARP struct {
	calls []string
	err   error
}

func (g *fakeSAMGARP) SendGratuitousARP(_ context.Context, address, ifname string) error {
	g.calls = append(g.calls, address+"@"+ifname)
	return g.err
}

func (a *fakeSAMApplier) SetProxyARP(_ context.Context, ifname string, enabled bool) error {
	value := "0"
	if enabled {
		value = "1"
	}
	a.proxyARP = append(a.proxyARP, ifname+"="+value)
	a.calls = append(a.calls, "proxyarp:"+ifname+"="+value)
	return nil
}

func (a *fakeSAMApplier) EnsureProxyNeighbor(_ context.Context, address, ifname string) error {
	a.ensure = append(a.ensure, address+"@"+ifname)
	a.calls = append(a.calls, "ensure:"+address+"@"+ifname)
	return a.ensureErr
}

func (a *fakeSAMApplier) DeleteProxyNeighbor(_ context.Context, address, ifname string) error {
	a.delete = append(a.delete, address+"@"+ifname)
	a.calls = append(a.calls, "delete:"+address+"@"+ifname)
	return a.deleteErr
}

func (a *fakeSAMApplier) EnsureOSAddressAbsent(_ context.Context, address string) (samOSAddressDeassignResult, error) {
	a.deassign = append(a.deassign, address)
	a.calls = append(a.calls, "deassign:"+address)
	result := a.deassignResult
	if result.address == "" {
		result.address = address
	}
	return result, a.deassignErr
}

func (a *fakeSAMApplier) ReconcileForwardPaths(_ context.Context, paths []sam.CaptureAction) error {
	a.forwardSets = append(a.forwardSets, append([]sam.CaptureAction(nil), paths...))
	for _, path := range paths {
		a.calls = append(a.calls, "forward:"+path.Address+"@"+path.Interface+"<->"+path.PeerInterface)
	}
	return a.forwardErr
}

func samRemoteAddressClaimStatus(name, address, ifname string) routerstate.ObjectStatus {
	return routerstate.ObjectStatus{
		APIVersion: api.HybridAPIVersion,
		Kind:       "RemoteAddressClaim",
		Name:       name,
		Status: map[string]any{
			"captureProxyNeighbor": map[string]any{"address": address, "interface": ifname},
		},
	}
}

func assertSAMCalls(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("calls = %#v, want %#v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("calls = %#v, want %#v", got, want)
		}
	}
}
