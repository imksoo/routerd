// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/sam"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestFaultEvaluationSnapshotTypedRouteWithdrawal(t *testing.T) {
	if platform.CurrentOS() != platform.OSLinux {
		t.Skip("Linux route-command fixture; FreeBSD dataplane runtime is a separate gate")
	}
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	for _, withdrawal := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "expired-withdrawal"}[withdrawal], func(t *testing.T) {
			src, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "source.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer src.Close()
			dst, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "snapshot.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer dst.Close()
			part := dynamicconfig.NewPart("plan", "MobilityPool/pool/node/router", nil, 1, now.Add(-time.Minute), now.Add(time.Minute))
			part.Spec.Digest = "fixture"
			part.Spec.MobilityDataplane = dynamicconfig.MobilityDataplanePlan{PoolPrefix: "192.0.2.0/24", Routes: []dynamicconfig.MobilityIPv4RouteIntent{{ID: "pool/capture-prefix", PoolRef: "pool", Purpose: dynamicconfig.MobilityIPv4RoutePurposeCapturePrefix, Destination: "192.0.2.0/24", Device: "lan0", Metric: 90}}}
			part.Spec.MobilityDataplane.Captures = []dynamicconfig.LocalCaptureIntent{{ID: "pool/capture", PoolRef: "pool", Address: "192.0.2.20/32", Disposition: dynamicconfig.CaptureDesired, CaptureType: "provider-secondary-ip", CaptureInterface: "lan0", TunnelInterfaces: []string{"wg-test"}}}
			part.Spec.MobilityDataplane.StaticAddresses = []dynamicconfig.MobilityIPv4AddressIntent{{ID: "pool/source", PoolRef: "pool", Purpose: dynamicconfig.MobilityIPv4AddressPurposeCaptureSource, Interface: "lan0", Address: "192.0.2.1/32"}}
			if withdrawal {
				part.Spec.ExpiresAt = now
			}
			record, err := codec.Encode(part)
			if err != nil {
				t.Fatal(err)
			}
			if err := src.UpsertDynamicConfigPart(record); err != nil {
				t.Fatal(err)
			}
			if err := src.SaveObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName, map[string]any{"appliedMobilityRoutes": []mobilityAppliedIPv4Route{{ID: "pool/capture-prefix", PoolRef: "pool", PoolPrefix: "192.0.2.0/24", Purpose: string(dynamicconfig.MobilityIPv4RoutePurposeCapturePrefix), Destination: "192.0.2.0/24", Device: "lan0", Metric: 90}}}); err != nil {
				t.Fatal(err)
			}
			if err := src.CopyEvaluationStateTo(dst); err != nil {
				t.Fatal(err)
			}
			candidate := &api.Router{}
			before, err := buildDynamicRouteSAMView(candidate, src, now, platform.OSLinux)
			if err != nil {
				t.Fatal(err)
			}
			after, err := buildDynamicRouteSAMView(candidate, dst, now, platform.OSLinux)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before.MobilityDataplane, after.MobilityDataplane) {
				t.Fatal("typed plan differs after snapshot")
			}
			if !withdrawal && (len(after.MobilityDataplane.Captures) != 1 || len(after.MobilityDataplane.StaticAddresses) != 1) {
				t.Fatal("active capture/address fixture was not evaluated")
			}
			if len(after.RouteRouter.Spec.Resources) != 0 {
				t.Fatal("typed plan became generic resources")
			}
			beforeCapture, err := sam.PlanLocalCaptureIntents(before.MobilityDataplane.Captures, platform.OSLinux)
			if err != nil {
				t.Fatal(err)
			}
			afterCapture, err := sam.PlanLocalCaptureIntents(after.MobilityDataplane.Captures, platform.OSLinux)
			if err != nil || !reflect.DeepEqual(beforeCapture, afterCapture) {
				t.Fatal("capture intent changed")
			}
			apply := func(store *routerstate.SQLiteStore, view dynamicRouteSAMView) []string {
				var commands []string
				controller := IPv4RouteController{Router: view.RouteRouter, Store: store, MobilityDataplane: view.MobilityDataplane,
					DevicePresent: func(context.Context, string) bool { return true },
					Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
						command := name + " " + strings.Join(args, " ")
						commands = append(commands, command)
						if strings.HasPrefix(command, "ip route show") {
							return []byte("192.0.2.0/24 dev lan0 proto static metric 90\n"), nil
						}
						return nil, nil
					},
				}
				if err := controller.reconcile(context.Background()); err != nil {
					t.Fatal(err)
				}
				return commands
			}
			want, got := apply(src, before), apply(dst, after)
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("commands changed: source=%v snapshot=%v", want, got)
			}
			deleted := false
			for _, command := range got {
				if strings.Contains(command, "route del 192.0.2.0/24 dev lan0 metric 90") {
					deleted = true
				}
			}
			if deleted != withdrawal {
				t.Fatalf("withdrawal=%t commands=%v", withdrawal, got)
			}
		})
	}
}
