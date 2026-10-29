// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"testing"

	"github.com/imksoo/routerd/pkg/sam"
)

// These fakes are shared with dynamic route tests and intentionally compile on
// every target OS. Linux-only SAM behavior tests remain in sam_test.go.
type fakeSAMApplier struct {
	ensure         []string
	delete         []string
	deassign       []string
	proxyARP       []string
	acceptLocal    []string
	ipForwarding   []string
	calls          []string
	forwardSets    [][]sam.CaptureAction
	ensureErr      error
	deleteErr      error
	onDelete       func()
	deassignErr    error
	forwardErr     error
	proxyARPErr    error
	proxyARPResult *samProxyARPApplyResult
}

type fakeSAMGARP struct {
	calls []string
	err   error
}

func (g *fakeSAMGARP) SendGratuitousARP(_ context.Context, address, ifname string) error {
	g.calls = append(g.calls, address+"@"+ifname)
	return g.err
}

func (a *fakeSAMApplier) SetProxyARP(_ context.Context, ifname string, enabled, owned bool, beforeAcquire func() error) (samProxyARPApplyResult, error) {
	value := "0"
	if enabled {
		value = "1"
	}
	a.proxyARP = append(a.proxyARP, ifname+"="+value)
	a.calls = append(a.calls, "proxyarp:"+ifname+"="+value)
	if a.proxyARPErr != nil {
		return samProxyARPApplyResult{}, a.proxyARPErr
	}
	result := samProxyARPApplyResult{changedByRouterd: enabled && !owned}
	if a.proxyARPResult != nil {
		result = *a.proxyARPResult
	}
	if result.changedByRouterd {
		if err := recordSAMAcquisition(beforeAcquire); err != nil {
			return samProxyARPApplyResult{}, err
		}
	}
	return result, nil
}

func (a *fakeSAMApplier) SetAcceptLocal(_ context.Context, iface string, enabled, owned bool, beforeAcquire func() error) (samProxyARPApplyResult, error) {
	value := "0"
	if enabled {
		value = "1"
	}
	if enabled && !owned {
		if err := recordSAMAcquisition(beforeAcquire); err != nil {
			return samProxyARPApplyResult{}, err
		}
	}
	a.acceptLocal = append(a.acceptLocal, iface+"="+value)
	return samProxyARPApplyResult{changedByRouterd: enabled && !owned}, nil
}

func (a *fakeSAMApplier) SetIPForwarding(_ context.Context, enabled bool) error {
	value := "0"
	if enabled {
		value = "1"
	}
	a.ipForwarding = append(a.ipForwarding, value)
	a.calls = append(a.calls, "ipforward="+value)
	return nil
}

func (a *fakeSAMApplier) EnsureProxyNeighbor(_ context.Context, address, ifname string, owned bool, beforeAcquire func() error) error {
	if !owned {
		if err := recordSAMAcquisition(beforeAcquire); err != nil {
			return err
		}
	}
	a.ensure = append(a.ensure, address+"@"+ifname)
	a.calls = append(a.calls, "ensure:"+address+"@"+ifname)
	return a.ensureErr
}

func (a *fakeSAMApplier) DeleteProxyNeighbor(_ context.Context, address, ifname string) error {
	a.delete = append(a.delete, address+"@"+ifname)
	a.calls = append(a.calls, "delete:"+address+"@"+ifname)
	if a.onDelete != nil {
		a.onDelete()
	}
	return a.deleteErr
}

func (a *fakeSAMApplier) EnsureOSAddressAbsent(_ context.Context, address string) error {
	a.deassign = append(a.deassign, address)
	a.calls = append(a.calls, "deassign:"+address)
	return a.deassignErr
}

func (a *fakeSAMApplier) ReconcileForwardPaths(ctx context.Context, paths []sam.CaptureAction, setAcceptLocal func(context.Context, string, bool) error) error {
	a.forwardSets = append(a.forwardSets, append([]sam.CaptureAction(nil), paths...))
	for _, path := range paths {
		a.calls = append(a.calls, "forward:"+path.Address+"@"+path.Interface+"<->"+path.PeerInterface)
	}
	if a.forwardErr != nil {
		return a.forwardErr
	}
	seen := map[string]bool{}
	for _, path := range paths {
		for _, iface := range []string{path.Interface, path.PeerInterface} {
			if iface != "" && !seen[iface] && setAcceptLocal != nil {
				if err := setAcceptLocal(ctx, iface, true); err != nil {
					return err
				}
				seen[iface] = true
			}
		}
	}
	return nil
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
