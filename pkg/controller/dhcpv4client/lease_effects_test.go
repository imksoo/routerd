// SPDX-License-Identifier: BSD-3-Clause

package dhcpv4client

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func effectController(command outputCommandFunc) *Controller {
	return &Controller{Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.DHCPv4ClientSpec{Interface: "eth0", RouteMetric: 100, UseDNS: boolPtr(false)}},
	}}}, Store: mapStore{}, Command: command}
}

func TestApplyWithdrawalRetainsFailedOwnedEffects(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	var calls []string
	failAddress := true
	c := effectController(func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		calls = append(calls, line)
		switch line {
		case "ip -4 route show default":
			return []byte("default via 192.0.2.1 dev eth0 metric 100\ndefault via 198.51.100.1 dev other metric 200\n"), nil
		case "ip -4 addr show dev eth0":
			return []byte("inet 192.0.2.10/24 scope global eth0\ninet 192.0.2.200/24 scope global eth0\n"), nil
		case "ip -4 addr del 192.0.2.10/24 dev eth0":
			if failAddress {
				return nil, errors.New("busy")
			}
		}
		return nil, nil
	})
	current := map[string]any{"ifname": "eth0", "appliedAddress": "192.0.2.10/24", "appliedDefaultGateway": "192.0.2.1", "appliedRouteMetric": 100}
	next := map[string]any{"phase": daemonapi.ResourcePhaseExpired}
	if err := c.applyLease(context.Background(), "wan", current, next); err == nil {
		t.Fatal("withdrawal failure was hidden")
	}
	if mapString(next, "appliedAddress") != "192.0.2.10/24" || mapString(next, "appliedDefaultGateway") != "" {
		t.Fatalf("partial effects lost ownership: %#v", next)
	}
	for _, call := range calls {
		if strings.Contains(call, "del") && (strings.Contains(call, "198.51.100") || strings.Contains(call, "192.0.2.200")) {
			t.Fatalf("deleted foreign resource: %s", call)
		}
	}
	failAddress = false
	calls = nil
	retry := map[string]any{"phase": daemonapi.ResourcePhaseReleased}
	if err := c.applyLease(context.Background(), "wan", next, retry); err != nil {
		t.Fatal(err)
	}
	if mapString(retry, "appliedAddress") != "" {
		t.Fatal("successful withdrawal retained ownership")
	}
	if strings.Join(calls, "\n") != "ip -4 addr show dev eth0\nip -4 addr del 192.0.2.10/24 dev eth0" {
		t.Fatalf("retry did not resume just failed effect: %v", calls)
	}
}

func TestApplyDoesNotOwnPreexistingMatchingResources(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	var calls []string
	c := effectController(func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		calls = append(calls, line)
		if strings.Contains(line, "addr show") {
			return []byte("inet 192.0.2.10/24 scope global eth0\n"), nil
		}
		if strings.Contains(line, "route show") {
			return []byte("default via 192.0.2.1 dev eth0 metric 100\n"), nil
		}
		return nil, errors.New("unexpected mutation")
	})
	next := map[string]any{"phase": daemonapi.ResourcePhaseBound, "interface": "eth0", "currentAddress": "192.0.2.10", "prefixLength": 24, "defaultGateway": "192.0.2.1"}
	if err := c.applyLease(context.Background(), "wan", nil, next); err != nil {
		t.Fatal(err)
	}
	if mapString(next, "appliedAddress") != "" || mapString(next, "appliedDefaultGateway") != "" {
		t.Fatalf("claimed foreign matching resources: %#v", next)
	}
	calls = nil
	if err := c.applyLease(context.Background(), "wan", next, map[string]any{"phase": daemonapi.ResourcePhaseExpired}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("touched resources it never created: %v", calls)
	}
}

type failedEffectStore struct {
	err   error
	saved mapStore
}

func (s *failedEffectStore) ObjectStatus(v, k, n string) map[string]any {
	return s.saved.ObjectStatus(v, k, n)
}
func (s *failedEffectStore) SaveObjectStatus(v, k, n string, status map[string]any) error {
	if s.err != nil {
		return s.err
	}
	return s.saved.SaveObjectStatus(v, k, n, status)
}

func TestApplyPersistsIntentBeforeKernelAndRecoversPending(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	store := &failedEffectStore{err: errors.New("storage unavailable"), saved: mapStore{}}
	var mutations int
	present := false
	c := effectController(func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		if strings.Contains(line, "addr show") {
			if present {
				return []byte("inet 192.0.2.10/24 scope global eth0\n"), nil
			}
			return nil, nil
		}
		if strings.Contains(line, "addr add") {
			mutations++
			saved := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
			if mapString(saved, "pendingAddress") != "192.0.2.10/24" || mapString(saved, "appliedAddress") != "" {
				t.Fatalf("effect preceded durable intent: %#v", saved)
			}
			present = true
			return nil, nil
		}
		return nil, nil
	})
	c.Store = store
	next := map[string]any{"phase": daemonapi.ResourcePhaseBound, "currentAddress": "192.0.2.10", "prefixLength": 24}
	if err := c.applyLease(context.Background(), "wan", nil, next); err == nil || mutations != 0 {
		t.Fatal("wrote kernel after intent persistence failed")
	}
	store.err = nil
	if err := c.applyLease(context.Background(), "wan", nil, next); err != nil {
		t.Fatal(err)
	}
	// Simulate a stop after the kernel effect and before final status save.
	pending := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
	recovered := map[string]any{"phase": daemonapi.ResourcePhaseBound, "currentAddress": "192.0.2.10", "prefixLength": 24}
	if err := c.applyLease(context.Background(), "wan", pending, recovered); err != nil {
		t.Fatal(err)
	}
	if mutations != 1 || mapString(recovered, "appliedAddress") != "192.0.2.10/24" || mapString(recovered, "pendingAddress") != "" {
		t.Fatalf("pending effect recovery failed: %#v mutations %d", recovered, mutations)
	}
}

func TestApplyObservationFailureKeepsOwnership(t *testing.T) {
	c := effectController(func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("observation failed") })
	current := map[string]any{"ifname": "eth0", "appliedAddress": "192.0.2.10/24"}
	next := map[string]any{"phase": daemonapi.ResourcePhaseExpired}
	if err := c.applyLease(context.Background(), "wan", current, next); err == nil {
		t.Fatal("failed observation fabricated absence")
	}
	if mapString(next, "appliedAddress") == "" {
		t.Fatal("failed observation discarded ownership")
	}
}

func TestDefaultRouteMetricAndFreeBSDWithdrawal(t *testing.T) {
	if dhcpv4DefaultRouteLineMatches("default via 192.0.2.1 dev eth0 metric 100", "eth0", "192.0.2.1", 0) {
		t.Fatal("metric zero matched foreign priority")
	}
	if !dhcpv4DefaultRouteLineMatches("default via 192.0.2.1 dev eth0", "eth0", "192.0.2.1", 0) {
		t.Fatal("default metric did not match zero")
	}
	var calls []string
	command := func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		calls = append(calls, line)
		return []byte("Destination Gateway Flags Netif\ndefault 192.0.2.1 UGS em0\ndefault 198.51.100.1 UGS em1\n"), nil
	}
	present, err := dhcpv4DefaultRoutePresent(context.Background(), platform.OSFreeBSD, command, "em0", "192.0.2.1", 0)
	if err != nil || !present {
		t.Fatalf("FreeBSD observation failed: %v %v", present, err)
	}
	if err := removeDefaultRoute(context.Background(), platform.OSFreeBSD, command, "em0", "192.0.2.1", 0); err != nil {
		t.Fatal(err)
	}
	if calls[len(calls)-1] != "route -n delete -inet default 192.0.2.1 -ifp em0" {
		t.Fatal(calls)
	}
}

func TestApplyExpiredLeaseWithdrawsWhenDaemonUnavailable(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	var removed bool
	c := effectController(func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		if strings.Contains(line, "addr show") {
			return []byte("inet 192.0.2.10/24 scope global eth0\n"), nil
		}
		if line == "ip -4 addr del 192.0.2.10/24 dev eth0" {
			removed = true
			return nil, nil
		}
		return nil, errors.New("unexpected command")
	})
	c.DaemonSockets = map[string]string{"wan": filepath.Join(t.TempDir(), "absent.sock")}
	c.Store = mapStore{api.NetAPIVersion + "/DHCPv4Client/wan": {"phase": daemonapi.ResourcePhaseBound, "ifname": "eth0", "appliedAddress": "192.0.2.10/24", "expiresAt": time.Unix(1, 0).UTC().Format(time.RFC3339Nano)}}
	if err := c.Reconcile(context.Background(), "wan"); err == nil {
		t.Fatal("unavailable daemon was reported healthy")
	}
	status, readErr := c.Store.ReadObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !removed || mapString(status, "appliedAddress") != "" || mapString(status, "phase") != daemonapi.ResourcePhaseExpired {
		t.Fatalf("dead worker extended expired authority: %#v", status)
	}
}

func TestApplyRecoversDNSAfterEffectBeforeStatusSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	c := effectController(func(context.Context, string, ...string) ([]byte, error) { return nil, nil })
	c.Router.Spec.Resources[0].Spec = api.DHCPv4ClientSpec{Interface: "eth0", UseRoutes: boolPtr(false)}
	c.ResolvConfPath = path
	next := map[string]any{"phase": daemonapi.ResourcePhaseBound, "currentAddress": "192.0.2.10", "prefixLength": 24, "dnsServers": []string{"192.0.2.53"}}
	if err := c.applyLease(context.Background(), "wan", nil, next); err != nil {
		t.Fatal(err)
	}
	// The store retains the pre-DNS effect intent, as if the final status write
	// failed or the process stopped after writing the managed resolver file.
	pending, readErr := c.Store.ReadObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
	if readErr != nil {
		t.Fatal(readErr)
	}
	if mapString(pending, "pendingDNSServers") != "192.0.2.53" || mapString(pending, "appliedDNSServers") != "" {
		t.Fatalf("DNS intent was not durable before effect: %#v", pending)
	}
	withdrawn := map[string]any{"phase": daemonapi.ResourcePhaseExpired}
	if err := c.applyLease(context.Background(), "wan", pending, withdrawn); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "nameserver") || mapString(withdrawn, "appliedDNSServers") != "" || mapString(withdrawn, "pendingDNSServers") != "" {
		t.Fatalf("crash left DNS authority installed: %s %#v", data, withdrawn)
	}
}

func TestApplyRouteWithdrawalFailureDoesNotDelayAddressWithdrawal(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	removed := false
	c := effectController(func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		switch line {
		case "ip -4 route show default":
			return []byte("default via 192.0.2.1 dev eth0 metric 100\n"), nil
		case "ip -4 route del default via 192.0.2.1 dev eth0 metric 100":
			return nil, errors.New("route busy")
		case "ip -4 addr show dev eth0":
			return []byte("inet 192.0.2.10/24 scope global eth0\n"), nil
		case "ip -4 addr del 192.0.2.10/24 dev eth0":
			removed = true
			return nil, nil
		}
		return nil, errors.New("unexpected command")
	})
	current := map[string]any{"ifname": "eth0", "appliedAddress": "192.0.2.10/24", "appliedDefaultGateway": "192.0.2.1", "appliedRouteMetric": 100}
	next := map[string]any{"phase": daemonapi.ResourcePhaseExpired}
	if err := c.applyLease(context.Background(), "wan", current, next); err == nil {
		t.Fatal("route error was hidden")
	}
	if !removed || mapString(next, "appliedAddress") != "" || mapString(next, "appliedDefaultGateway") != "192.0.2.1" {
		t.Fatalf("independent cleanup was lost: %#v", next)
	}
}

func TestApplyResolvedWithdrawalPreservesOtherLinkSettings(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	path := filepath.Join(t.TempDir(), "resolv.conf")
	if err := os.Symlink("/run/systemd/resolve/stub-resolv.conf", path); err != nil {
		t.Fatal(err)
	}
	var calls []string
	command := func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		calls = append(calls, line)
		switch line {
		case "resolvectl dns eth0":
			return []byte("Link 2 (eth0): 192.0.2.53\n"), nil
		case "resolvectl domain eth0":
			return []byte("Link 2 (eth0): ~. corp.test\n"), nil
		}
		return nil, nil
	}
	if err := withdrawDNS(context.Background(), platform.OSLinux, command, "eth0", path, "wan", "192.0.2.53"); err != nil {
		t.Fatal(err)
	}
	want := "resolvectl dns eth0\nresolvectl domain eth0\nresolvectl domain eth0 corp.test\nresolvectl dns eth0 "
	if strings.Join(calls, "\n") != want {
		t.Fatalf("unrelated link settings were reset: %v", calls)
	}
}

func (s *failedEffectStore) ListObjectStatuses() ([]routerstate.ObjectStatus, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.saved.ListObjectStatuses()
}
func (s *failedEffectStore) DeleteObject(v, k, n string) error {
	if s.err != nil {
		return s.err
	}
	return s.saved.DeleteObject(v, k, n)
}

func (s *failedEffectStore) ReadObjectStatus(v, k, n string) (map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.saved.ReadObjectStatus(v, k, n)
}
