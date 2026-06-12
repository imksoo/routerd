// SPDX-License-Identifier: BSD-3-Clause

package provideractioncontroller

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	enginepkg "github.com/imksoo/routerd/pkg/provideraction"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// Store is the action journal + dynamic part surface required by the
// controller. *state.SQLiteStore satisfies it.
type Store interface {
	enginepkg.Store
}

// Controller imports provider ActionPlans and, when ProviderActionPolicy permits
// policy auto-approval, executes pending provider actions through the same
// Engine path used by routerctl action execute.
type Controller struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
	Runner enginepkg.ExecutorRunner
	Now    func() time.Time
	DryRun bool
	Logger *slog.Logger
}

func (c Controller) Reconcile(ctx context.Context) error {
	if c.Store == nil {
		return nil
	}
	now := c.now()
	policy, plugins, err := enginepkg.PolicyAndPlugins(c.Router)
	if err != nil {
		return err
	}
	runner := c.Runner
	if runner == nil {
		runner = enginepkg.RunExecutor
	}
	engine, err := enginepkg.NewEngine(enginepkg.Config{
		Store:   c.Store,
		Runner:  runner,
		Now:     func() time.Time { return now },
		Log:     controllerLogger{logger: c.Logger},
		Plugins: plugins,
	})
	if err != nil {
		return err
	}
	if _, err := engine.ImportFromDynamicParts(); err != nil {
		return err
	}
	staticPlans, err := staticRemoteAddressClaimPlans(c.Router)
	if err != nil {
		return err
	}
	if len(staticPlans) > 0 {
		if _, err := engine.ImportPlans("RemoteAddressClaim", staticPlans); err != nil {
			return err
		}
	}
	if c.DryRun {
		c.log("provideraction: auto execution dry-run disabled")
		return nil
	}
	enabled, reason := enginepkg.AutoExecutionEnabled(policy)
	if !enabled {
		c.log("provideraction: auto execution disabled: " + reason)
		return nil
	}
	if _, err := engine.RecoverStaleRunningActions(); err != nil {
		return err
	}
	rows, err := c.Store.ListActions(routerstate.ActionExecutionFilter{})
	if err != nil {
		return fmt.Errorf("list action journal: %w", err)
	}
	candidates := autoExecutionCandidates(rows, policy.MaxActionsPerRun)
	for _, row := range candidates {
		if err := engine.Execute(ctx, row.ID, enginepkg.ModeExecute, policy); err != nil {
			c.log("provideraction: auto execute action failed", "id", row.ID, "key", row.IdempotencyKey, "error", err)
			continue
		}
		updated, found, err := c.Store.GetActionByID(row.ID)
		if err != nil {
			return fmt.Errorf("load executed action %d: %w", row.ID, err)
		}
		if found {
			_ = c.publishProviderCaptureChanged(ctx, updated, now)
		}
	}
	return nil
}

func (c Controller) publishProviderCaptureChanged(ctx context.Context, row routerstate.ActionExecutionRecord, now time.Time) error {
	if c.Bus == nil || row.Status != routerstate.ActionSucceeded || !providerCaptureAction(row.Action) {
		return nil
	}
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "provider-action-execution", Kind: "provider-action-execution"}, enginepkg.ProviderCaptureChangedEvent, daemonapi.SeverityInfo)
	event.Time = now
	event.Attributes = map[string]string{
		"actionID":       fmt.Sprint(row.ID),
		"action":         row.Action,
		"provider":       row.Provider,
		"providerRef":    row.ProviderRef,
		"idempotencyKey": row.IdempotencyKey,
	}
	return c.Bus.Publish(ctx, event)
}

func providerCaptureAction(action string) bool {
	switch strings.TrimSpace(action) {
	case "assign-secondary-ip", "unassign-secondary-ip":
		return true
	default:
		return false
	}
}

func (c Controller) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c Controller) log(msg string, args ...any) {
	if c.Logger != nil {
		c.Logger.Debug(msg, args...)
	}
}

func autoExecutionCandidates(rows []routerstate.ActionExecutionRecord, limit int) []routerstate.ActionExecutionRecord {
	if limit <= 0 {
		return nil
	}
	out := make([]routerstate.ActionExecutionRecord, 0, limit)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	for _, row := range rows {
		switch row.Status {
		case routerstate.ActionPending, routerstate.ActionApproved:
			out = append(out, row)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

type controllerLogger struct {
	logger *slog.Logger
}

func (l controllerLogger) Printf(format string, args ...any) {
	if l.logger != nil {
		l.logger.Debug(fmt.Sprintf(format, args...))
	}
}

func staticRemoteAddressClaimPlans(router *api.Router) ([]dynamicconfig.ActionPlan, error) {
	if router == nil {
		return nil, nil
	}
	profiles := map[string]api.CloudProviderProfileSpec{}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "CloudProviderProfile" {
			continue
		}
		spec, err := resource.CloudProviderProfileSpec()
		if err != nil {
			return nil, err
		}
		profiles[resource.Metadata.Name] = spec
	}
	forwardingSeen := map[string]bool{}
	var plans []dynamicconfig.ActionPlan
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "RemoteAddressClaim" {
			continue
		}
		spec, err := resource.RemoteAddressClaimSpec()
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(spec.Capture.Type) != "provider-secondary-ip" {
			continue
		}
		profileRef := strings.TrimSpace(spec.Capture.ProviderRef)
		profile, ok := profiles[profileRef]
		if !ok {
			return nil, fmt.Errorf("%s spec.capture.providerRef %q does not reference a CloudProviderProfile", resource.ID(), profileRef)
		}
		claimPlans, err := staticRemoteAddressClaimProviderPlans(resource.Metadata.Name, spec, profile, forwardingSeen)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", resource.ID(), err)
		}
		plans = append(plans, claimPlans...)
	}
	return plans, nil
}

func staticRemoteAddressClaimProviderPlans(name string, spec api.RemoteAddressClaimSpec, profile api.CloudProviderProfileSpec, forwardingSeen map[string]bool) ([]dynamicconfig.ActionPlan, error) {
	provider := strings.TrimSpace(profile.Provider)
	providerRef := strings.TrimSpace(spec.Capture.ProviderRef)
	nicRef := strings.TrimSpace(spec.Capture.NICRef)
	address := strings.TrimSpace(spec.Address)
	if provider == "" {
		return nil, fmt.Errorf("CloudProviderProfile provider is required")
	}
	if providerRef == "" {
		return nil, fmt.Errorf("capture.providerRef is required")
	}
	if nicRef == "" {
		return nil, fmt.Errorf("capture.nicRef is required")
	}
	if _, _, err := net.ParseCIDR(address); err != nil {
		return nil, fmt.Errorf("spec.address %q must be CIDR: %w", address, err)
	}
	strategy := firstNonEmpty(strings.TrimSpace(spec.Capture.CaptureStrategy), strings.TrimSpace(spec.Capture.Strategy), "secondary-ip")
	if strategy != "secondary-ip" {
		return nil, fmt.Errorf("provider-secondary-ip static action generation supports captureStrategy secondary-ip, got %q", strategy)
	}
	target := copyStringMap(spec.Capture.Target)
	target["provider"] = provider
	target["providerRef"] = providerRef
	target["nicRef"] = nicRef
	target["address"] = address
	target["captureStrategy"] = strategy
	addStaticProfileTargetFields(target, provider, profile, name, address, nicRef)
	assign := dynamicconfig.ActionPlan{
		Name:           safeName("remote-claim-" + name + "-assign-" + address),
		Provider:       provider,
		ProviderRef:    providerRef,
		Action:         "assign-secondary-ip",
		Target:         target,
		Mode:           "dry-run",
		Description:    fmt.Sprintf("Assign %s as a secondary IP on %s NIC %s for RemoteAddressClaim/%s", address, provider, nicRef, name),
		RiskLevel:      "medium",
		IdempotencyKey: "remote-address-claim:" + name + ":" + provider + ":" + nicRef + ":assign-secondary-ip:" + address,
		ExpectedEffects: []string{
			fmt.Sprintf("%s NIC %s would advertise secondary IP %s", provider, nicRef, address),
		},
		Undo: &dynamicconfig.ActionUndo{Action: "unassign-secondary-ip", Parameters: copyStringMap(target)},
	}
	plans := []dynamicconfig.ActionPlan{assign}
	forwardingKey := provider + "\x00" + providerRef + "\x00" + nicRef
	if !forwardingSeen[forwardingKey] {
		params, err := staticForwardingParams(provider)
		if err != nil {
			return nil, err
		}
		forwardingSeen[forwardingKey] = true
		plans = append(plans, dynamicconfig.ActionPlan{
			Name:           safeName("remote-claim-" + name + "-forwarding-" + nicRef),
			Provider:       provider,
			ProviderRef:    providerRef,
			Action:         "ensure-forwarding-enabled",
			Target:         copyStringMap(target),
			Mode:           "dry-run",
			Description:    fmt.Sprintf("Ensure forwarding is enabled on %s NIC %s for RemoteAddressClaim/%s", provider, nicRef, name),
			RiskLevel:      "medium",
			IdempotencyKey: "remote-address-claim:" + name + ":" + provider + ":" + nicRef + ":ensure-forwarding-enabled",
			Parameters:     params,
			ExpectedEffects: []string{
				fmt.Sprintf("%s NIC %s would forward traffic for provider captures", provider, nicRef),
			},
			Undo: &dynamicconfig.ActionUndo{Action: "ensure-forwarding-disabled", Parameters: mergeStringMaps(target, params)},
		})
	}
	return plans, nil
}

func addStaticProfileTargetFields(target map[string]string, provider string, profile api.CloudProviderProfileSpec, name, address, nicRef string) {
	if profile.SubscriptionID != "" {
		target["subscriptionId"] = strings.TrimSpace(profile.SubscriptionID)
	}
	if profile.ResourceGroup != "" {
		target["resourceGroup"] = strings.TrimSpace(profile.ResourceGroup)
	}
	if provider != "azure" {
		return
	}
	if target["nicName"] == "" {
		target["nicName"] = azureResourceName(nicRef)
	}
	if target["ipConfigName"] == "" {
		target["ipConfigName"] = safeName(name + "-" + address)
	}
}

func staticForwardingParams(provider string) (map[string]string, error) {
	switch provider {
	case "aws":
		return map[string]string{"sourceDestCheck": "false"}, nil
	case "azure":
		return map[string]string{"ipForwarding": "true"}, nil
	case "oci":
		return map[string]string{"skipSourceDestCheck": "true"}, nil
	case "gcp":
		return map[string]string{"canIpForward": "true"}, nil
	default:
		return nil, fmt.Errorf("provider %q is not supported for provider action plans", provider)
	}
}

func copyStringMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mergeStringMaps(a, b map[string]string) map[string]string {
	out := copyStringMap(a)
	for k, v := range b {
		out[k] = v
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func safeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func azureResourceName(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	parts := strings.Split(ref, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if strings.TrimSpace(parts[i]) != "" {
			return strings.TrimSpace(parts[i])
		}
	}
	return ref
}
