// SPDX-License-Identifier: BSD-3-Clause

package firewall

import (
	"context"
	"fmt"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/firewallbackend"
	"github.com/imksoo/routerd/pkg/platform"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

const RulesAppliedEvent = "routerd.firewall.rules.applied"

type Controller struct {
	Router       *api.Router
	Bus          *bus.Bus
	Store        Store
	DryRun       bool
	ForceApply   bool
	NftablesPath string
	NftCommand   string
	// LocalCaptureIntents is the already-decided dynamic Mobility dataplane
	// plan. It matters to the PF backend, where path-MTU rules live in the
	// native firewall ruleset rather than a separate nftables table.
	LocalCaptureIntents []dynamicconfig.LocalCaptureIntent
}

func (c Controller) Reconcile(ctx context.Context) error {
	if c.Router == nil {
		return nil
	}
	targetOS := platform.CurrentOS()
	configured := hasFirewall(c.Router)
	if !configured && targetOS != platform.OSLinux && (targetOS != platform.OSFreeBSD || len(c.LocalCaptureIntents) == 0) {
		return nil
	}
	backend := firewallbackend.ForPlatform(targetOS, c.NftCommand)
	if nft, ok := backend.(firewallbackend.Nftables); ok {
		nft.Force = c.ForceApply
		backend = nft
	}
	var ruleset firewallbackend.Ruleset
	if !configured && targetOS == platform.OSLinux {
		ruleset = firewallbackend.Ruleset{
			Backend: backend.Name(),
			Path:    stringutil.FirstNonEmpty(c.NftablesPath, "/run/routerd/firewall.nft"),
			Data:    []byte("destroy table inet routerd_filter\n"),
		}
	} else {
		var err error
		ruleset, err = backend.Render(c.Router, c.NftablesPath, c.LocalCaptureIntents)
		if err != nil {
			_ = c.saveErrorStatuses(ctx, backend.Name(), c.NftablesPath, "RenderFailed", err)
			return err
		}
	}
	changed, err := backend.Apply(ctx, ruleset, c.DryRun)
	if err != nil {
		_ = c.saveErrorStatuses(ctx, ruleset.Backend, ruleset.Path, "ApplyFailed", err)
		return err
	}
	return c.savePolicyStatuses(ctx, ruleset.Backend, ruleset.Path, changed, ruleset.InternalHoles)
}

func (c Controller) saveErrorStatuses(ctx context.Context, backend, path, reason string, applyErr error) error {
	status := map[string]any{
		"phase":        "Error",
		"backend":      backend,
		"dryRun":       c.DryRun,
		"reason":       reason,
		"error":        applyErr.Error(),
		"rulesetPath":  path,
		"nftablesPath": path,
		"conditions":   []map[string]any{{"type": "Applied", "status": "False", "reason": reason, "message": applyErr.Error()}},
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.FirewallAPIVersion || resource.Kind == "IngressService" {
			continue
		}
		if err := c.Store.SaveObjectStatus(api.FirewallAPIVersion, resource.Kind, resource.Metadata.Name, status); err != nil {
			return err
		}
	}
	if c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.firewall.rules.error", daemonapi.SeverityError)
		event.Attributes = map[string]string{"backend": backend, "nftablesPath": path, "dryRun": fmt.Sprintf("%t", c.DryRun), "reason": reason, "error": applyErr.Error()}
		_ = c.Bus.Publish(ctx, event)
	}
	return nil
}

func (c Controller) savePolicyStatuses(ctx context.Context, backend, path string, changed bool, internalHoles int) error {
	status := map[string]any{
		"phase":         "Applied",
		"backend":       backend,
		"dryRun":        c.DryRun,
		"changed":       changed,
		"rules":         firewallRuleCount(c.Router),
		"internalHoles": internalHoles,
		"rulesetPath":   path,
		"nftablesPath":  path,
		"conditions":    []map[string]any{{"type": "Applied", "status": "True", "reason": "Rendered"}},
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.FirewallAPIVersion || resource.Kind == "IngressService" {
			continue
		}
		if err := c.Store.SaveObjectStatus(api.FirewallAPIVersion, resource.Kind, resource.Metadata.Name, status); err != nil {
			return err
		}
	}
	if changed && c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, RulesAppliedEvent, daemonapi.SeverityInfo)
		event.Attributes = map[string]string{"backend": backend, "nftablesPath": path, "dryRun": fmt.Sprintf("%t", c.DryRun), "internalHoles": fmt.Sprintf("%d", internalHoles)}
		_ = c.Bus.Publish(ctx, event)
	}
	return nil
}

func hasFirewall(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.FirewallAPIVersion && (resource.Kind == "FirewallZone" || resource.Kind == "FirewallPolicy" || resource.Kind == "FirewallRule" || resource.Kind == "FirewallFlowPinhole" || resource.Kind == "ClientPolicy" || resource.Kind == "LocalServiceRedirect") {
			return true
		}
	}
	return false
}

func firewallRuleCount(router *api.Router) int {
	n := 0
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.FirewallAPIVersion && resource.Kind == "FirewallRule" {
			n++
		}
	}
	return n
}
