package wanegress

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
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
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
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
	Name       string
	Source     string
	Device     string
	Gateway    string
	RouteTable int
	Metric     int
	Ready      bool
	Weight     int
	Index      int
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
		c.Logger.Warn("wan egress reconcile failed", "error", err)
	}
}

func (c Controller) Reconcile(ctx context.Context) error {
	now := c.now()
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "WANEgressPolicy" {
			continue
		}
		if err := c.reconcilePolicy(ctx, resource, now); err != nil {
			return err
		}
	}
	return nil
}

func (c Controller) reconcilePolicy(ctx context.Context, resource api.Resource, now time.Time) error {
	spec, err := resource.WANEgressPolicySpec()
	if err != nil {
		return err
	}
	if selection := defaultString(spec.Selection, SelectionHighestWeightReady); selection != SelectionHighestWeightReady {
		status := map[string]any{
			"phase":   PhasePending,
			"reason":  ReasonUnsupported,
			"message": fmt.Sprintf("selection %q is reserved but not implemented", selection),
		}
		return c.Store.SaveObjectStatus(api.NetAPIVersion, "WANEgressPolicy", resource.Metadata.Name, status)
	}
	candidates := c.candidateStates(spec)
	selected, ok := selectHighestWeightReady(candidates)
	if !ok {
		status := map[string]any{"phase": PhasePending, "reason": ReasonNoReadyCandidates, "candidates": statusCandidates(candidates)}
		return c.Store.SaveObjectStatus(api.NetAPIVersion, "WANEgressPolicy", resource.Metadata.Name, status)
	}

	previous := c.Store.ObjectStatus(api.NetAPIVersion, "WANEgressPolicy", resource.Metadata.Name)
	previousName, _ := previous["selectedCandidate"].(string)
	lastTransitionAt := parseTime(fmt.Sprint(previous["lastTransitionAt"]))
	hysteresis := parseDurationOrDefault(spec.Hysteresis, 30*time.Second)
	if previousName != "" && previousName != selected.Name && lastTransitionAt != (time.Time{}) && now.Sub(lastTransitionAt) < hysteresis {
		if current, currentReady := candidateByName(candidates, previousName); currentReady && current.Ready {
			selected = current
		}
	}

	changed := previousName != "" && previousName != selected.Name
	if previousName == "" {
		changed = true
	}
	transitionAt := now
	if !changed {
		transitionAt = lastTransitionAt
		if transitionAt.IsZero() {
			transitionAt = now
		}
	}
	status := map[string]any{
		"phase":              PhaseApplied,
		"selectedCandidate":  selected.Name,
		"selectedSource":     selected.Source,
		"selectedDevice":     selected.Device,
		"selectedGateway":    selected.Gateway,
		"selectedRouteTable": selected.RouteTable,
		"selectedMetric":     selected.Metric,
		"selectedWeight":     selected.Weight,
		"lastTransitionAt":   transitionAt.UTC().Format(time.RFC3339Nano),
		"hysteresis":         hysteresis.String(),
		"dryRun":             true,
		"candidates":         statusCandidates(candidates),
	}
	if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "WANEgressPolicy", resource.Metadata.Name, status); err != nil {
		return err
	}
	if changed {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, EventRouteChanged, daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "WANEgressPolicy", Name: resource.Metadata.Name}
		event.Attributes = map[string]string{
			"selectedCandidate": selected.Name,
			"selectedDevice":    selected.Device,
			"selectedGateway":   selected.Gateway,
			"dryRun":            "true",
		}
		return c.Bus.Publish(ctx, event)
	}
	return nil
}

func (c Controller) candidateStates(spec api.WANEgressPolicySpec) []CandidateState {
	out := make([]CandidateState, 0, len(spec.Candidates))
	for i, candidate := range spec.Candidates {
		name := candidate.Name
		if name == "" {
			name = candidate.Source
		}
		if name == "" {
			name = "candidate-" + strconv.Itoa(i)
		}
		out = append(out, CandidateState{
			Name:       name,
			Source:     candidate.Source,
			Device:     valueFromRef(c.Store, candidate.Device),
			Gateway:    valueFromRef(c.Store, candidate.Gateway),
			RouteTable: candidate.RouteTable,
			Metric:     candidate.Metric,
			Ready:      c.ready(candidate),
			Weight:     candidate.Weight,
			Index:      i,
		})
	}
	return out
}

func (c Controller) ready(candidate api.WANEgressPolicyCandidate) bool {
	if candidate.HealthCheck != "" {
		status := c.Store.ObjectStatus(api.NetAPIVersion, "HealthCheck", candidate.HealthCheck)
		if fmt.Sprint(status["phase"]) != "Healthy" {
			return false
		}
	}
	if len(candidate.ReadyWhen) == 0 {
		if candidate.Source == "" {
			return true
		}
		kind, name, ok := splitResourceRef(candidate.Source)
		if !ok {
			return false
		}
		return readyPhase(c.Store.ObjectStatus(api.NetAPIVersion, kind, name)["phase"])
	}
	for _, predicate := range candidate.ReadyWhen {
		if !evalReadyWhen(c.Store, predicate) {
			return false
		}
	}
	return true
}

func selectHighestWeightReady(candidates []CandidateState) (CandidateState, bool) {
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
		if ready[i].Name != ready[j].Name {
			return ready[i].Name < ready[j].Name
		}
		return ready[i].Index < ready[j].Index
	})
	return ready[0], true
}

func evalReadyWhen(store Store, predicate api.ReadyWhenSpec) bool {
	if len(predicate.AnyOf) > 0 {
		for _, group := range predicate.AnyOf {
			if evalReadyWhenGroup(store, group) {
				return true
			}
		}
		return false
	}
	return evalPredicate(store, api.ReadyWhenPredicateSpec{Field: predicate.Field, Equals: predicate.Equals, NotEmpty: predicate.NotEmpty})
}

func evalReadyWhenGroup(store Store, group []api.ReadyWhenPredicateSpec) bool {
	for _, predicate := range group {
		if !evalPredicate(store, predicate) {
			return false
		}
	}
	return len(group) > 0
}

func evalPredicate(store Store, predicate api.ReadyWhenPredicateSpec) bool {
	value := valueFromRef(store, predicate.Field)
	if predicate.NotEmpty && strings.TrimSpace(value) == "" {
		return false
	}
	if predicate.Equals != "" && value != predicate.Equals {
		return false
	}
	if predicate.Field != "" && !predicate.NotEmpty && predicate.Equals == "" {
		return strings.TrimSpace(value) != ""
	}
	return true
}

func valueFromRef(store Store, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(ref, "${"), "}")
	if !strings.Contains(inner, ".status.") {
		return ref
	}
	parts := strings.SplitN(inner, ".status.", 2)
	kind, name, ok := splitResourceRef(parts[0])
	if !ok {
		return ""
	}
	value := store.ObjectStatus(api.NetAPIVersion, kind, name)[parts[1]]
	switch typed := value.(type) {
	case string:
		return typed
	case []string:
		data, _ := json.Marshal(typed)
		return string(data)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, fmt.Sprint(item))
		}
		data, _ := json.Marshal(out)
		return string(data)
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
}

func splitResourceRef(ref string) (string, string, bool) {
	parts := strings.Split(strings.TrimSpace(ref), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func readyPhase(value any) bool {
	switch fmt.Sprint(value) {
	case "Applied", "Bound", "Healthy", "Installed", "Ready", "Running", "Up":
		return true
	default:
		return false
	}
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
			"name":       candidate.Name,
			"source":     candidate.Source,
			"device":     candidate.Device,
			"gateway":    candidate.Gateway,
			"routeTable": candidate.RouteTable,
			"metric":     candidate.Metric,
			"weight":     candidate.Weight,
			"ready":      candidate.Ready,
		})
	}
	return out
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
