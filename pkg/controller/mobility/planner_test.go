// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestPlacementDecision(t *testing.T) {
	base := placementPoolSpec()
	members := plannerMembers(base.Members)
	if got := evaluatePlacement(members["azure-router-a"], members); !got.Active || got.ActiveNode != "azure-router-a" {
		t.Fatalf("router-a placement = %+v, want active", got)
	}
	if got := evaluatePlacement(members["azure-router-b"], members); got.Active || got.ActiveNode != "azure-router-a" {
		t.Fatalf("router-b placement = %+v, want standby", got)
	}
	base.Members[1].Maintenance.Drain = true
	members = plannerMembers(base.Members)
	if got := evaluatePlacement(members["azure-router-b"], members); !got.Active || got.ActiveNode != "azure-router-b" {
		t.Fatalf("router-b after drain = %+v, want active", got)
	}
	base.Members[2].Maintenance.Drain = true
	members = plannerMembers(base.Members)
	if got := evaluatePlacement(members["azure-router-b"], members); got.Active || got.ActiveNode != "" {
		t.Fatalf("all drained placement = %+v, want fail-closed", got)
	}
	ungrouped := plannerMembers(plannedPoolSpec().Members)
	if got := evaluatePlacement(ungrouped["azure-router"], ungrouped); !got.Active || got.ActiveNode != "azure-router" {
		t.Fatalf("ungrouped placement = %+v, want active", got)
	}
}

func plannedPoolSpec() api.MobilityPoolSpec {
	return api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.MobilityPoolMember{
			{
				NodeRef:  "onprem-router",
				Site:     "onprem",
				Role:     "onprem",
				Capture:  api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan"},
				Delivery: api.MobilityMemberDelivery{PeerRef: "azure", Mode: "route", TunnelInterface: "wg-hybrid"},
			},
			{
				NodeRef: "azure-router",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:         "provider-secondary-ip",
					ProviderRef:  "azure-provider",
					ProviderMode: "nic-secondary-ip",
					NICRef:       "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic",
					Target:       map[string]string{"region": "japaneast"},
				},
				Delivery: api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
			},
		},
	}
}

func placementPoolSpec() api.MobilityPoolSpec {
	spec := plannedPoolSpec()
	spec.Members = []api.MobilityPoolMember{
		spec.Members[0],
		{
			NodeRef: "azure-router-a",
			Site:    "azure",
			Role:    "cloud",
			Capture: api.MobilityMemberCapture{
				Type:         "provider-secondary-ip",
				ProviderRef:  "azure-provider",
				ProviderMode: "nic-secondary-ip",
				NICRef:       "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-a",
				Target:       map[string]string{"region": "japaneast", "ipConfigName": "capture-a"},
			},
			Delivery:  api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
			Placement: api.MobilityMemberPlacement{Group: "azure-edge", Priority: 10},
		},
		{
			NodeRef: "azure-router-b",
			Site:    "azure",
			Role:    "cloud",
			Capture: api.MobilityMemberCapture{
				Type:         "provider-secondary-ip",
				ProviderRef:  "azure-provider",
				ProviderMode: "nic-secondary-ip",
				NICRef:       "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-b",
				Target:       map[string]string{"region": "japaneast", "ipConfigName": "capture-b"},
			},
			Delivery:  api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
			Placement: api.MobilityMemberPlacement{Group: "azure-edge", Priority: 20},
		},
	}
	return spec
}

func centralizedOwnershipPoolSpec() api.MobilityPoolSpec {
	spec := placementPoolSpec()
	spec.IPOwnershipPolicy = api.MobilityIPOwnershipPolicy{Type: "centralized"}
	spec.Members[1].Placement.Priority = 10
	spec.Members[2].Placement.Priority = 20
	return spec
}

func awsFailoverPoolSpec() api.MobilityPoolSpec {
	spec := plannedPoolSpec()
	spec.Members = []api.MobilityPoolMember{
		spec.Members[0],
		{
			NodeRef: "aws-router-a",
			Site:    "aws",
			Role:    "cloud",
			Capture: api.MobilityMemberCapture{
				Type:         "provider-secondary-ip",
				ProviderRef:  "aws-provider",
				ProviderMode: "nic-secondary-ip",
				NICRef:       "eni-a",
				Target:       map[string]string{"region": "ap-northeast-1"},
			},
			Delivery:  api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
			Placement: api.MobilityMemberPlacement{Group: "aws-edge", Priority: 10},
		},
		{
			NodeRef: "aws-router-b",
			Site:    "aws",
			Role:    "cloud",
			Capture: api.MobilityMemberCapture{
				Type:         "provider-secondary-ip",
				ProviderRef:  "aws-provider",
				ProviderMode: "nic-secondary-ip",
				NICRef:       "eni-b",
				Target:       map[string]string{"region": "ap-northeast-1"},
			},
			Delivery:  api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
			Placement: api.MobilityMemberPlacement{Group: "aws-edge", Priority: 20},
		},
		{
			NodeRef:  "azure-router",
			Site:     "azure",
			Role:     "cloud",
			Capture:  api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", ProviderMode: "nic-secondary-ip", NICRef: "azure-nic"},
			Delivery: api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
		},
		{
			NodeRef:  "oci-router",
			Site:     "oci",
			Role:     "cloud",
			Capture:  api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "oci-provider", ProviderMode: "vnic-secondary-ip", NICRef: "oci-vnic"},
			Delivery: api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
		},
	}
	spec.IPOwnershipPolicy = api.MobilityIPOwnershipPolicy{Type: "centralized", AutoFailover: true}
	return spec
}

func planningRouter() *api.Router {
	return planningRouterForNode("azure-router", plannedPoolSpec())
}

func planningRouterForNode(nodeName string, spec api.MobilityPoolSpec) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
				Metadata: api.ObjectMeta{Name: "cloudedge"},
				Spec:     api.EventGroupSpec{NodeName: nodeName},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
				Metadata: api.ObjectMeta{Name: "cloudedge"},
				Spec:     spec,
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile"},
				Metadata: api.ObjectMeta{Name: "azure-provider"},
				Spec: api.CloudProviderProfileSpec{
					Provider:       "azure",
					SubscriptionID: "sub-1",
					ResourceGroup:  "rg-router",
					Capabilities:   []string{"nic-secondary-ip", "ip-forwarding"},
					Auth:           api.ProviderAuth{Mode: "external-command", Command: "az"},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile"},
				Metadata: api.ObjectMeta{Name: "aws-provider"},
				Spec: api.CloudProviderProfileSpec{
					Provider:     "aws",
					Capabilities: []string{"nic-secondary-ip", "ip-forwarding"},
					Auth:         api.ProviderAuth{Mode: "external-command", Command: "aws"},
				},
			},
		}},
	}
}

func routerWithBGPRouter(router *api.Router) *api.Router {
	cp := *router
	cp.Spec.Resources = append(append([]api.Resource(nil), router.Spec.Resources...), api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
		Metadata: api.ObjectMeta{Name: "mobility-bgp"},
		Spec:     api.BGPRouterSpec{ASN: 64512, RouterID: "10.99.0.1"},
	})
	return &cp
}

func routerWithEventGroupListen(router *api.Router, address string) *api.Router {
	cp := *router
	cp.Spec.Resources = append([]api.Resource(nil), router.Spec.Resources...)
	for i := range cp.Spec.Resources {
		if cp.Spec.Resources[i].APIVersion != api.FederationAPIVersion || cp.Spec.Resources[i].Kind != "EventGroup" {
			continue
		}
		spec, err := cp.Spec.Resources[i].EventGroupSpec()
		if err != nil {
			continue
		}
		spec.Listen.Address = address
		cp.Spec.Resources[i].Spec = spec
	}
	return &cp
}

func mobilityPoolResource(t *testing.T, router *api.Router, name string) api.Resource {
	t.Helper()
	for _, res := range router.Spec.Resources {
		if res.APIVersion == api.MobilityAPIVersion && res.Kind == "MobilityPool" && res.Metadata.Name == name {
			return res
		}
	}
	t.Fatalf("MobilityPool/%s not found", name)
	return api.Resource{}
}

func saveBGPInstalledNextHops(t *testing.T, store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
}, nextHops map[string][]string) {
	t.Helper()
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{"installedNextHops": nextHops}); err != nil {
		t.Fatalf("SaveObjectStatus(BGPRouter/mobility-bgp): %v", err)
	}
}

func saveBGPStatus(t *testing.T, store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
}, nextHops map[string][]string, prefixes []map[string]any, livenessMarkers map[string]string) {
	t.Helper()
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{"installedNextHops": nextHops, "prefixes": prefixes, "livenessMarkers": livenessMarkers}); err != nil {
		t.Fatalf("SaveObjectStatus(BGPRouter/mobility-bgp): %v", err)
	}
}

func latestPart(t *testing.T, store interface {
	GetDynamicConfigPartsBySource(string) ([]routerstate.DynamicConfigPartRecord, error)
}, source string) routerstate.DynamicConfigPartRecord {
	t.Helper()
	parts, err := store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		t.Fatalf("GetDynamicConfigPartsBySource(%s): %v", source, err)
	}
	if len(parts) == 0 {
		t.Fatalf("GetDynamicConfigPartsBySource(%s) returned no parts", source)
	}
	return parts[0]
}

func findActionPlan(plans []dynamicconfig.ActionPlan, action string) *dynamicconfig.ActionPlan {
	for i := range plans {
		if plans[i].Action == action {
			return &plans[i]
		}
	}
	return nil
}

func findActionPlanByAddress(plans []dynamicconfig.ActionPlan, action, address string) *dynamicconfig.ActionPlan {
	for i := range plans {
		if plans[i].Action == action && plans[i].Target["address"] == address {
			return &plans[i]
		}
	}
	return nil
}

func decodeResources(t *testing.T, raw string) []api.Resource {
	t.Helper()
	var resources []api.Resource
	if err := json.Unmarshal([]byte(raw), &resources); err != nil {
		t.Fatalf("decode resources: %v raw=%s", err, raw)
	}
	return resources
}

func decodeActionPlans(t *testing.T, raw string) []dynamicconfig.ActionPlan {
	t.Helper()
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var plans []dynamicconfig.ActionPlan
	if err := json.Unmarshal([]byte(raw), &plans); err != nil {
		t.Fatalf("decode action plans: %v raw=%s", err, raw)
	}
	return plans
}

func importApprovedAction(t *testing.T, plan *dynamicconfig.ActionPlan, source string, store *routerstate.SQLiteStore, now time.Time) (int64, error) {
	t.Helper()
	targetJSON, err := json.Marshal(plan.Target)
	if err != nil {
		return 0, err
	}
	paramsJSON, err := json.Marshal(plan.Parameters)
	if err != nil {
		return 0, err
	}
	_, err = store.ImportAction(routerstate.ActionExecutionRecord{
		IdempotencyKey: plan.IdempotencyKey,
		Source:         source,
		Provider:       plan.Provider,
		ProviderRef:    plan.ProviderRef,
		Action:         plan.Action,
		TargetJSON:     string(targetJSON),
		ParametersJSON: string(paramsJSON),
		RiskLevel:      plan.RiskLevel,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		return 0, err
	}
	rows, err := store.ListActions(routerstate.ActionExecutionFilter{})
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		if row.IdempotencyKey != plan.IdempotencyKey {
			continue
		}
		if err := store.ApproveAction(row.ID, "test", now); err != nil {
			return 0, err
		}
		return row.ID, nil
	}
	return 0, fmt.Errorf("imported action %q not found", plan.IdempotencyKey)
}

func seedSucceededBGPCaptureAction(t *testing.T, store *routerstate.SQLiteStore, providerRef, nicRef, holder, address, action string, epoch int64, at time.Time) {
	t.Helper()
	_ = epoch
	pathSig := "prefix=" + normalizeAddressString(address) + ";seeded=true"
	targetJSON, err := json.Marshal(map[string]string{"address": address, "nicRef": nicRef, "providerRef": providerRef})
	if err != nil {
		t.Fatalf("marshal target: %v", err)
	}
	paramsJSON, err := json.Marshal(map[string]string{
		bgpPathSigParam:     pathSig,
		captureParamHolder:  holder,
		"mobilityPathSigID": bgpPathSigHash(pathSig),
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	key := strings.Join([]string{"test", providerRef, nicRef, action, address, "pathsig", bgpPathSigHash(pathSig), fmt.Sprint(at.UnixNano())}, ":")
	if _, err := store.ImportAction(routerstate.ActionExecutionRecord{
		IdempotencyKey: key,
		Source:         "test",
		Provider:       strings.TrimSuffix(providerRef, "-provider"),
		ProviderRef:    providerRef,
		Action:         action,
		TargetJSON:     string(targetJSON),
		ParametersJSON: string(paramsJSON),
		Status:         routerstate.ActionPending,
	}); err != nil {
		t.Fatalf("ImportAction: %v", err)
	}
	rec, ok, err := store.GetActionByIdempotencyKey(key)
	if err != nil || !ok {
		t.Fatalf("GetActionByIdempotencyKey: ok=%v err=%v", ok, err)
	}
	if err := store.ApproveAction(rec.ID, "test", at.Add(-time.Second)); err != nil {
		t.Fatalf("ApproveAction: %v", err)
	}
	claimed, err := store.BeginActionExecution(rec.ID, at.Add(-500*time.Millisecond))
	if err != nil || !claimed {
		t.Fatalf("BeginActionExecution: claimed=%v err=%v", claimed, err)
	}
	if err := store.MarkActionResult(rec.ID, routerstate.ActionSucceeded, "ok", "", nil, at); err != nil {
		t.Fatalf("MarkActionResult: %v", err)
	}
}

func countKind(resources []api.Resource, kind string) int {
	count := 0
	for _, res := range resources {
		if strings.EqualFold(res.Kind, kind) {
			count++
		}
	}
	return count
}
