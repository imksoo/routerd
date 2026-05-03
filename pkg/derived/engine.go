package derived

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
)

const (
	EmitAllTrue     = "all_true"
	EmitAnyTrue     = "any_true"
	RetractAnyFalse = "any_false"
	RetractAllFalse = "all_false"

	PhaseAsserted  = "Asserted"
	PhasePending   = "Pending"
	PhaseRetracted = "Retracted"
	PhaseInactive  = "Inactive"

	PendingNone       = "None"
	PendingAsserting  = "Asserting"
	PendingRetracting = "Retracting"
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

	mu    sync.Mutex
	state map[string]*derivedState
}

type derivedState struct {
	initialized       bool
	asserted          bool
	pendingTransition string
	pendingTarget     bool
	timer             *time.Timer
	lastAssertedAt    time.Time
	lastRetractedAt   time.Time
}

type Reader interface {
	Value(path string) (string, bool)
}

type Engine interface {
	Reconcile(ctx context.Context, resource api.Resource, reader Reader) ([]daemonapi.DaemonEvent, error)
}

func (c *Controller) Start(ctx context.Context) {
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
		c.reconcileLogged(ctx, false)
		for {
			select {
			case event, ok := <-ch:
				if !ok {
					return
				}
				if event.Daemon.Kind == "routerd-derived" {
					continue
				}
				c.reconcileLogged(ctx, false)
			case <-ticker.C:
				c.reconcileLogged(ctx, false)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c *Controller) reconcileLogged(ctx context.Context, emitInitial bool) {
	if err := c.Reconcile(ctx, emitInitial); err != nil && c.Logger != nil {
		c.Logger.Warn("derived event reconcile failed", "error", err)
	}
}

func (c *Controller) Reconcile(ctx context.Context, emitInitial bool) error {
	c.init()
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DerivedEvent" {
			continue
		}
		if err := c.reconcileResource(ctx, resource, emitInitial); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) reconcileResource(ctx context.Context, resource api.Resource, forceInitial bool) error {
	spec, err := resource.DerivedEventSpec()
	if err != nil {
		return err
	}
	target := c.target(spec)
	state := c.resourceState(resource.Metadata.Name)
	if !state.initialized {
		state.initialized = true
		state.asserted = target
		if target {
			state.lastAssertedAt = c.now()
		} else {
			state.lastRetractedAt = c.now()
		}
		if spec.EmitInitial || forceInitial {
			return c.publish(ctx, resource, spec, state, target)
		}
		return c.saveStatus(resource, state)
	}
	if state.pendingTransition != PendingNone && target == state.asserted {
		c.cancelPending(state)
		return c.saveStatus(resource, state)
	}
	if target == state.asserted {
		return c.saveStatus(resource, state)
	}
	return c.schedule(ctx, resource, spec, state, target)
}

func (c *Controller) schedule(ctx context.Context, resource api.Resource, spec api.DerivedEventSpec, state *derivedState, target bool) error {
	_ = ctx
	pending := PendingRetracting
	if target {
		pending = PendingAsserting
	}
	if state.pendingTransition == pending && state.pendingTarget == target {
		return c.saveStatus(resource, state)
	}
	c.cancelPending(state)
	state.pendingTransition = pending
	state.pendingTarget = target
	delay := durationOr(spec.Hysteresis, 0)
	if delay == 0 {
		state.asserted = target
		state.pendingTransition = PendingNone
		return c.publish(context.Background(), resource, spec, state, target)
	}
	state.timer = time.AfterFunc(delay, func() {
		if c.target(spec) != target {
			c.mu.Lock()
			c.cancelPending(state)
			c.mu.Unlock()
			_ = c.saveStatus(resource, state)
			return
		}
		state.asserted = target
		state.pendingTransition = PendingNone
		_ = c.publish(context.Background(), resource, spec, state, target)
	})
	return c.saveStatus(resource, state)
}

func (c *Controller) publish(ctx context.Context, resource api.Resource, spec api.DerivedEventSpec, state *derivedState, asserted bool) error {
	now := c.now()
	suffix := ".retracted"
	phase := PhaseRetracted
	if asserted {
		suffix = ".asserted"
		phase = PhaseAsserted
		state.lastAssertedAt = now
	} else {
		state.lastRetractedAt = now
	}
	state.asserted = asserted
	state.pendingTransition = PendingNone
	if err := c.saveStatusWithPhase(resource, state, phase); err != nil {
		return err
	}
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd-derived", Kind: "routerd-derived", Instance: resource.Metadata.Name}, spec.Topic+suffix, daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DerivedEvent", Name: resource.Metadata.Name}
	event.Reason = "DerivedEventAsserted"
	event.Message = "derived event asserted"
	if !asserted {
		event.Reason = "DerivedEventRetracted"
		event.Message = "derived event retracted"
	}
	event.Attributes = map[string]string{"asserted": fmt.Sprintf("%t", asserted)}
	return c.Bus.Publish(ctx, event)
}

func (c *Controller) target(spec api.DerivedEventSpec) bool {
	values := make([]bool, 0, len(spec.Inputs))
	for _, input := range spec.Inputs {
		values = append(values, c.eval(input))
	}
	if len(values) == 0 {
		return false
	}
	switch defaultString(spec.EmitWhen, EmitAllTrue) {
	case EmitAnyTrue:
		for _, value := range values {
			if value {
				return true
			}
		}
		return false
	default:
		for _, value := range values {
			if !value {
				return false
			}
		}
		return true
	}
}

func (c *Controller) eval(input api.ReadyWhenSpec) bool {
	if len(input.AnyOf) > 0 {
		for _, group := range input.AnyOf {
			if c.evalGroup(group) {
				return true
			}
		}
		return false
	}
	return c.evalPredicate(api.ReadyWhenPredicateSpec{Field: input.Field, Equals: input.Equals, NotEmpty: input.NotEmpty})
}

func (c *Controller) evalGroup(group []api.ReadyWhenPredicateSpec) bool {
	for _, input := range group {
		if !c.evalPredicate(input) {
			return false
		}
	}
	return len(group) > 0
}

func (c *Controller) evalPredicate(input api.ReadyWhenPredicateSpec) bool {
	value := c.value(input.Field)
	if input.NotEmpty && strings.TrimSpace(value) == "" {
		return false
	}
	if input.Equals != "" && value != input.Equals {
		return false
	}
	if input.Field != "" && !input.NotEmpty && input.Equals == "" {
		return strings.TrimSpace(value) != ""
	}
	return true
}

func (c *Controller) value(ref string) string {
	ref = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(ref, "${"), "}"))
	if ref == "" || !strings.Contains(ref, ".status.") {
		return ref
	}
	parts := strings.SplitN(ref, ".status.", 2)
	left, field := parts[0], parts[1]
	segments := strings.Split(left, "/")
	if len(segments) != 2 {
		return ""
	}
	status := c.Store.ObjectStatus(api.NetAPIVersion, segments[0], segments[1])
	value := status[field]
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

func (c *Controller) saveStatus(resource api.Resource, state *derivedState) error {
	phase := PhaseInactive
	if state.pendingTransition != PendingNone {
		phase = PhasePending
	} else if state.asserted {
		phase = PhaseAsserted
	} else if !state.lastRetractedAt.IsZero() {
		phase = PhaseRetracted
	}
	return c.saveStatusWithPhase(resource, state, phase)
}

func (c *Controller) saveStatusWithPhase(resource api.Resource, state *derivedState, phase string) error {
	status := map[string]any{
		"phase":             phase,
		"asserted":          state.asserted,
		"pendingTransition": defaultString(state.pendingTransition, PendingNone),
	}
	if !state.lastAssertedAt.IsZero() {
		status["lastAssertedAt"] = state.lastAssertedAt.UTC().Format(time.RFC3339Nano)
	}
	if !state.lastRetractedAt.IsZero() {
		status["lastRetractedAt"] = state.lastRetractedAt.UTC().Format(time.RFC3339Nano)
	}
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "DerivedEvent", resource.Metadata.Name, status)
}

func (c *Controller) resourceState(name string) *derivedState {
	c.init()
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.state[name]
	if state == nil {
		state = &derivedState{pendingTransition: PendingNone}
		c.state[name] = state
	}
	return state
}

func (c *Controller) init() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == nil {
		c.state = map[string]*derivedState{}
	}
}

func (c *Controller) cancelPending(state *derivedState) {
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
	state.pendingTransition = PendingNone
}

func (c *Controller) StopTimers() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, state := range c.state {
		c.cancelPending(state)
	}
}

func (c *Controller) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now().UTC()
}

func durationOr(value string, fallback time.Duration) time.Duration {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
