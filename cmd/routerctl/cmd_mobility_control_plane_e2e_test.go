// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	mobilitycontroller "github.com/imksoo/routerd/pkg/controller/mobility"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerplugin "github.com/imksoo/routerd/pkg/plugin"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"gopkg.in/yaml.v3"
)

func TestMobilityFourNodeControlPlaneDynamicRender(t *testing.T) {
	now := time.Now().UTC()
	fixture := fourNodeMobilityFixture(now)
	for _, node := range fixture.nodes {
		t.Run(node.nodeRef, func(t *testing.T) {
			tmp := t.TempDir()
			statePath := filepath.Join(tmp, "routerd.db")
			store, err := routerstate.OpenSQLite(statePath)
			if err != nil {
				t.Fatalf("OpenSQLite: %v", err)
			}
			defer func() {
				if store != nil {
					_ = store.Close()
				}
			}()
			for _, event := range fixture.events {
				if err := store.RecordFederationEvent(event); err != nil {
					t.Fatalf("RecordFederationEvent(%s): %v", event.ID, err)
				}
			}
			router := fourNodeMobilityRouter(node.nodeRef, fixture)
			controller := mobilitycontroller.Controller{Router: router, Store: store, Now: func() time.Time { return now }}
			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatalf("mobility Reconcile: %v", err)
			}

			source := mobilitycontroller.DynamicSource("cloudedge", node.nodeRef)
			parts, err := store.GetDynamicConfigPartsBySource(source)
			if err != nil {
				t.Fatalf("GetDynamicConfigPartsBySource: %v", err)
			}
			if len(parts) != 1 || parts[0].Generation != 1 {
				t.Fatalf("parts = %+v, want one generation-1 part", parts)
			}
			resources := decodeMobilityDynamicResources(t, parts[0].ResourcesJSON)
			claims := claimsByAddress(t, resources)
			if len(claims) != len(fixture.nodes)-1 {
				t.Fatalf("claims = %d, want %d: %+v", len(claims), len(fixture.nodes)-1, claims)
			}
			if _, found := claims[node.address]; found {
				t.Fatalf("self-owned address %s must not be claimed by %s", node.address, node.nodeRef)
			}
			assertExpectedDeliveries(t, node, claims)

			var plans []dynamicconfig.ActionPlan
			if strings.TrimSpace(parts[0].ActionPlansJSON) != "" {
				if err := json.Unmarshal([]byte(parts[0].ActionPlansJSON), &plans); err != nil {
					t.Fatalf("decode action plans: %v", err)
				}
			}
			assertExpectedActionPlans(t, node, plans)

			configPath := filepath.Join(tmp, "router.yaml")
			writeRouterYAML(t, configPath, router)
			if err := store.Close(); err != nil {
				t.Fatalf("close store before render: %v", err)
			}
			store = nil

			var rendered bytes.Buffer
			if err := run([]string{
				"dynamic", "render",
				"--config", configPath,
				"--state-file", statePath,
				"-o", "yaml",
			}, &rendered, &bytes.Buffer{}); err != nil {
				t.Fatalf("dynamic render: %v\n%s", err, rendered.String())
			}
			output := rendered.String()
			for address := range claims {
				for _, want := range []string{
					"kind: RemoteAddressClaim",
					"address: " + address,
					"routerd.net/dynamic-source: " + source,
				} {
					if !strings.Contains(output, want) {
						t.Fatalf("dynamic render output missing %q\n---\n%s", want, output)
					}
				}
			}
		})
	}
}

type mobilityFixture struct {
	nodes  []mobilityFixtureNode
	events []routerstate.EventRecord
}

type mobilityFixtureNode struct {
	nodeRef         string
	site            string
	role            string
	address         string
	capture         api.MobilityMemberCapture
	delivery        api.MobilityMemberDelivery
	deliveryTo      []api.MobilityMemberDeliveryTarget
	providerProfile api.CloudProviderProfileSpec
}

func fourNodeMobilityFixture(now time.Time) mobilityFixture {
	nodes := []mobilityFixtureNode{
		{
			nodeRef: "onprem-router",
			site:    "onprem",
			role:    "onprem",
			address: "10.88.60.10/32",
			capture: api.MobilityMemberCapture{
				Type:      "proxy-arp",
				Interface: "lan",
				ActiveWhen: api.CaptureActiveWhen{
					Type:              "vrrp-master",
					VirtualAddressRef: "onprem-vip",
				},
			},
			deliveryTo: []api.MobilityMemberDeliveryTarget{
				{NodeRef: "aws-router", PeerRef: "aws-main", Mode: "route", TunnelInterface: "wg-aws"},
				{NodeRef: "azure-router", PeerRef: "azure-main", Mode: "route", TunnelInterface: "wg-azure"},
				{NodeRef: "oci-router", PeerRef: "oci-main", Mode: "route", TunnelInterface: "wg-oci"},
			},
		},
		{
			nodeRef:  "aws-router",
			site:     "aws",
			role:     "cloud",
			address:  "10.88.60.11/32",
			capture:  api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "aws-provider", ProviderMode: "eni-secondary-ip", NICRef: "eni-aws-router", Target: map[string]string{"region": "ap-northeast-1"}},
			delivery: api.MobilityMemberDelivery{PeerRef: "onprem-main", Mode: "route", TunnelInterface: "wg-onprem"},
			providerProfile: api.CloudProviderProfileSpec{
				Provider:     "aws",
				Capabilities: []string{"nic-secondary-ip", "ip-forwarding"},
				Auth:         api.ProviderAuth{Mode: "external-command", Command: "/bin/true"},
			},
		},
		{
			nodeRef:  "azure-router",
			site:     "azure",
			role:     "cloud",
			address:  "10.88.60.12/32",
			capture:  api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", ProviderMode: "nic-secondary-ip", NICRef: "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/azure-router-nic", Target: map[string]string{"region": "japaneast", "ipConfigName": "mobility-capture"}},
			delivery: api.MobilityMemberDelivery{PeerRef: "onprem-main", Mode: "route", TunnelInterface: "wg-onprem"},
			providerProfile: api.CloudProviderProfileSpec{
				Provider:       "azure",
				SubscriptionID: "sub-1",
				ResourceGroup:  "rg-router",
				Capabilities:   []string{"nic-secondary-ip", "ip-forwarding"},
				Auth:           api.ProviderAuth{Mode: "external-command", Command: "/bin/true"},
			},
		},
		{
			nodeRef:  "oci-router",
			site:     "oci",
			role:     "cloud",
			address:  "10.88.60.13/32",
			capture:  api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "oci-provider", ProviderMode: "vnic-private-ip", NICRef: "ocid1.vnic.oc1.example", Target: map[string]string{"region": "ap-tokyo-1", "compartmentId": "ocid1.compartment.oc1.example"}},
			delivery: api.MobilityMemberDelivery{PeerRef: "onprem-main", Mode: "route", TunnelInterface: "wg-onprem"},
			providerProfile: api.CloudProviderProfileSpec{
				Provider:     "oci",
				Capabilities: []string{"nic-secondary-ip", "ip-forwarding"},
				Auth:         api.ProviderAuth{Mode: "external-command", Command: "/bin/true"},
			},
		},
	}
	events := make([]routerstate.EventRecord, 0, len(nodes))
	observedAt := now.Add(-time.Minute)
	for i, node := range nodes {
		events = append(events, routerstate.EventRecord{
			ID:         "evt-" + node.nodeRef,
			Group:      "cloudedge",
			SourceNode: node.nodeRef,
			Type:       mobilitycontroller.ObservedEventType,
			Subject:    node.address,
			Payload:    map[string]string{"address": node.address},
			DedupeKey:  node.nodeRef,
			ObservedAt: observedAt.Add(time.Duration(i) * time.Second),
			ExpiresAt:  observedAt.Add(10 * time.Minute),
		})
	}
	return mobilityFixture{nodes: nodes, events: events}
}

func fourNodeMobilityRouter(selfNode string, fixture mobilityFixture) *api.Router {
	resources := []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
			Metadata: api.ObjectMeta{Name: "cloudedge"},
			Spec: api.EventGroupSpec{
				NodeName: selfNode,
				Listen:   api.EventGroupListen{Address: "169.254.200.1", Port: 8787},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "lan", Managed: true},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "onprem-vip"},
			Spec: api.VirtualAddressSpec{
				Family:    "ipv4",
				Interface: "lan",
				Address:   "10.88.60.1/32",
				Mode:      "vrrp",
				VRRP:      api.VirtualAddressVRRPSpec{VirtualRouterID: 60, Peers: []string{"10.88.60.2"}},
			},
		},
	}
	for i, node := range fixture.nodes {
		if node.nodeRef == selfNode {
			continue
		}
		resources = append(resources, api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventPeer"},
			Metadata: api.ObjectMeta{Name: "peer-" + node.nodeRef},
			Spec: api.EventPeerSpec{
				GroupRef: "cloudedge",
				NodeName: node.nodeRef,
				Endpoint: fmt.Sprintf("http://169.254.200.%d:8787", i+2),
				Types:    []string{mobilitycontroller.ObservedEventType, mobilitycontroller.ExpiredEventType},
			},
		})
	}
	for _, peer := range []struct {
		name      string
		role      string
		nodeID    string
		ifaceName string
	}{
		{name: "onprem-main", role: "onprem", nodeID: "onprem-router", ifaceName: "wg-onprem"},
		{name: "aws-main", role: "cloud", nodeID: "aws-router", ifaceName: "wg-aws"},
		{name: "azure-main", role: "cloud", nodeID: "azure-router", ifaceName: "wg-azure"},
		{name: "oci-main", role: "cloud", nodeID: "oci-router", ifaceName: "wg-oci"},
	} {
		resources = append(resources, api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"},
			Metadata: api.ObjectMeta{Name: peer.name},
			Spec: api.OverlayPeerSpec{
				Role:   peer.role,
				NodeID: peer.nodeID,
				Underlay: api.OverlayUnderlay{
					Type:      "wireguard",
					Interface: peer.ifaceName,
				},
			},
		})
	}
	members := make([]api.MobilityPoolMember, 0, len(fixture.nodes))
	for _, node := range fixture.nodes {
		members = append(members, api.MobilityPoolMember{
			NodeRef:    node.nodeRef,
			Site:       node.site,
			Role:       node.role,
			Capture:    node.capture,
			Delivery:   node.delivery,
			DeliveryTo: node.deliveryTo,
		})
		if strings.TrimSpace(node.providerProfile.Provider) != "" {
			resources = append(resources, api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile"},
				Metadata: api.ObjectMeta{Name: node.capture.ProviderRef},
				Spec:     node.providerProfile,
			})
		}
	}
	resources = append(resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
		Metadata: api.ObjectMeta{Name: "cloudedge"},
		Spec: api.MobilityPoolSpec{
			Prefix:   "10.88.60.0/24",
			GroupRef: "cloudedge",
			Members:  members,
			LeasePolicy: api.MobilityLeasePolicy{
				TTL:          "5m",
				HoldDuration: "30s",
			},
			CapturePolicy: api.MobilityCapturePolicy{Mode: "all-non-owner-sites"},
		},
	})
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "router-" + selfNode},
		Spec:     api.RouterSpec{Resources: resources},
	}
}

func assertExpectedDeliveries(t *testing.T, node mobilityFixtureNode, claims map[string]api.RemoteAddressClaimSpec) {
	t.Helper()
	if node.nodeRef == "onprem-router" {
		want := map[string]api.AddressDelivery{
			"10.88.60.11/32": {PeerRef: "aws-main", Mode: "route", TunnelInterface: "wg-aws"},
			"10.88.60.12/32": {PeerRef: "azure-main", Mode: "route", TunnelInterface: "wg-azure"},
			"10.88.60.13/32": {PeerRef: "oci-main", Mode: "route", TunnelInterface: "wg-oci"},
		}
		for address, delivery := range want {
			got := claims[address].Delivery
			if got.PeerRef != delivery.PeerRef || got.TunnelInterface != delivery.TunnelInterface {
				t.Fatalf("%s delivery for %s = %+v, want %+v", node.nodeRef, address, got, delivery)
			}
		}
		return
	}
	for address, claim := range claims {
		if claim.Delivery.PeerRef != "onprem-main" || claim.Delivery.TunnelInterface != "wg-onprem" {
			t.Fatalf("%s delivery for %s = %+v, want onprem fallback", node.nodeRef, address, claim.Delivery)
		}
	}
}

func assertExpectedActionPlans(t *testing.T, node mobilityFixtureNode, plans []dynamicconfig.ActionPlan) {
	t.Helper()
	if node.role != "cloud" {
		if len(plans) != 0 {
			t.Fatalf("%s actionPlans = %+v, want none", node.nodeRef, plans)
		}
		return
	}
	if len(plans) != 4 {
		t.Fatalf("%s actionPlans = %d, want 4 (3 assign + 1 forwarding): %+v", node.nodeRef, len(plans), plans)
	}
	assigns := 0
	forwarding := 0
	for _, plan := range plans {
		if err := routerplugin.ValidateActionPlan(plan); err != nil {
			t.Fatalf("ValidateActionPlan(%s): %v", plan.Name, err)
		}
		if plan.Action == "ensure-forwarding-enabled" {
			forwarding++
			if !strings.HasPrefix(plan.Target["address"], "10.88.60.") {
				t.Fatalf("%s forwarding target address = %q, want pool representative address", node.nodeRef, plan.Target["address"])
			}
			if plan.Undo == nil || plan.Undo.Parameters["address"] != plan.Target["address"] {
				t.Fatalf("%s forwarding undo must carry target address, plan=%+v", node.nodeRef, plan)
			}
			continue
		}
		if plan.Action != "assign-secondary-ip" {
			continue
		}
		assigns++
		for key, value := range node.capture.Target {
			if plan.Target[key] != value {
				t.Fatalf("%s assign target[%s]=%q want %q full=%+v", node.nodeRef, key, plan.Target[key], value, plan.Target)
			}
		}
		if plan.Target["nicRef"] != node.capture.NICRef || plan.Target["providerRef"] != node.capture.ProviderRef {
			t.Fatalf("%s assign target missing capture base fields: %+v", node.nodeRef, plan.Target)
		}
	}
	if assigns != 3 {
		t.Fatalf("%s assign actionPlans = %d, want 3", node.nodeRef, assigns)
	}
	if forwarding != 1 {
		t.Fatalf("%s forwarding actionPlans = %d, want 1", node.nodeRef, forwarding)
	}
}

func decodeMobilityDynamicResources(t *testing.T, raw string) []api.Resource {
	t.Helper()
	var resources []api.Resource
	if err := json.Unmarshal([]byte(raw), &resources); err != nil {
		t.Fatalf("decode resources: %v raw=%s", err, raw)
	}
	return resources
}

func claimsByAddress(t *testing.T, resources []api.Resource) map[string]api.RemoteAddressClaimSpec {
	t.Helper()
	out := map[string]api.RemoteAddressClaimSpec{}
	for _, res := range resources {
		if res.Kind != "RemoteAddressClaim" {
			continue
		}
		spec, err := res.RemoteAddressClaimSpec()
		if err != nil {
			t.Fatalf("RemoteAddressClaimSpec(%s): %v", res.Metadata.Name, err)
		}
		out[spec.Address] = spec
	}
	return out
}

func writeRouterYAML(t *testing.T, path string, router *api.Router) {
	t.Helper()
	data, err := yaml.Marshal(router)
	if err != nil {
		t.Fatalf("marshal router yaml: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write router yaml: %v", err)
	}
}
