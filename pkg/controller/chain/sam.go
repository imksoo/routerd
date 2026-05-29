// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/sam"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type samProxyNeighborApplier interface {
	EnsureProxyNeighbor(ctx context.Context, address, ifname string) error
	DeleteProxyNeighbor(ctx context.Context, address, ifname string) error
}

func samSelectResources(resources []api.Resource, kind string) []api.Resource {
	var out []api.Resource
	for _, resource := range resources {
		if resource.Kind == kind {
			out = append(out, resource)
		}
	}
	return out
}

type SAMController struct {
	Router    *api.Router
	Bus       *bus.Bus
	Store     Store
	Lowerings []sam.DeliveryLowering
	DryRun    bool
	OS        platform.OS
	Applier   samProxyNeighborApplier
}

func (c SAMController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	targetOS := c.OS
	if targetOS == "" {
		targetOS = platform.CurrentOS()
	}
	if targetOS != platform.OSLinux {
		return c.reconcileStatuses(targetOS)
	}
	if err := c.cleanupRemovedCaptures(ctx); err != nil {
		return err
	}
	actions, err := sam.PlanCapture(c.Router, targetOS)
	if err != nil {
		return err
	}
	var failures []string
	for _, action := range actions {
		if action.Kind != "proxy-neighbor" {
			continue
		}
		if c.DryRun {
			continue
		}
		applier := c.Applier
		if applier == nil {
			applier = defaultSAMProxyNeighborApplier()
		}
		if err := applier.EnsureProxyNeighbor(ctx, action.Address, action.Interface); err != nil {
			failures = append(failures, fmt.Sprintf("%s %s dev %s: %v", action.ClaimName, action.Address, action.Interface, err))
		}
	}
	if err := c.reconcileStatuses(targetOS); err != nil {
		return err
	}
	if len(failures) > 0 {
		return fmt.Errorf("SAM capture failed: %s", strings.Join(failures, "; "))
	}
	return nil
}

func (c SAMController) reconcileStatuses(targetOS platform.OS) error {
	claims := samSelectResources(c.Router.Spec.Resources, "RemoteAddressClaim")
	for _, claim := range claims {
		status := sam.StatusForRemoteAddressClaim(claim, c.Lowerings, c.Store, targetOS)
		status["dryRun"] = c.DryRun
		if targetOS == platform.OSLinux {
			if spec, err := claim.RemoteAddressClaimSpec(); err == nil && strings.TrimSpace(spec.Capture.Type) == "proxy-arp" {
				status["captureProxyNeighbor"] = map[string]any{
					"address":   strings.TrimSpace(spec.Address),
					"interface": strings.TrimSpace(spec.Capture.Interface),
				}
			}
		}
		if err := c.Store.SaveObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", claim.Metadata.Name, status); err != nil {
			return err
		}
	}
	for _, domain := range samSelectResources(c.Router.Spec.Resources, "AddressMobilityDomain") {
		status := sam.StatusForAddressMobilityDomain(domain, claims, c.Store)
		if err := c.Store.SaveObjectStatus(api.HybridAPIVersion, "AddressMobilityDomain", domain.Metadata.Name, status); err != nil {
			return err
		}
	}
	return nil
}

func (c SAMController) cleanupRemovedCaptures(ctx context.Context) error {
	if c.Store == nil {
		return nil
	}
	lister, ok := c.Store.(interface {
		ListObjectStatuses() ([]routerstate.ObjectStatus, error)
	})
	if !ok {
		return nil
	}
	deleter, ok := c.Store.(interface {
		DeleteObject(apiVersion, kind, name string) error
	})
	if !ok {
		return nil
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return err
	}
	desired := map[string]bool{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion == api.HybridAPIVersion && resource.Kind == "RemoteAddressClaim" {
			desired[resource.Metadata.Name] = true
		}
	}
	applier := c.Applier
	if applier == nil {
		applier = defaultSAMProxyNeighborApplier()
	}
	for _, status := range statuses {
		if status.APIVersion != api.HybridAPIVersion || status.Kind != "RemoteAddressClaim" || desired[status.Name] {
			continue
		}
		if !c.DryRun {
			if capture, ok := status.Status["captureProxyNeighbor"].(map[string]any); ok {
				address := strings.TrimSpace(fmt.Sprint(capture["address"]))
				ifname := strings.TrimSpace(fmt.Sprint(capture["interface"]))
				if address != "" && address != "<nil>" && ifname != "" && ifname != "<nil>" {
					if err := applier.DeleteProxyNeighbor(ctx, address, ifname); err != nil {
						return fmt.Errorf("delete removed SAM proxy neighbor %s dev %s: %w", address, ifname, err)
					}
				}
			}
		}
		if err := deleter.DeleteObject(api.HybridAPIVersion, "RemoteAddressClaim", status.Name); err != nil {
			return err
		}
		if c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.sam.capture.removed", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.HybridAPIVersion, Kind: "RemoteAddressClaim", Name: status.Name}
			event.Attributes = map[string]string{"removedAt": time.Now().UTC().Format(time.RFC3339Nano)}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}
