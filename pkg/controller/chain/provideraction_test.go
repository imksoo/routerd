// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	provideractioncontroller "github.com/imksoo/routerd/pkg/controller/provideraction"
	enginepkg "github.com/imksoo/routerd/pkg/provideraction"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestProviderActionReconcileUsesUpdatedRunnerRouter(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	runner := &Runner{Router: providerActionBaseRouter(), Store: store}
	controller := provideractioncontroller.Controller{Router: runner.Router, Store: store}
	if err := runner.reconcileProviderAction(context.Background(), eventedStore{Store: store}, controller); err != nil {
		t.Fatalf("initial reconcileProviderAction: %v", err)
	}
	rows, err := store.ListActions(routerstate.ActionExecutionFilter{})
	if err != nil {
		t.Fatalf("ListActions initial: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("initial actions = %d, want 0", len(rows))
	}

	runner.Router = providerActionRouterWithStaticClaim()
	if err := runner.reconcileProviderAction(context.Background(), eventedStore{Store: store}, controller); err != nil {
		t.Fatalf("updated reconcileProviderAction: %v", err)
	}
	rec, ok, err := store.GetActionByIdempotencyKey("remote-address-claim:app:aws:eni-1:assign-secondary-ip:10.0.0.5/32")
	if err != nil {
		t.Fatalf("GetActionByIdempotencyKey: %v", err)
	}
	if !ok {
		t.Fatalf("expected static RemoteAddressClaim provider action to be imported after Runner.Router update")
	}
	if rec.Status != routerstate.ActionPending {
		t.Fatalf("status = %q, want %q", rec.Status, routerstate.ActionPending)
	}
}

func providerActionBaseRouter() *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "ProviderActionPolicy"},
				Metadata: api.ObjectMeta{Name: "policy"},
				Spec: api.ProviderActionPolicySpec{
					Enabled:          true,
					DryRunOnly:       boolPtr(false),
					RequireApproval:  boolPtr(true),
					AllowedProviders: []string{"aws"},
					AllowedActions:   []string{"assign-secondary-ip"},
					AllowedCIDRs:     []string{"10.0.0.0/24"},
					MaxActionsPerRun: 4,
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"},
				Metadata: api.ObjectMeta{Name: "aws-executor"},
				Spec: api.PluginSpec{
					Executable:   "/bin/true",
					Capabilities: []string{enginepkg.CapabilityExecuteProviderAction},
				},
			},
		}},
	}
}

func providerActionRouterWithStaticClaim() *api.Router {
	router := providerActionBaseRouter()
	router.Spec.Resources = append(router.Spec.Resources,
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile"},
			Metadata: api.ObjectMeta{Name: "aws-prod"},
			Spec: api.CloudProviderProfileSpec{
				Provider: "aws",
				Auth:     api.ProviderAuth{Mode: "external-command", Command: "aws"},
			},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "RemoteAddressClaim"},
			Metadata: api.ObjectMeta{Name: "app"},
			Spec: api.RemoteAddressClaimSpec{
				Address:   "10.0.0.5/32",
				OwnerSide: "cloud",
				Capture: api.AddressCapture{
					Type:               "provider-secondary-ip",
					ProviderRef:        "aws-prod",
					NICRef:             "eni-1",
					ConfigureOSAddress: true,
					Interface:          "eth0",
				},
				Delivery: api.AddressDelivery{Mode: "route", PeerRef: "onprem", TunnelInterface: "ipip0"},
			},
		},
	)
	return router
}
