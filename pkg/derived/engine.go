// SPDX-License-Identifier: BSD-3-Clause

package derived

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	EmitAllTrue       = "all_true"
	EmitAnyTrue       = "any_true"
	RetractAnyFalse   = "any_false"
	RetractAllFalse   = "all_false"
	PhaseAsserted     = "Asserted"
	PhasePending      = "Pending"
	PhaseRetracted    = "Retracted"
	PendingNone       = "None"
	PendingAsserting  = "Asserting"
	PendingRetracting = "Retracting"
)

// Store is the existing object's current-status owner around the bus journal.
// A feature transition and its committed latch are one SQLite transaction.
type Store interface {
	EventJournal() *routerstate.SQLiteStore
	ReadObjectStatus(string, string, string) (map[string]any, error)
	CommitDerived(context.Context, string, routerstate.DerivedCheckpoint, routerstate.DerivedCheckpoint, *daemonapi.DaemonEvent) ([]daemonapi.DaemonEvent, error)
}

type Controller struct {
	Router   *api.Router
	Bus      *bus.Bus
	Store    Store
	Interval time.Duration
	Now      func() time.Time
	Logger   *slog.Logger

	mu       sync.Mutex
	prepared bool
	rules    []preparedRule
	nextWake time.Time
}

type preparedPredicate struct {
	kind, name, field, literal string
	reference                  bool
	equals                     string
	notEmpty                   bool
}

type preparedRule struct {
	name, topic, definition          string
	inputs                           [][][]preparedPredicate
	emitAny, retractAll, emitInitial bool
	hysteresis                       time.Duration
}

func (c *Controller) prepare() error {
	if c.prepared {
		return nil
	}
	var rules []preparedRule
	if c.Router != nil {
		for _, resource := range c.Router.Spec.Resources {
			if resource.Kind != "DerivedEvent" {
				continue
			}
			spec, err := resource.DerivedEventSpec()
			if err != nil {
				return err
			}
			r, err := prepareRule(resource.Metadata.Name, spec)
			if err != nil {
				return fmt.Errorf("%s: %w", resource.ID(), err)
			}
			rules = append(rules, r)
		}
	}
	if len(rules) > 0 && (c.Store == nil || c.Store.EventJournal() == nil || c.Bus == nil || !c.Bus.PersistsTo(c.Store.EventJournal())) {
		return fmt.Errorf("DerivedEvent status and bus must share the same transactional journal")
	}
	c.rules, c.prepared = rules, true
	return nil
}

func prepareRule(name string, spec api.DerivedEventSpec) (preparedRule, error) {
	r := preparedRule{name: name, topic: spec.Topic, emitInitial: spec.EmitInitial}
	if name == "" || spec.Topic == "" || len(spec.Inputs) == 0 {
		return r, fmt.Errorf("DerivedEvent requires a name, topic and inputs")
	}
	switch defaultString(spec.EmitWhen, EmitAllTrue) {
	case EmitAllTrue:
	case EmitAnyTrue:
		r.emitAny = true
	default:
		return r, fmt.Errorf("invalid assertion guard")
	}
	switch defaultString(spec.RetractWhen, RetractAnyFalse) {
	case RetractAnyFalse:
	case RetractAllFalse:
		r.retractAll = true
	default:
		return r, fmt.Errorf("invalid retraction guard")
	}
	if spec.Hysteresis != "" {
		var err error
		r.hysteresis, err = time.ParseDuration(spec.Hysteresis)
		if err != nil || r.hysteresis < 0 {
			return r, fmt.Errorf("invalid hysteresis %q", spec.Hysteresis)
		}
	}
	for _, input := range spec.Inputs {
		groups := input.AnyOf
		if len(groups) == 0 {
			groups = [][]api.ReadyWhenPredicateSpec{{{Field: input.Field, Equals: input.Equals, NotEmpty: input.NotEmpty}}}
		}
		var prepared [][]preparedPredicate
		for _, group := range groups {
			var predicates []preparedPredicate
			for _, predicate := range group {
				p, err := preparePredicate(predicate)
				if err != nil {
					return r, err
				}
				predicates = append(predicates, p)
			}
			prepared = append(prepared, predicates)
		}
		r.inputs = append(r.inputs, prepared)
	}
	data, err := json.Marshal(spec)
	if err != nil {
		return r, err
	}
	r.definition = fmt.Sprintf("%x", sha256.Sum256(data))
	return r, nil
}

func preparePredicate(input api.ReadyWhenPredicateSpec) (preparedPredicate, error) {
	p := preparedPredicate{equals: input.Equals, notEmpty: input.NotEmpty}
	ref := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(input.Field, "${"), "}"))
	if !strings.Contains(ref, ".status.") {
		p.literal = ref
		return p, nil
	}
	parts := strings.SplitN(ref, ".status.", 2)
	owner := strings.Split(parts[0], "/")
	if len(owner) != 2 || owner[0] == "" || owner[1] == "" || parts[1] == "" {
		return p, fmt.Errorf("invalid status reference %q", input.Field)
	}
	p.kind, p.name, p.field, p.reference = owner[0], owner[1], parts[1], true
	return p, nil
}

// Start owns a single timer and all reconciliation. Once done closes, no
// detached timer callback can read state or publish a transition.
func (c *Controller) Start(ctx context.Context) (<-chan struct{}, error) {
	c.mu.Lock()
	err := c.prepare()
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	if len(c.rules) == 0 {
		close(done)
		return done, nil
	}
	ch, unsubscribe := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.**"}}, 64)
	go func() {
		defer close(done)
		defer unsubscribe()
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-ch:
				if !ok {
					return
				}
				if event.Daemon.Kind == "routerd-derived" {
					continue
				}
			case <-timer.C:
			}
			if err := c.Reconcile(ctx, false); err != nil && ctx.Err() == nil && c.Logger != nil {
				c.Logger.Warn("derived event reconcile failed", "error", err)
			}
			c.mu.Lock()
			delay := c.Interval
			if delay <= 0 {
				delay = 5 * time.Second
			}
			if !c.nextWake.IsZero() {
				until := c.nextWake.Sub(c.now())
				if until > 0 && until < delay {
					delay = until
				}
			}
			c.mu.Unlock()
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(delay)
		}
	}()
	return done, nil
}

func (c *Controller) Reconcile(ctx context.Context, forceInitial bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.prepare(); err != nil {
		return err
	}
	c.nextWake = time.Time{}
	for _, r := range c.rules {
		if err := c.reconcileRule(ctx, r, forceInitial); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) reconcileRule(ctx context.Context, r preparedRule, forceInitial bool) error {
	status, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "DerivedEvent", r.name)
	if err != nil {
		return err
	}
	expected, err := routerstate.DecodeDerivedCheckpoint(status)
	if err != nil {
		return err
	}
	next := expected
	if next.Definition != r.definition {
		next = routerstate.DerivedCheckpoint{Definition: r.definition, Revision: expected.Revision}
	}
	target, err := c.target(r, next.Asserted)
	if err != nil {
		return fmt.Errorf("DerivedEvent/%s inputs: %w", r.name, err)
	}
	now := c.now()
	publish := false
	if !next.Initialized {
		next.Initialized, next.Asserted = true, target
		setTransitionTime(&next, now)
		publish = r.emitInitial || forceInitial
	} else if target == next.Asserted {
		next.PendingDue, next.PendingTarget = time.Time{}, false
	} else if r.hysteresis == 0 {
		next.Asserted = target
		next.PendingDue, next.PendingTarget = time.Time{}, false
		setTransitionTime(&next, now)
		publish = true
	} else if next.PendingDue.IsZero() || next.PendingTarget != target {
		next.PendingDue, next.PendingTarget = now.Add(r.hysteresis), target
	} else if !now.Before(next.PendingDue) {
		next.Asserted = target
		next.PendingDue, next.PendingTarget = time.Time{}, false
		setTransitionTime(&next, now)
		publish = true
	}
	if !next.PendingDue.IsZero() && (c.nextWake.IsZero() || next.PendingDue.Before(c.nextWake)) {
		c.nextWake = next.PendingDue
	}
	if next == expected {
		return nil
	}
	var output *daemonapi.DaemonEvent
	if publish {
		event := transitionEvent(r, next.Asserted, now)
		output = &event
	}
	events, err := c.Store.CommitDerived(ctx, r.name, expected, next, output)
	if err != nil {
		return err
	}
	for _, event := range events {
		if err := c.Bus.PublishRecorded(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func setTransitionTime(next *routerstate.DerivedCheckpoint, now time.Time) {
	if next.Asserted {
		next.LastAssertedAt = now
	} else {
		next.LastRetractedAt = now
	}
}

func transitionEvent(r preparedRule, asserted bool, now time.Time) daemonapi.DaemonEvent {
	suffix, reason, message := ".retracted", "DerivedEventRetracted", "derived event retracted"
	if asserted {
		suffix, reason, message = ".asserted", "DerivedEventAsserted", "derived event asserted"
	}
	e := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd-derived", Kind: "routerd-derived", Instance: r.name}, r.topic+suffix, daemonapi.SeverityInfo)
	e.Time = now
	e.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DerivedEvent", Name: r.name}
	e.Reason, e.Message = reason, message
	e.Attributes = map[string]string{"asserted": fmt.Sprintf("%t", asserted)}
	return e
}

// All predicates referring to the same object use the same error-bearing read.
// Both independent guards consume the complete collected input snapshot.
func (c *Controller) target(r preparedRule, asserted bool) (bool, error) {
	statuses := map[string]map[string]any{}
	allTrue, anyTrue := true, false
	for _, groups := range r.inputs {
		matched := false
		for _, group := range groups {
			groupMatched := len(group) > 0
			for _, p := range group {
				value := p.literal
				if p.reference {
					key := p.kind + "/" + p.name
					status, found := statuses[key]
					if !found {
						var err error
						status, err = c.Store.ReadObjectStatus(api.NetAPIVersion, p.kind, p.name)
						if err != nil {
							return false, err
						}
						statuses[key] = status
					}
					value = statusValue(status[p.field])
				}
				if p.notEmpty && strings.TrimSpace(value) == "" || p.equals != "" && value != p.equals || p.reference && !p.notEmpty && p.equals == "" && strings.TrimSpace(value) == "" {
					groupMatched = false
				}
			}
			matched = matched || groupMatched
		}
		allTrue, anyTrue = allTrue && matched, anyTrue || matched
	}
	emit := allTrue
	if r.emitAny {
		emit = anyTrue
	}
	if emit {
		return true, nil
	}
	retract := !allTrue
	if r.retractAll {
		retract = !anyTrue
	}
	if retract {
		return false, nil
	}
	return asserted, nil
}

func statusValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
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
		return fmt.Sprint(value)
	}
}

func (c *Controller) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
