// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
)

func TestSystemdDPIHelpersWithdrawAndReapply(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	for _, mode := range []string{"removed", "disabled", "traffic-only"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			logger := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallEventLog"}, Metadata: api.ObjectMeta{Name: "default"}, Spec: api.FirewallLogSpec{Enabled: true}}
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{logger}}}
			var stopped []string
			stopFailure := false
			controller := SystemdUnitController{Router: router, Store: mapStore{}, SystemdSystemDir: dir, BGPStatePath: filepath.Join(dir, "bgp", "applied.json"), Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name == "systemctl" && len(args) == 3 && args[0] == "disable" && args[1] == "--now" {
					if stopFailure && args[2] == "routerd-firewall-logger.service" {
						return nil, errors.New("stop not acknowledged")
					}
					stopped = append(stopped, args[2])
				}
				if name == "systemctl" && len(args) > 0 && (args[0] == "is-active" || args[0] == "is-enabled") {
					return nil, errors.New("inactive")
				}
				return []byte("ok"), nil
			}}
			units := []string{"routerd-firewall-logger.service", render.DPIClassifierUnitName, render.NDPIAgentUnitName}
			if err := controller.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			for _, unit := range units {
				if _, err := os.Stat(filepath.Join(dir, unit)); err != nil {
					t.Fatal(err)
				}
			}
			router.Spec.Resources = nil
			if mode == "disabled" {
				disabled := logger
				disabled.Spec = api.FirewallLogSpec{Enabled: false}
				router.Spec.Resources = []api.Resource{disabled}
			} else if mode == "traffic-only" {
				router.Spec.Resources = []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "TrafficFlowLog"}, Metadata: api.ObjectMeta{Name: "flows"}, Spec: api.TrafficFlowLogSpec{Enabled: true, IncludeApplicationLayer: true}}}
			}
			stopped = nil
			stopFailure = true
			if err := controller.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "stop not acknowledged") {
				t.Fatalf("unconfirmed logger stop was not reported: %v", err)
			}
			for _, unit := range units {
				if _, err := os.Stat(filepath.Join(dir, unit)); err != nil {
					t.Fatalf("failed stop lost %s: %v", unit, err)
				}
			}
			stopFailure = false
			if err := controller.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			want := units
			if mode == "traffic-only" {
				want = units[:1]
			}
			if !slices.Equal(stopped, want) {
				t.Fatalf("stop order = %v, want %v", stopped, want)
			}
			for _, unit := range want {
				if _, err := os.Stat(filepath.Join(dir, unit)); !os.IsNotExist(err) {
					t.Fatalf("removed helper remains: %s: %v", unit, err)
				}
			}
			router.Spec.Resources = []api.Resource{logger}
			if err := controller.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			for _, unit := range units {
				if _, err := os.Stat(filepath.Join(dir, unit)); err != nil {
					t.Fatalf("reapply did not restore %s: %v", unit, err)
				}
			}
		})
	}
}
