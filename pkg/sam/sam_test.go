// SPDX-License-Identifier: BSD-3-Clause

package sam

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestEvaluateCaptureGateUsesTypedObservation(t *testing.T) {
	capture := api.MobilityMemberCapture{ActiveWhen: api.CaptureActiveWhen{
		Type:              "vrrp-master",
		VirtualAddressRef: "VirtualAddress/lan-vip",
	}}
	for _, tt := range []struct {
		name        string
		observation CaptureGateObservation
		active      bool
		reason      string
		message     string
	}{
		{
			name: "master", observation: CaptureGateObservation{VirtualAddressStatusAvailable: true, VirtualAddressRole: "master"},
			active: true, reason: "VRRPMaster",
		},
		{
			name: "backup", observation: CaptureGateObservation{VirtualAddressStatusAvailable: true, VirtualAddressRole: "backup"},
			reason: "CaptureGateInactive", message: "VirtualAddress is not VRRP master",
		},
		{
			name: "unavailable", observation: CaptureGateObservation{},
			reason: "CaptureGateInactive", message: "capture activeWhen requires an available vrrp-master VirtualAddress",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateCaptureGate(capture, tt.observation)
			if got.Active != tt.active || got.Reason != tt.reason || got.Message != tt.message || got.VirtualAddressRef != "lan-vip" {
				t.Fatalf("EvaluateCaptureGate() = %#v", got)
			}
		})
	}
}

func TestPlanLocalCaptureIntentsLowersProviderCaptureDirectly(t *testing.T) {
	actions, err := PlanLocalCaptureIntents([]dynamicconfig.LocalCaptureIntent{{
		ID: "cloud/10.88.60.9", PoolRef: "cloud", PoolPrefix: "10.88.60.0/24", Address: "10.88.60.9/32",
		Disposition: dynamicconfig.CaptureDesired, CaptureType: "provider-secondary-ip",
		CaptureInterface: "ens3", TunnelInterfaces: []string{"wg-cloud"}, GratuitousARP: true,
	}}, platform.OSLinux)
	if err != nil {
		t.Fatal(err)
	}
	if !captureActionPresent(actions, "deassign-os-address", "", "10.88.60.9/32", "") ||
		!captureActionPresent(actions, "proxy-neighbor", "", "10.88.60.9/32", "ens3") ||
		!captureActionPresent(actions, "forward-path", "", "10.88.60.9/32", "ens3") {
		t.Fatalf("actions = %#v", actions)
	}
}

func TestPlanLocalCaptureIntentsLowersProxyARPCaptureWithTunnelForwarding(t *testing.T) {
	actions, err := PlanLocalCaptureIntents([]dynamicconfig.LocalCaptureIntent{{
		ID: "svnet1/192.168.123.111", PoolRef: "svnet1", PoolPrefix: "192.168.123.0/24", Address: "192.168.123.111/32",
		Disposition: dynamicconfig.CaptureDesired, CaptureType: "proxy-arp",
		CaptureInterface: "ens19", TunnelInterfaces: []string{"samt-rr-a", "samt-rr-b"},
	}}, platform.OSLinux)
	if err != nil {
		t.Fatal(err)
	}
	if !captureActionPresent(actions, "proxy-neighbor", "", "192.168.123.111/32", "ens19") ||
		!captureActionPresent(actions, "forward-local-path", "", "192.168.123.111/32", "ens19") {
		t.Fatalf("actions = %#v", actions)
	}
	var tunnels []string
	for _, action := range actions {
		if action.Kind == "forward-local-path" {
			tunnels = append(tunnels, action.PeerInterface)
		}
	}
	if len(tunnels) != 2 || tunnels[0] != "samt-rr-a" || tunnels[1] != "samt-rr-b" {
		t.Fatalf("forward-local tunnels = %#v", tunnels)
	}
}

func TestPlanLocalCaptureIntentsDoesNotApplyHeldCapture(t *testing.T) {
	actions, err := PlanLocalCaptureIntents([]dynamicconfig.LocalCaptureIntent{{
		ID: "cloud/10.88.60.9", PoolRef: "cloud", PoolPrefix: "10.88.60.0/24", Address: "10.88.60.9/32", Disposition: dynamicconfig.CaptureHold,
		CaptureType: "proxy-arp", CaptureInterface: "lan0",
	}}, platform.OSLinux)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 0 {
		t.Fatalf("held intent actions = %#v", actions)
	}
}

func TestPlanLocalCaptureIntentsLowersProviderReleaseToOSDeassign(t *testing.T) {
	actions, err := PlanLocalCaptureIntents([]dynamicconfig.LocalCaptureIntent{{
		ID: "cloud/10.88.60.9", PoolRef: "cloud", PoolPrefix: "10.88.60.0/24", Address: "10.88.60.9/32", Disposition: dynamicconfig.CaptureRelease,
		CaptureType: "provider-secondary-ip",
	}}, platform.OSLinux)
	if err != nil {
		t.Fatal(err)
	}
	if !captureActionPresent(actions, "deassign-os-address", "", "10.88.60.9/32", "") {
		t.Fatalf("release actions = %#v", actions)
	}
	if captureActionPresent(actions, "proxy-neighbor", "", "10.88.60.9/32", "") {
		t.Fatalf("release unexpectedly captures: %#v", actions)
	}
}

func TestPlanLocalCaptureIntentsRejectsUnknownDispositionOrCaptureType(t *testing.T) {
	for _, intent := range []dynamicconfig.LocalCaptureIntent{
		{ID: "cloud/10.88.60.9", PoolRef: "cloud", PoolPrefix: "10.88.60.0/24", Address: "10.88.60.9/32", Disposition: "unknown", CaptureType: "proxy-arp", CaptureInterface: "lan0"},
		{ID: "cloud/10.88.60.9", PoolRef: "cloud", PoolPrefix: "10.88.60.0/24", Address: "10.88.60.9/32", Disposition: dynamicconfig.CaptureDesired, CaptureType: "unknown", CaptureInterface: "lan0"},
		{ID: "cloud/10.88.60.9", PoolRef: "cloud", PoolPrefix: "10.88.60.0/24", Address: "10.88.60.9/32", Disposition: dynamicconfig.CaptureRelease, CaptureType: "unknown"},
	} {
		if actions, err := PlanLocalCaptureIntents([]dynamicconfig.LocalCaptureIntent{intent}, platform.OSLinux); err == nil || len(actions) != 0 {
			t.Fatalf("PlanLocalCaptureIntents(%#v) = %#v, %v; want failure without actions", intent, actions, err)
		}
	}
}

func captureActionPresent(actions []CaptureAction, kind, key, address, iface string) bool {
	for _, action := range actions {
		if action.Kind == kind && (key == "" || action.Key == key) && action.Address == address && (iface == "" || action.Interface == iface) {
			return true
		}
	}
	return false
}
