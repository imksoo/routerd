// SPDX-License-Identifier: BSD-3-Clause

package codec

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestEncodeDecodeRoundTripCorePart(t *testing.T) {
	now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	record, err := Encode(dynamicconfig.DynamicConfigPart{
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:     "test/source",
			Generation: 7,
			ObservedAt: now,
			ExpiresAt:  now.Add(time.Hour),
			Digest:     "sha256:test",
			Resources: []api.Resource{{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "lan0", Managed: true, Owner: "routerd"},
			}},
			Directives: []dynamicconfig.DynamicConfigDirective{{
				Op:     dynamicconfig.DirectiveOpMask,
				Target: dynamicconfig.DirectiveTarget{APIVersion: api.NetAPIVersion, Kind: "Interface", Name: "wan"},
			}},
			ActionPlans: []dynamicconfig.ActionPlan{{Name: "display-only", Provider: "test", Action: "noop"}},
			MobilityDataplane: dynamicconfig.MobilityDataplanePlan{
				PoolPrefix: "192.0.2.0/24",
				Captures: []dynamicconfig.LocalCaptureIntent{{
					ID: "pool/192.0.2.1", PoolRef: "pool", Address: "192.0.2.1/32", Disposition: dynamicconfig.CaptureDesired,
				}},
				Routes: []dynamicconfig.MobilityIPv4RouteIntent{{
					ID: "pool/route/local/192.0.2.1", PoolRef: "pool", Purpose: dynamicconfig.MobilityIPv4RoutePurposeLocalInventory,
					Destination: "192.0.2.1/32", Device: "ens3", Metric: 1,
				}},
				StaticAddresses: []dynamicconfig.MobilityIPv4AddressIntent{{
					ID: "pool/address/capture-source", PoolRef: "pool", Purpose: dynamicconfig.MobilityIPv4AddressPurposeCaptureSource,
					Interface: "ens3", Address: "192.0.2.254/32",
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.ActionPlansJSON == "" || record.MobilityDataplaneJSON == "" {
		t.Fatalf("Encode() omitted populated optional fields: %#v", record)
	}
	mobility, err := DecodeMobilityDataplanePlan(record.MobilityDataplaneJSON)
	if err != nil {
		t.Fatalf("DecodeMobilityDataplanePlan: %v", err)
	}
	if len(mobility.Captures) != 1 || len(mobility.Routes) != 1 || len(mobility.StaticAddresses) != 1 {
		t.Fatalf("decoded mobility plan = %#v", mobility)
	}
	if mobility.Routes[0].Purpose != dynamicconfig.MobilityIPv4RoutePurposeLocalInventory || mobility.StaticAddresses[0].Purpose != dynamicconfig.MobilityIPv4AddressPurposeCaptureSource {
		t.Fatalf("decoded mobility plan purposes = %#v", mobility)
	}

	part, err := Decode(record)
	if err != nil {
		t.Fatal(err)
	}
	if part.Metadata.Name != "test/source-7" || part.Spec.Source != "test/source" || part.Spec.Generation != 7 {
		t.Fatalf("Decode() core identity = %#v", part)
	}
	if len(part.Spec.Resources) != 1 || part.Spec.Resources[0].ID() != api.NetAPIVersion+"/Interface/lan" {
		t.Fatalf("Decode() resources = %#v", part.Spec.Resources)
	}
	if spec, err := part.Spec.Resources[0].InterfaceSpec(); err != nil || spec.IfName != "lan0" {
		t.Fatalf("Decode() resource spec = %#v, %v", spec, err)
	}
	if len(part.Spec.Directives) != 1 || part.Spec.Directives[0].Target.Name != "wan" {
		t.Fatalf("Decode() directives = %#v", part.Spec.Directives)
	}
}

func TestEncodeKeepsEmptyOptionalColumnsEmpty(t *testing.T) {
	record, err := Encode(dynamicconfig.DynamicConfigPart{Spec: dynamicconfig.DynamicConfigPartSpec{
		Source: "test/empty", ActionPlans: []dynamicconfig.ActionPlan{}, MobilityDataplane: dynamicconfig.MobilityDataplanePlan{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if record.ActionPlansJSON != "" || record.MobilityDataplaneJSON != "" || record.ARPObserverIntentsJSON != "" || record.FIBVerdictsJSON != "" {
		t.Fatalf("empty optional columns = %#v", record)
	}
	if !strings.Contains(record.ResourcesJSON, "null") || !strings.Contains(record.DirectivesJSON, "null") {
		t.Fatalf("nil core slices must retain JSON persistence shape: %#v", record)
	}
}

func TestDecodeMobilityDataplanePlanEmpty(t *testing.T) {
	plan, err := DecodeMobilityDataplanePlan("  \n")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.IsEmpty() {
		t.Fatalf("empty plan = %#v", plan)
	}
}

func TestDecodeMobilityDataplanePlanRejectsNonCanonicalNonemptyPayload(t *testing.T) {
	for _, raw := range []string{"null", "{}", `{"unknown":true}`, `{"captures":[]}`} {
		t.Run(raw, func(t *testing.T) {
			if _, err := DecodeMobilityDataplanePlan(raw); err == nil {
				t.Fatalf("DecodeMobilityDataplanePlan(%q) accepted an empty noncanonical payload", raw)
			}
		})
	}
}

func TestDecodeMobilityFIBVerdictsStrictlyRejectsAmbiguousPayload(t *testing.T) {
	valid := `[{"poolRef":"cloudedge","scope":{"prefix":"10.77.60.0/24"}},{"poolRef":"cloudedge","address":"10.77.60.10/32","action":"deliver-remote"}]`
	verdicts, err := DecodeMobilityFIBVerdicts(valid)
	if err != nil || len(verdicts) != 2 {
		t.Fatalf("DecodeMobilityFIBVerdicts(valid) = %#v, %v", verdicts, err)
	}
	for _, raw := range []string{
		`null`,
		`[null]`,
		`[{"poolRef":"cloudedge","unknown":true}]`,
		`[{"poolRef":"cloudedge","poolRef":"other"}]`,
		`[{"poolRef":null}]`,
		valid + ` []`,
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := DecodeMobilityFIBVerdicts(raw); err == nil {
				t.Fatalf("DecodeMobilityFIBVerdicts(%q) accepted an ambiguous payload", raw)
			}
		})
	}
}

func TestDecodeIgnoresLegacyGenericMobilityPoolPayload(t *testing.T) {
	resources, err := json.Marshal([]api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
		Metadata: api.ObjectMeta{Name: "legacy-mobility-route"},
		Spec:     api.IPv4RouteSpec{Destination: "192.0.2.10/32", Device: "ens3"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	directives, err := json.Marshal([]dynamicconfig.DynamicConfigDirective{{
		Op:     dynamicconfig.DirectiveOpMask,
		Target: dynamicconfig.DirectiveTarget{APIVersion: api.NetAPIVersion, Kind: "IPv4Route", Name: "legacy-mobility-route"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	record := routerstate.DynamicConfigPartRecord{
		Source:         "MobilityPool/cloudedge/node/router-a",
		ResourcesJSON:  string(resources),
		DirectivesJSON: string(directives),
	}
	part, err := Decode(record)
	if err != nil {
		t.Fatal(err)
	}
	if len(part.Spec.Resources) != 0 || len(part.Spec.Directives) != 0 {
		t.Fatalf("legacy generic MobilityPool payload remained effective: %#v", part.Spec)
	}
	if resources, err := DecodeGenericResources(record); err != nil || len(resources) != 0 {
		t.Fatalf("DecodeGenericResources() = %#v, %v", resources, err)
	}
	if directives, err := DecodeGenericDirectives(record); err != nil || len(directives) != 0 {
		t.Fatalf("DecodeGenericDirectives() = %#v, %v", directives, err)
	}
}
