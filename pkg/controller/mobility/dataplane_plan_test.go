// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"net/netip"
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

func TestPlanCapturePrefixEffectsEmitsTypedSourceAndExcludedPrefixRoutes(t *testing.T) {
	in := bgpDeliveryPlannerInput{PoolRuntimeSnapshot: PoolRuntimeSnapshot{Pool: NormalizedMobilityPool{
		Name:                 "cloudedge",
		SelfCaptureInterface: "lan0",
		Spec:                 api.MobilityPoolSpec{Prefix: "10.77.60.0/29"},
		Prefix:               netip.MustParsePrefix("10.77.60.0/29"),
		Self: memberPlanInfo{
			Capture: api.MobilityMemberCapture{
				Type:             "proxy-arp",
				ExcludeAddresses: []string{"10.77.60.0/31", "10.77.60.7/32"},
			},
			CaptureSourceAddress: "10.77.60.1",
		},
	}}}
	captures := []dynamicconfig.LocalCaptureIntent{{
		ID:          "mobility-cloudedge-10-77-60-2",
		PoolRef:     "cloudedge",
		Address:     "10.77.60.2/32",
		Disposition: dynamicconfig.CaptureDesired,
		CaptureType: "proxy-arp",
	}}

	routes, staticAddresses := planCapturePrefixEffects(in, in.Pool.Prefix, captures)
	if got, want := staticAddresses, []dynamicconfig.MobilityIPv4AddressIntent{{
		ID:        "mobility-cloudedge-capture-source",
		PoolRef:   "cloudedge",
		Purpose:   dynamicconfig.MobilityIPv4AddressPurposeCaptureSource,
		Interface: "lan0",
		Address:   "10.77.60.1/32",
	}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("static address intents = %#v, want %#v", got, want)
	}
	wantRoutes := []dynamicconfig.MobilityIPv4RouteIntent{
		{
			ID:              "mobility-cloudedge-capture-10-77-60-2-31",
			PoolRef:         "cloudedge",
			Purpose:         dynamicconfig.MobilityIPv4RoutePurposeCapturePrefix,
			Destination:     "10.77.60.2/31",
			Device:          "lan0",
			PreferredSource: "10.77.60.1",
			Metric:          90,
		},
		{
			ID:              "mobility-cloudedge-capture-10-77-60-4-31",
			PoolRef:         "cloudedge",
			Purpose:         dynamicconfig.MobilityIPv4RoutePurposeCapturePrefix,
			Destination:     "10.77.60.4/31",
			Device:          "lan0",
			PreferredSource: "10.77.60.1",
			Metric:          90,
		},
		{
			ID:              "mobility-cloudedge-capture-10-77-60-6-32",
			PoolRef:         "cloudedge",
			Purpose:         dynamicconfig.MobilityIPv4RoutePurposeCapturePrefix,
			Destination:     "10.77.60.6/32",
			Device:          "lan0",
			PreferredSource: "10.77.60.1",
			Metric:          90,
		},
	}
	if !reflect.DeepEqual(routes, wantRoutes) {
		t.Fatalf("capture prefix route intents = %#v, want %#v", routes, wantRoutes)
	}
}

func TestPlanCapturePrefixEffectsDoesNotReinterpretHoldOrExternalSourceOwnership(t *testing.T) {
	in := bgpDeliveryPlannerInput{PoolRuntimeSnapshot: PoolRuntimeSnapshot{Pool: NormalizedMobilityPool{
		Name:                 "cloudedge",
		SelfCaptureInterface: "lan0",
		Spec:                 api.MobilityPoolSpec{Prefix: "10.77.60.0/24"},
		Prefix:               netip.MustParsePrefix("10.77.60.0/24"),
		Self: memberPlanInfo{
			Capture:                  api.MobilityMemberCapture{Type: "proxy-arp"},
			CaptureSourceAddress:     "10.77.60.1",
			CaptureSourceAddressFrom: api.StatusValueSourceSpec{Resource: "DHCPv4Client/lan"},
		},
	}}}
	hold := []dynamicconfig.LocalCaptureIntent{{
		ID:          "mobility-cloudedge-10-77-60-2",
		PoolRef:     "cloudedge",
		Address:     "10.77.60.2/32",
		Disposition: dynamicconfig.CaptureHold,
		CaptureType: "proxy-arp",
	}}
	if routes, staticAddresses := planCapturePrefixEffects(in, in.Pool.Prefix, hold); len(routes) != 0 || len(staticAddresses) != 0 {
		t.Fatalf("hold effects = routes=%#v static=%#v, want none", routes, staticAddresses)
	}

	desired := append([]dynamicconfig.LocalCaptureIntent(nil), hold...)
	desired[0].Disposition = dynamicconfig.CaptureProtectExisting
	routes, staticAddresses := planCapturePrefixEffects(in, in.Pool.Prefix, desired)
	if len(routes) != 1 || routes[0].Destination != "10.77.60.0/24" || routes[0].PreferredSource != "10.77.60.1" {
		t.Fatalf("external-source prefix route effects = %#v", routes)
	}
	if len(staticAddresses) != 0 {
		t.Fatalf("external-source static address effects = %#v, want none", staticAddresses)
	}
}

func TestPlanLocalCaptureIntentsProjectsProviderHold(t *testing.T) {
	in := bgpDeliveryPlannerInput{
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool: NormalizedMobilityPool{
				Name:                 "cloudedge",
				SelfCaptureInterface: "ens3",
				Self: memberPlanInfo{Capture: api.MobilityMemberCapture{
					Type: "provider-secondary-ip",
				}},
			},
			TunnelInterfaces: []string{"wg-cloud"},
		},
		Decisions: []ownershipDecision{{
			Address:            "10.77.60.2/32",
			CaptureDisposition: dynamicconfig.CaptureHold,
			CaptureReason:      "capture release is fenced while placement is unsettled",
		}},
	}

	captures := planLocalCaptureIntents(in)
	if len(captures) != 1 || captures[0].Disposition != dynamicconfig.CaptureHold || captures[0].CaptureInterface != "ens3" {
		t.Fatalf("provider hold capture intents = %#v", captures)
	}
}
