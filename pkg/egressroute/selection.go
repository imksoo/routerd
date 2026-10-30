// SPDX-License-Identifier: BSD-3-Clause

package egressroute

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/healthcheck"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

const (
	SelectionHighestWeightReady = "highest-weight-ready"
	SelectionWeightedECMP       = "weighted-ecmp"

	PhaseApplied = "Applied"
	PhasePending = "Pending"

	ReasonNoReadyCandidates = "NoReadyCandidates"
	ReasonUnsupported       = "UnsupportedSelection"

	EventRouteChanged = "routerd.lan.route.changed"
)

type Store interface {
	resourcequery.StatusReader
	HealthObservation(string) (api.HealthObservation, error)
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
}

type Controller struct {
	Router   *api.Router
	Bus      *bus.Bus
	Store    Store
	Interval time.Duration
	Now      func() time.Time
	Logger   *slog.Logger
}

type CandidateState struct {
	Name          string
	Source        string
	Device        string
	Gateway       string
	GatewaySource string
	RouteTable    int
	Metric        int
	Ready         bool
	Disabled      bool
	Weight        int
	Priority      int
	Index         int
}

type Selection struct {
	Candidate CandidateState
	Reason    string
}

type Selector interface {
	Reconcile(ctx context.Context, policy api.Resource, candidates []CandidateState) (Selection, error)
}

func (c Controller) Start(ctx context.Context) {
	if c.Router == nil || c.Bus == nil || c.Store == nil {
		return
	}
	interval := c.Interval
	if interval == 0 {
		interval = 5 * time.Second
	}
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.**"}}, 64)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case event, ok := <-ch:
				if !ok {
					return
				}
				if event.Type == EventRouteChanged {
					continue
				}
				c.reconcileLogged(ctx)
			case <-ticker.C:
				c.reconcileLogged(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c Controller) reconcileLogged(ctx context.Context) {
	if err := c.Reconcile(ctx); err != nil && c.Logger != nil {
		c.Logger.Warn("egress route reconcile failed", "error", err)
	}
}

func (c Controller) Reconcile(ctx context.Context) error {
	now := c.now()
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "EgressRoutePolicy" {
			continue
		}
		if err := c.reconcilePolicy(ctx, resource, now); err != nil {
			return err
		}
	}
	return nil
}

func (c Controller) reconcilePolicy(ctx context.Context, resource api.Resource, now time.Time) error {
	spec, err := resource.EgressRoutePolicySpec()
	if err != nil {
		return err
	}
	if strings.TrimSpace(spec.Mode) != "" {
		return nil
	}
	if selection := defaultString(spec.Selection, SelectionHighestWeightReady); selection != SelectionHighestWeightReady {
		status := map[string]any{
			"phase":   PhasePending,
			"reason":  ReasonUnsupported,
			"message": fmt.Sprintf("selection %q is reserved but not implemented", selection),
		}
		return c.Store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", resource.Metadata.Name, status)
	}
	candidates, err := c.candidateStates(spec)
	if err != nil {
		return err
	}
	selected, ok := SelectHighestWeightReady(candidates)
	if !ok {
		status := map[string]any{"phase": PhasePending, "reason": ReasonNoReadyCandidates, "candidates": statusCandidates(candidates)}
		return c.Store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", resource.Metadata.Name, status)
	}

	previous, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", resource.Metadata.Name)
	if err != nil {
		return fmt.Errorf("read previous egress selection: %w", err)
	}
	previousName, _ := previous["selectedCandidate"].(string)
	lastTransitionAt := parseTime(fmt.Sprint(previous["lastTransitionAt"]))
	hysteresis := parseDurationOrDefault(spec.Hysteresis, 30*time.Second)
	if previousName != "" && previousName != selected.Name && lastTransitionAt != (time.Time{}) && now.Sub(lastTransitionAt) < hysteresis {
		if current, currentReady := candidateByName(candidates, previousName); currentReady && current.Ready {
			selected = current
		}
	}

	changed := selectionStatusChanged(previous, selected)
	transitionAt := now
	if !changed {
		transitionAt = lastTransitionAt
		if transitionAt.IsZero() {
			transitionAt = now
		}
	}
	status := map[string]any{
		"phase":                 PhaseApplied,
		"family":                defaultString(spec.Family, "ipv4"),
		"destinationCIDRs":      destinationCIDRs(spec),
		"selectedCandidate":     selected.Name,
		"selectedSource":        selected.Source,
		"selectedDevice":        selected.Device,
		"selectedGateway":       selected.Gateway,
		"selectedGatewaySource": selected.GatewaySource,
		"selectedRouteTable":    selected.RouteTable,
		"selectedMetric":        selected.Metric,
		"selectedWeight":        selected.Weight,
		"lastTransitionAt":      transitionAt.UTC().Format(time.RFC3339Nano),
		"hysteresis":            hysteresis.String(),
		"role":                  "advisory",
		"advisory":              true,
		"candidates":            statusCandidates(candidates),
	}
	if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", resource.Metadata.Name, status); err != nil {
		return err
	}
	if changed {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, EventRouteChanged, daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy", Name: resource.Metadata.Name}
		event.Attributes = map[string]string{
			"selectedCandidate":     selected.Name,
			"selectedDevice":        selected.Device,
			"selectedGateway":       selected.Gateway,
			"selectedGatewaySource": selected.GatewaySource,
			"role":                  "advisory",
			"advisory":              "true",
		}
		return c.Bus.Publish(ctx, event)
	}
	return nil
}

func selectionStatusChanged(previous map[string]any, selected CandidateState) bool {
	if len(previous) == 0 {
		return true
	}
	if fmt.Sprint(previous["phase"]) != PhaseApplied {
		return true
	}
	if fmt.Sprint(previous["selectedCandidate"]) != selected.Name {
		return true
	}
	if fmt.Sprint(previous["selectedDevice"]) != selected.Device {
		return true
	}
	if fmt.Sprint(previous["selectedGateway"]) != selected.Gateway {
		return true
	}
	if fmt.Sprint(previous["selectedGatewaySource"]) != selected.GatewaySource {
		return true
	}
	if statusInt(previous["selectedRouteTable"]) != selected.RouteTable {
		return true
	}
	if statusInt(previous["selectedMetric"]) != selected.Metric {
		return true
	}
	return false
}

func statusInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, _ := strconv.Atoi(v.String())
		return i
	case string:
		i, _ := strconv.Atoi(v)
		return i
	default:
		return 0
	}
}

func (c Controller) candidateStates(spec api.EgressRoutePolicySpec) ([]CandidateState, error) {
	out := make([]CandidateState, 0, len(spec.Candidates))
	for i, candidate := range spec.Candidates {
		name := candidate.Name
		if name == "" {
			name = candidate.Source
		}
		if name == "" {
			name = "candidate-" + strconv.Itoa(i)
		}
		healthDisabled, err := c.healthCheckDisabled(candidate.HealthCheck)
		if err != nil {
			return nil, err
		}
		disabled := candidateDisabled(candidate) || c.sourceDisabled(candidate.Source) || healthDisabled
		device, err := resourcequery.ReadValue(c.Store, candidate.DeviceFrom)
		if err != nil {
			return nil, fmt.Errorf("candidate %s device: %w", name, err)
		}
		gateway, err := resourcequery.ReadValue(c.Store, candidate.GatewayFrom)
		if err != nil {
			return nil, fmt.Errorf("candidate %s gateway: %w", name, err)
		}
		resolved := stringutil.FirstNonEmpty(device, strings.TrimSpace(candidate.Device)) != "" && (candidate.GatewayFrom.Resource == "" || gateway != "")
		ready := false
		if !disabled {
			ready, err = c.ready(candidate, resolved)
			if err != nil {
				return nil, fmt.Errorf("candidate %s readiness: %w", name, err)
			}
		}
		out = append(out, CandidateState{
			Name:          name,
			Source:        candidate.Source,
			Device:        stringutil.FirstNonEmpty(device, strings.TrimSpace(candidate.Device)),
			Gateway:       stringutil.FirstNonEmpty(gateway, strings.TrimSpace(candidate.Gateway)),
			GatewaySource: defaultString(candidate.GatewaySource, "none"),
			RouteTable:    candidate.EffectiveTable(),
			Metric:        candidate.EffectiveMetric(),
			Ready:         ready,
			Disabled:      disabled,
			Weight:        candidate.Weight,
			Priority:      candidate.Priority,
			Index:         i,
		})
	}
	return out, nil
}

func (c Controller) ready(candidate api.EgressRoutePolicyCandidate, resolved bool) (bool, error) {
	healthChecked := false
	if candidate.HealthCheck != "" {
		ready, err := c.healthCheckReady(candidate.HealthCheck)
		if err != nil || !ready {
			return false, err
		}
		healthChecked = true
	}
	dependenciesReady, err := resourcequery.ReadDependenciesReady(c.Store, candidate.DependsOn)
	if err != nil || !dependenciesReady {
		return false, err
	}
	if candidate.Source == "" {
		return resolved, nil
	}
	sourceReady, err := resourcequery.ReadSourceReady(c.Store, candidate.Source)
	if err != nil {
		return false, err
	}
	return (sourceReady || healthChecked) && resolved, nil
}

func (c Controller) healthCheckReady(name string) (bool, error) {
	if c.Router == nil {
		return false, nil
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "HealthCheck" || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.HealthCheckSpec()
		if err != nil {
			return false, err
		}
		resolved, err := healthcheck.ResolveSpecWithStoreForResource(c.Router, c.Store, name, spec)
		if err != nil {
			return false, err
		}
		observation, err := c.Store.HealthObservation(name)
		if err != nil {
			return false, fmt.Errorf("read Health observation %s: %w", name, err)
		}
		return observation.Usable(c.now(), resolved), nil
	}
	return false, nil
}

func (c Controller) sourceDisabled(source string) bool {
	if c.Router == nil {
		return false
	}
	kind, name, ok := resourcequery.SplitResource(source)
	if !ok || kind != "PPPoESession" {
		return false
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != kind || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.PPPoESessionSpec()
		return err == nil && !api.BoolDefault(spec.Enabled, true)
	}
	return false
}

func (c Controller) healthCheckDisabled(name string) (bool, error) {
	if c.Router == nil || strings.TrimSpace(name) == "" {
		return false, nil
	}
	status, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "HealthCheck", name)
	if err != nil {
		return false, fmt.Errorf("read healthcheck %s status: %w", name, err)
	}
	switch fmt.Sprint(status["phase"]) {
	case "Disabled", "Standby", "NotApplicable":
		return true, nil
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "HealthCheck" || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.HealthCheckSpec()
		return !api.BoolDefault(spec.Enabled, true), err
	}
	return false, nil
}

func candidateDisabled(candidate api.EgressRoutePolicyCandidate) bool {
	return !api.BoolDefault(candidate.Enabled, true)
}

func SelectHighestWeightReady(candidates []CandidateState) (CandidateState, bool) {
	ready := make([]CandidateState, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Ready {
			ready = append(ready, candidate)
		}
	}
	if len(ready) == 0 {
		return CandidateState{}, false
	}
	sort.SliceStable(ready, func(i, j int) bool {
		if ready[i].Weight != ready[j].Weight {
			return ready[i].Weight > ready[j].Weight
		}
		if ready[i].Priority != ready[j].Priority {
			return ready[i].Priority < ready[j].Priority
		}
		if ready[i].Name != ready[j].Name {
			return ready[i].Name < ready[j].Name
		}
		return ready[i].Index < ready[j].Index
	})
	return ready[0], true
}

func candidateByName(candidates []CandidateState, name string) (CandidateState, bool) {
	for _, candidate := range candidates {
		if candidate.Name == name {
			return candidate, true
		}
	}
	return CandidateState{}, false
}

func statusCandidates(candidates []CandidateState) []map[string]any {
	out := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, map[string]any{
			"name":          candidate.Name,
			"source":        candidate.Source,
			"device":        candidate.Device,
			"gateway":       candidate.Gateway,
			"gatewaySource": candidate.GatewaySource,
			"routeTable":    candidate.RouteTable,
			"metric":        candidate.Metric,
			"weight":        candidate.Weight,
			"ready":         candidate.Ready,
			"disabled":      candidate.Disabled,
		})
	}
	return out
}

func destinationCIDRs(spec api.EgressRoutePolicySpec) []string {
	if len(spec.DestinationCIDRs) > 0 {
		return spec.DestinationCIDRs
	}
	if defaultString(spec.Family, "ipv4") == "ipv6" {
		return []string{"::/0"}
	}
	return []string{"0.0.0.0/0"}
}

func parseDurationOrDefault(value string, fallback time.Duration) time.Duration {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func (c Controller) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now().UTC()
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
