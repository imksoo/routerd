// SPDX-License-Identifier: BSD-3-Clause

package vrrp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type mapStore map[string]map[string]any

func (s mapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s mapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if status := s[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

type statefulMapStore struct {
	mapStore
	values map[string]routerstate.Value
	now    time.Time
}

func (s statefulMapStore) Get(name string) routerstate.Value {
	if value, ok := s.values[name]; ok {
		return value
	}
	return routerstate.Value{Status: routerstate.StatusUnknown, Since: s.Now(), UpdatedAt: s.Now()}
}

func (s statefulMapStore) Age(name string) time.Duration { return s.Now().Sub(s.Get(name).Since) }
func (s statefulMapStore) Now() time.Time                { return s.now }

func TestSyncFailoverVMACFollowsObservedVRRPRole(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
		Metadata: api.ObjectMeta{Name: "lan-gw"},
		Spec: api.VirtualAddressSpec{Interface: "lan", Address: "172.18.0.1/32", Family: "ipv4", Mode: "vrrp", VRRP: api.VirtualAddressVRRPSpec{
			VirtualRouterID: 18,
			FailoverVMAC:    &api.VirtualAddressVRRPFailoverVMACSpec{ParentInterface: "wan", Interface: "wan-vmac", MACAddress: "02:00:5e:00:01:13"},
		}},
	}}}}
	var calls []string
	controller := &Controller{Router: router, Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}}
	aliases := map[string]string{"lan": "ens19", "wan": "ens18"}
	if err := syncFailoverVMACs(context.Background(), controller, aliases, map[string]string{"lan-gw": "master"}); err != nil {
		t.Fatalf("sync master: %v", err)
	}
	if err := syncFailoverVMACs(context.Background(), controller, aliases, map[string]string{"lan-gw": "backup"}); err != nil {
		t.Fatalf("sync backup: %v", err)
	}
	want := []string{
		"/usr/local/sbin/routerd-vrrp-vmac activate --parent ens18 --interface wan-vmac --mac 02:00:5e:00:01:13",
		"/usr/local/sbin/routerd-vrrp-vmac deactivate --parent ens18 --interface wan-vmac --mac 02:00:5e:00:01:13",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("VMAC sync calls = %#v, want %#v", calls, want)
	}
}

func TestSyncFailoverVMACKeepsWANAndLANInOneRoleTransition(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
		Metadata: api.ObjectMeta{Name: "lan-gw"},
		Spec: api.VirtualAddressSpec{Interface: "lan", Address: "172.18.0.1/32", Family: "ipv4", Mode: "vrrp", VRRP: api.VirtualAddressVRRPSpec{
			VirtualRouterID: 18,
			FailoverVMAC:    &api.VirtualAddressVRRPFailoverVMACSpec{ParentInterface: "wan", Interface: "wan-vmac", MACAddress: "02:00:5e:00:01:13"},
			AdditionalFailoverVMACs: []api.VirtualAddressVRRPFailoverVMACSpec{{
				ParentInterface: "lan", Interface: "lan-vrrp", MACAddress: "02:00:5e:00:01:12", LinkLocalAddress: "fe80::5eff:fe00:112", WithdrawRouterAdvertisement: true,
			}},
		}},
	}}}}
	var calls []string
	controller := &Controller{Router: router, Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}}
	aliases := map[string]string{"lan": "ens19", "wan": "ens18"}
	if err := syncFailoverVMACs(context.Background(), controller, aliases, map[string]string{"lan-gw": "master"}); err != nil {
		t.Fatalf("sync master: %v", err)
	}
	if err := syncFailoverVMACs(context.Background(), controller, aliases, map[string]string{"lan-gw": "backup"}); err != nil {
		t.Fatalf("sync backup: %v", err)
	}
	want := []string{
		"/usr/local/sbin/routerd-vrrp-vmac activate --vmac ens18,wan-vmac,02:00:5e:00:01:13,,false --vmac ens19,lan-vrrp,02:00:5e:00:01:12,fe80::5eff:fe00:112,true",
		"/usr/local/sbin/routerd-vrrp-vmac deactivate --vmac ens18,wan-vmac,02:00:5e:00:01:13,,false --vmac ens19,lan-vrrp,02:00:5e:00:01:12,fe80::5eff:fe00:112,true",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("VMAC sync calls = %#v, want %#v", calls, want)
	}
}

func (s mapStore) ListObjectStatuses() ([]routerstate.ObjectStatus, error) {
	var out []routerstate.ObjectStatus
	for key, status := range s {
		parts := strings.Split(key, "/")
		if len(parts) != 4 {
			continue
		}
		out = append(out, routerstate.ObjectStatus{APIVersion: parts[0] + "/" + parts[1], Kind: parts[2], Name: parts[3], Status: status})
	}
	return out, nil
}

func TestReconcileLowersVRRPPriorityAfterTrackHysteresis(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/BGPRouter/lan": {"phase": "Degraded"},
	}
	var calls []string
	controller := Controller{
		Router:     vrrpRouter("vrrp"),
		Store:      store,
		DryRun:     false,
		ConfigPath: t.TempDir() + "/keepalived.conf",
		Systemctl:  "systemctl",
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip")
	if status["priority"] != 150 {
		t.Fatalf("priority should not drop before confirm threshold: %#v", status)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("third reconcile: %v", err)
	}
	if len(calls) == 0 {
		t.Fatal("expected keepalived reload calls")
	}
	status = store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip")
	if status["priority"] != 100 {
		t.Fatalf("priority status = %#v", status)
	}
}

func TestReconcileRestoresVRRPPriorityAfterHealthyHysteresis(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/BGPRouter/lan": {"phase": "Degraded"},
	}
	controller := Controller{Router: vrrpRouter("vrrp"), Store: store, DryRun: true, ConfigPath: t.TempDir() + "/keepalived.conf"}
	for i := 0; i < 3; i++ {
		if err := controller.Reconcile(context.Background()); err != nil {
			t.Fatalf("unhealthy reconcile %d: %v", i, err)
		}
	}
	store[api.NetAPIVersion+"/BGPRouter/lan"] = map[string]any{"phase": "Established"}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first healthy reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip")
	if status["priority"] != 100 {
		t.Fatalf("priority should remain penalized before healthy threshold: %#v", status)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second healthy reconcile: %v", err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip")
	if status["priority"] != 150 {
		t.Fatalf("priority should restore after healthy threshold: %#v", status)
	}
}

func TestReconcileRestoresTrackHysteresisFromStore(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/BGPRouter/lan": {"phase": "Degraded"},
		api.NetAPIVersion + "/VirtualAddress/vip": {
			"track": []map[string]any{{
				"resource":       "BGPRouter/lan",
				"penalized":      true,
				"healthyCount":   0,
				"unhealthyCount": 3,
			}},
		},
	}
	controller := Controller{Router: vrrpRouter("vrrp"), Store: store, DryRun: true, ConfigPath: t.TempDir() + "/keepalived.conf"}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip")
	if status["priority"] != 100 {
		t.Fatalf("priority should stay penalized after restart restore: %#v", status)
	}
	track, ok := status["track"].([]map[string]any)
	if !ok || len(track) != 1 || track[0]["unhealthyCount"] != 4 {
		t.Fatalf("track state was not restored and advanced: %#v", status["track"])
	}
}

func TestReconcilePenalizesUnhealthyTrackedResourceWhenWhenMatches(t *testing.T) {
	router := vrrpRouter("vrrp")
	spec, _ := router.Spec.Resources[1].VirtualAddressSpec()
	spec.Track = []api.ResourceTrackSpec{{
		Resource:                    "HealthCheck/internet-via-dslite",
		UnhealthyPenalty:            80,
		ConfirmConsecutiveUnhealthy: 1,
	}}
	router.Spec.Resources[1].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
		Metadata: api.ObjectMeta{Name: "internet-via-dslite"},
		Spec: api.HealthCheckSpec{
			When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
				"VirtualAddress/vip.role": {Equals: "master"},
			}},
			Target: "1.1.1.1",
		},
	})
	store := mapStore{
		api.NetAPIVersion + "/VirtualAddress/vip": {"role": "master"},
		api.NetAPIVersion + "/HealthCheck/internet-via-dslite": {
			"phase": "Unhealthy",
		},
	}
	controller := Controller{Router: router, Store: store, DryRun: true, ConfigPath: t.TempDir() + "/keepalived.conf"}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip")
	if status["priority"] != 70 {
		t.Fatalf("matching unhealthy track must apply penalty: %#v", status)
	}
}

func TestVRRPWhenStoreDelegatesRouterState(t *testing.T) {
	now := time.Date(2026, 7, 30, 20, 0, 0, 0, time.UTC)
	store := statefulMapStore{mapStore: mapStore{}, now: now, values: map[string]routerstate.Value{
		"cluster.role": {Status: routerstate.StatusSet, Value: "master", Since: now.Add(-time.Minute), UpdatedAt: now},
	}}
	when := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"cluster.role": {Equals: "master"}}}
	if !resourcequery.ResourceWhenMatches(when, newVRRPWhenStore(store)) {
		t.Fatal("VRRP when store must delegate router state")
	}
}

func TestReconcileDoesNotPenalizeTrackedResourceWhenWhenIsFalse(t *testing.T) {
	router := vrrpRouter("vrrp")
	spec, _ := router.Spec.Resources[1].VirtualAddressSpec()
	spec.Track = []api.ResourceTrackSpec{{
		Resource:                    "HealthCheck/internet-via-dslite",
		UnhealthyPenalty:            80,
		ConfirmConsecutiveUnhealthy: 1,
	}}
	router.Spec.Resources[1].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
		Metadata: api.ObjectMeta{Name: "internet-via-dslite"},
		Spec: api.HealthCheckSpec{
			When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
				"VirtualAddress/vip.role": {Equals: "master"},
			}},
			Target: "1.1.1.1",
		},
	})
	store := mapStore{
		api.NetAPIVersion + "/VirtualAddress/vip": {
			"role": "backup",
			"track": []map[string]any{{
				"resource": "HealthCheck/internet-via-dslite", "healthyCount": 0, "unhealthyCount": 2, "penalized": false,
			}},
		},
		api.NetAPIVersion + "/HealthCheck/internet-via-dslite": {"phase": "Unhealthy"},
	}
	controller := Controller{Router: router, Store: store, DryRun: true, ConfigPath: t.TempDir() + "/keepalived.conf"}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip")
	if status["priority"] != 150 {
		t.Fatalf("when false track must not lower priority: %#v", status)
	}
	track, ok := status["track"].([]map[string]any)
	if !ok || len(track) != 1 || track[0]["unhealthyCount"] != 2 || track[0]["penalized"] != false {
		t.Fatalf("neutral track must preserve counters: %#v", status["track"])
	}
}

func TestReconcileDoesNotPenalizePendingWhenFalseTrack(t *testing.T) {
	router := vrrpRouter("vrrp")
	spec, _ := router.Spec.Resources[1].VirtualAddressSpec()
	spec.Track = []api.ResourceTrackSpec{{
		Resource:                    "HealthCheck/internet-via-dslite",
		UnhealthyPenalty:            80,
		ConfirmConsecutiveUnhealthy: 1,
	}}
	router.Spec.Resources[1].Spec = spec
	store := mapStore{
		api.NetAPIVersion + "/HealthCheck/internet-via-dslite": {"phase": "Pending", "reason": "WhenFalse"},
	}
	controller := Controller{Router: router, Store: store, DryRun: true, ConfigPath: t.TempDir() + "/keepalived.conf"}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip")
	if status["priority"] != 150 {
		t.Fatalf("Pending/WhenFalse track must not lower priority: %#v", status)
	}
}

func TestReconcileAppliesStaticVirtualAddressIPv4(t *testing.T) {
	store := mapStore{}
	var calls []string
	controller := Controller{
		Router:          vrrpRouter("static"),
		Store:           store,
		IP:              "ip",
		OperatingSystem: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []string{"ip addr replace 10.240.70.10/32 dev ens18"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestReconcileAnnouncesRestoredStaticVirtualAddressIPv4(t *testing.T) {
	router := vrrpRouter("static")
	spec, _ := router.Spec.Resources[1].VirtualAddressSpec()
	spec.GratuitousARP = true
	router.Spec.Resources[1].Spec = spec
	store := mapStore{
		api.NetAPIVersion + "/VirtualAddress/vip": {
			"phase": "Pending", "reason": "WhenFalse", "ifname": "ens18", "address": "10.240.70.10/32", "staticAddressRemoved": true,
		},
	}
	var calls []string
	controller := Controller{
		Router:          router,
		Store:           store,
		IP:              "ip",
		Arping:          "arping",
		OperatingSystem: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := name + " " + strings.Join(args, " ")
			calls = append(calls, call)
			if call == "ip -4 -o addr show dev ens18" {
				return nil, nil
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []string{
		"ip -4 -o addr show dev ens18",
		"ip addr replace 10.240.70.10/32 dev ens18",
		"arping -U -c 3 -I ens18 10.240.70.10",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip")
	if status["phase"] != "Applied" || status["appliedAddress"] != "10.240.70.10/32" {
		t.Fatalf("status = %#v, want Applied restored address", status)
	}
}

func TestReconcileDoesNotRepeatStaticVirtualAddressAnnouncement(t *testing.T) {
	router := vrrpRouter("static")
	spec, _ := router.Spec.Resources[1].VirtualAddressSpec()
	spec.GratuitousARP = true
	router.Spec.Resources[1].Spec = spec
	store := mapStore{}
	var calls []string
	controller := Controller{
		Router:          router,
		Store:           store,
		IP:              "ip",
		Arping:          "arping",
		OperatingSystem: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := name + " " + strings.Join(args, " ")
			calls = append(calls, call)
			if call == "ip -4 -o addr show dev ens18" {
				return []byte("2: ens18 inet 10.240.70.10/32 scope global ens18\n"), nil
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []string{
		"ip -4 -o addr show dev ens18",
		"ip addr replace 10.240.70.10/32 dev ens18",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want no arping: %#v", calls, want)
	}
}

func TestReconcileReportsStaticVirtualAddressAnnouncementFailure(t *testing.T) {
	router := vrrpRouter("static")
	spec, _ := router.Spec.Resources[1].VirtualAddressSpec()
	spec.GratuitousARP = true
	router.Spec.Resources[1].Spec = spec
	store := mapStore{}
	arpingAttempts := 0
	controller := Controller{
		Router:          router,
		Store:           store,
		IP:              "ip",
		Arping:          "arping",
		OperatingSystem: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := name + " " + strings.Join(args, " ")
			if call == "ip -4 -o addr show dev ens18" {
				return nil, nil
			}
			if name == "arping" {
				arpingAttempts++
				if arpingAttempts == 1 {
					return []byte("send failed"), errors.New("exit 1")
				}
				return []byte("sent"), nil
			}
			return []byte("ok"), nil
		},
	}
	err := controller.Reconcile(context.Background())
	if err == nil || !strings.Contains(err.Error(), "arping -U -c 3 -I ens18 10.240.70.10") {
		t.Fatalf("error = %v, want arping failure", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip")
	if status["phase"] != "Error" || status["reason"] != "StaticVIPGratuitousARPFailed" {
		t.Fatalf("status = %#v, want explicit GARP failure", status)
	}
	controller.Command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		if call == "ip -4 -o addr show dev ens18" {
			return []byte("2: ens18 inet 10.240.70.10/32 scope global ens18\n"), nil
		}
		if name == "arping" {
			arpingAttempts++
			return []byte("sent"), nil
		}
		return []byte("ok"), nil
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("retry reconcile: %v", err)
	}
	if arpingAttempts != 2 {
		t.Fatalf("arping attempts = %d, want failed send plus retry despite present address", arpingAttempts)
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip"); status["phase"] != "Applied" {
		t.Fatalf("retry status = %#v, want Applied", status)
	}
}

func TestReconcileDoesNotAnnounceWhenStaticVirtualAddressIsFilteredOut(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/VirtualAddress/wan-nat-v4": {
			"phase": "Pending", "reason": "WhenFalse", "ifname": "ens18", "address": "192.168.1.249/32", "staticAddressRemoved": true,
		},
	}
	controller := Controller{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "ens18"},
		}}}},
		Store:           store,
		ConfigPath:      t.TempDir() + "/missing-keepalived.conf",
		OperatingSystem: platform.OSLinux,
		Command: func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("BACKUP effective config must not run static address or arping commands")
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
}

func TestReconcileKeepsFreeBSDStaticVirtualAddressCommandUnchangedWithGARPOptIn(t *testing.T) {
	router := vrrpRouter("static")
	spec, _ := router.Spec.Resources[1].VirtualAddressSpec()
	spec.GratuitousARP = true
	router.Spec.Resources[1].Spec = spec
	var calls []string
	controller := Controller{
		Router:          router,
		Store:           mapStore{},
		Ifconfig:        "ifconfig",
		OperatingSystem: platform.OSFreeBSD,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []string{"ifconfig ens18 inet 10.240.70.10/32 alias"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want FreeBSD address operation only %#v", calls, want)
	}
}

func TestReconcileIsolatesUnresolvedStaticVirtualAddress(t *testing.T) {
	store := mapStore{}
	var calls []string
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "ens18"},
		},
		{
			// A static address source that exists in config but has no address
			// yet (dynamically assigned): the VIP must wait as Pending.
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
			Metadata: api.ObjectMeta{Name: "dyn-src"},
			Spec:     api.IPv4StaticAddressSpec{Interface: "lan"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "pending-vip"},
			Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface:   "lan",
				Mode:        "static",
				AddressFrom: api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/dyn-src", Field: "address"},
			},
		},
		{
			// References a resource absent from config (a typo): a real
			// misconfiguration, reported as Error, not Pending.
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "error-vip"},
			Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface:   "lan",
				Mode:        "static",
				AddressFrom: api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/missing", Field: "address"},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "good-vip"},
			Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface: "lan",
				Address:   "10.240.70.20/32",
				Mode:      "static",
			},
		},
	}}}
	controller := Controller{
		Router:          router,
		Store:           store,
		IP:              "ip",
		OperatingSystem: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	pending := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "pending-vip")
	if pending["phase"] != "Pending" || pending["reason"] != "AddressUnresolved: IPv4StaticAddress/dyn-src" {
		t.Fatalf("pending VIP status = %#v", pending)
	}
	errored := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "error-vip")
	if errored["phase"] != "Error" {
		t.Fatalf("error VIP status = %#v", errored)
	}
	good := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "good-vip")
	if good["phase"] != "Applied" || good["appliedAddress"] != "10.240.70.20/32" {
		t.Fatalf("good VIP status = %#v", good)
	}
	want := []string{"ip addr replace 10.240.70.20/32 dev ens18"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestReconcileObservesVRRPRoleFromVIPAddress(t *testing.T) {
	store := mapStore{}
	controller := Controller{
		Router:          vrrpRouter("vrrp"),
		Store:           store,
		ConfigPath:      t.TempDir() + "/keepalived.conf",
		Systemctl:       "systemctl",
		IP:              "ip",
		OperatingSystem: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "ip" && strings.Join(args, " ") == "-4 addr show dev ens18" {
				return []byte("2: ens18 inet 10.240.70.10/32 scope global ens18\n"), nil
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip")
	if status["role"] != "master" {
		t.Fatalf("role = %#v, status=%#v", status["role"], status)
	}
	firstTransition := statusString(status, "lastRoleTransitionAt")
	if firstTransition == "" {
		t.Fatalf("lastRoleTransitionAt missing: %#v", status)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip")
	if status["lastRoleTransitionAt"] != firstTransition {
		t.Fatalf("lastRoleTransitionAt changed without role change: %#v", status)
	}
}

func TestReconcileRestartsInactiveSystemdKeepalived(t *testing.T) {
	store := mapStore{}
	active := true
	var calls []string
	controller := Controller{
		Router:              vrrpRouter("vrrp"),
		Store:               store,
		ConfigPath:          t.TempDir() + "/keepalived.conf",
		Systemctl:           "systemctl",
		IP:                  "ip",
		OperatingSystem:     platform.OSLinux,
		KeepalivedActiveTTL: -1,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			if name == "systemctl" && strings.Join(args, " ") == "is-active --quiet keepalived.service" {
				if active {
					return []byte("active"), nil
				}
				return []byte("inactive"), errors.New("inactive")
			}
			if name == "systemctl" && strings.Join(args, " ") == "restart keepalived.service" {
				active = true
				return []byte("ok"), nil
			}
			if name == "ip" && strings.Join(args, " ") == "-4 addr show dev ens18" {
				return []byte("2: ens18 inet 10.240.70.10/32 scope global ens18\n"), nil
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	active = false
	calls = nil
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if !containsString(calls, "systemctl restart keepalived.service") {
		t.Fatalf("missing systemd keepalived restart: %#v", calls)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip")
	if status["role"] != "master" || status["serviceActive"] != true {
		t.Fatalf("restarted keepalived status = %#v, want role master and serviceActive true", status)
	}
	if statusString(status, "lastRestartAt") == "" || statusString(status, "lastChangeReason") != "keepalived.service inactive" {
		t.Fatalf("restart metadata missing: %#v", status)
	}
}

func TestReconcileCachesKeepalivedActiveStatus(t *testing.T) {
	activeChecks := 0
	controller := Controller{
		Systemctl: "systemctl",
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			line := name + " " + strings.Join(args, " ")
			if line == "systemctl is-active --quiet keepalived.service" {
				activeChecks++
				return []byte("active"), nil
			}
			return []byte("ok"), nil
		},
	}
	if !controller.keepalivedServiceActive(context.Background()) {
		t.Fatalf("initial active check returned false")
	}
	if !controller.keepalivedServiceActive(context.Background()) {
		t.Fatalf("cached active check returned false")
	}
	if activeChecks != 1 {
		t.Fatalf("active checks = %d, want 1", activeChecks)
	}
	controller.keepalivedActiveCheckedAt = time.Now().Add(-time.Minute)
	if !controller.keepalivedServiceActive(context.Background()) {
		t.Fatalf("expired active check returned false")
	}
	if activeChecks != 2 {
		t.Fatalf("active checks after expiry = %d, want 2", activeChecks)
	}
}

func TestReconcileAppliesFreeBSDCARPVirtualAddressIPv4(t *testing.T) {
	store := mapStore{}
	var calls []string
	controller := Controller{
		Router:          vrrpRouter("vrrp"),
		Store:           store,
		OperatingSystem: platform.OSFreeBSD,
		Ifconfig:        "ifconfig",
		Sysctl:          "sysctl",
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			if name == "ifconfig" && len(args) == 1 && args[0] == "ens18" {
				return []byte("ens18: flags=...\n\tcarp: MASTER vhid 50 advbase 1 advskew 104\n"), nil
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, want := range []string{
		"kldload carp",
		"sysctl net.inet.carp.preempt=0",
		"ifconfig ens18 inet vhid 50 advbase 1 advskew 104 alias 10.240.70.10/32",
		"ifconfig ens18",
	} {
		if !containsString(calls, want) {
			t.Fatalf("calls missing %q: %#v", want, calls)
		}
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip")
	if status["backend"] != "carp" || status["role"] != "master" {
		t.Fatalf("unexpected CARP status: %#v", status)
	}
}

func TestReconcileSkipsNoopKeepalivedReloadWithSystemd(t *testing.T) {
	store := mapStore{}
	var calls []string
	controller := Controller{
		Router:          vrrpRouter("vrrp"),
		Store:           store,
		ConfigPath:      t.TempDir() + "/keepalived.conf",
		Systemctl:       "systemctl",
		IP:              "ip",
		OperatingSystem: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			if name == "ip" && strings.Join(args, " ") == "-4 addr show dev ens18" {
				return []byte("2: ens18 inet 10.240.70.10/32 scope global ens18\n"), nil
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !containsString(calls, "systemctl reload keepalived.service") {
		t.Fatalf("missing initial systemd reload: %#v", calls)
	}
	calls = nil
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	for _, unwanted := range []string{"systemctl reload keepalived.service", "systemctl restart keepalived.service", "systemctl reload-or-restart keepalived.service"} {
		if containsString(calls, unwanted) {
			t.Fatalf("no-op reconcile called %q: %#v", unwanted, calls)
		}
	}
}

func TestReconcileCleansRemovedStaticVirtualAddressIPv4(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/VirtualAddress/old": {
			"backend":        "iproute2",
			"ifname":         "ens18",
			"appliedAddress": "10.240.70.99/32",
		},
	}
	var calls []string
	controller := Controller{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens18"},
			},
		}}},
		Store:           store,
		IP:              "ip",
		OperatingSystem: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []string{"ip addr del 10.240.70.99/32 dev ens18"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "old")
	if status["phase"] != "Removed" || status["appliedAddress"] != "" {
		t.Fatalf("stale VIP status was not cleared: %#v", status)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("second reconcile repeated cleanup: calls = %#v, want %#v", calls, want)
	}
}

func TestReconcileCleansStaticVirtualAddressWhenConditionBecomesFalse(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/VirtualAddress/wan-nat-v4": {
			"phase":     "Pending",
			"reason":    "WhenFalse",
			"interface": "wan",
			"address":   "192.168.1.249/32",
		},
	}
	var calls []string
	controller := Controller{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18"},
			},
		}}},
		Store:           store,
		IP:              "ip",
		OperatingSystem: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []string{"ip addr del 192.168.1.249/32 dev ens18"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "wan-nat-v4")
	if status["phase"] != "Pending" || status["reason"] != "WhenFalse" || status["appliedAddress"] != "" || status["staticAddressRemoved"] != true {
		t.Fatalf("stale WhenFalse VIP status was not cleared: %#v", status)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("second reconcile repeated cleanup: calls = %#v, want %#v", calls, want)
	}
}

func TestReconcileStopsKeepalivedWhenVRRPRemoved(t *testing.T) {
	store := mapStore{}
	dir := t.TempDir()
	configPath := filepath.Join(dir, "keepalived.conf")
	if err := os.WriteFile(configPath, []byte("vrrp_instance old {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	controller := Controller{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens18"},
			},
		}}},
		Store:      store,
		ConfigPath: configPath,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []string{"systemctl is-active --quiet keepalived.service", "systemctl stop keepalived.service"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestObserveKeepalivedRolesPrefersKernelVIPOverCachedInactiveService(t *testing.T) {
	controller := &Controller{
		Router:                    vrrpRouter("vrrp"),
		keepalivedActiveCached:    false,
		keepalivedActiveCheckedAt: time.Now(),
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "ip" && reflect.DeepEqual(args, []string{"-4", "addr", "show", "dev", "ens18"}) {
				return []byte("    inet 10.240.70.10/32 scope global\n"), nil
			}
			if name == "systemctl" {
				t.Fatalf("MASTER VIP must not consult cached systemd activity: %v", args)
			}
			return nil, nil
		},
	}
	if got := observeKeepalivedRoles(context.Background(), controller, map[string]string{"lan": "ens18"}); got["vip"] != "master" {
		t.Fatalf("roles = %#v, want kernel MASTER", got)
	}
}

func TestReconcilePublishesRoleBeforeVMACRepairFailure(t *testing.T) {
	store := mapStore{}
	router := vrrpRouter("vrrp")
	spec, _ := router.Spec.Resources[1].VirtualAddressSpec()
	spec.VRRP.FailoverVMAC = &api.VirtualAddressVRRPFailoverVMACSpec{ParentInterface: "lan", Interface: "lan-vrrp", MACAddress: "02:00:5e:00:01:12"}
	router.Spec.Resources[1].Spec = spec
	controller := &Controller{Router: router, Store: store, OperatingSystem: platform.OSLinux, ConfigPath: filepath.Join(t.TempDir(), "keepalived.conf"), Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "/usr/local/sbin/routerd-vrrp-vmac" {
			return []byte("repair failed"), errors.New("exit status 1")
		}
		if name == "ip" {
			return []byte("    inet 10.240.70.10/32 scope global\n"), nil
		}
		return nil, nil
	}}
	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("expected VMAC repair error")
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip")
	if status["role"] != "master" || !strings.Contains(statusString(status, "vmacRepairError"), "repair failed") {
		t.Fatalf("status must publish role and repair error: %#v", status)
	}
}

func vrrpRouter(mode string) *api.Router {
	track := []api.ResourceTrackSpec(nil)
	vrrpSpec := api.VirtualAddressVRRPSpec{}
	if mode == "vrrp" {
		track = []api.ResourceTrackSpec{{Resource: "BGPRouter/lan", UnhealthyPenalty: 50}}
		vrrpSpec = api.VirtualAddressVRRPSpec{VirtualRouterID: 50, Priority: 150, Peers: []string{"10.240.70.3"}}
	}
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "ens18"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "vip"},
			Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Mode:      mode,
				VRRP:      vrrpSpec,
				Track:     track,
			},
		},
	}}}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
