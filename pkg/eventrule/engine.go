// SPDX-License-Identifier: BSD-3-Clause

package eventrule

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/eventconsumer"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	OperatorAllOf          = "all_of"
	OperatorAnyOf          = "any_of"
	OperatorSequence       = "sequence"
	OperatorWindow         = "window"
	OperatorAbsence        = "absence"
	OperatorThrottle       = "throttle"
	OperatorDebounce       = "debounce"
	OperatorCount          = "count"
	PhaseActive            = "Active"
	maxRuleCorrelationKeys = 4096
)

// Store is the actual public-status owner around one concrete SQLite journal.
// Its commit binds declared ownership metadata to checkpoint/result publication.
type Store interface {
	EventJournal() *routerstate.SQLiteStore
	CommitRule(context.Context, routerstate.EventRuleCheckpoint, routerstate.EventRuleCheckpoint, *daemonapi.DaemonEvent, bool) (routerstate.EventRuleCheckpoint, []daemonapi.DaemonEvent, error)
}

type Controller struct {
	Router        *api.Router
	Bus           *bus.Bus
	Store         Store
	Now           func() time.Time
	Logger        *slog.Logger
	Poll          time.Duration
	mu            sync.Mutex
	prepared      bool
	rules         []preparedRule
	initialCursor int64
	nextWake      time.Time
}

type preparedRule struct {
	resource                          api.Resource
	spec                              api.EventRuleSpec
	definition                        routerstate.EventRuleDefinition
	topics                            []string
	duration, window, quiet, interval time.Duration
}

// Prepare validates static work and captures input position without creating
// consumption state. The selected generation alone calls Start.
func (c *Controller) Prepare(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.prepared {
		return ctx.Err()
	}
	var rules []preparedRule
	if c.Router != nil {
		for _, resource := range c.Router.Spec.Resources {
			if resource.Kind != "EventRule" {
				continue
			}
			spec, err := resource.EventRuleSpec()
			if err != nil {
				return err
			}
			r, err := prepareRule(resource, spec)
			if err != nil {
				return err
			}
			rules = append(rules, r)
		}
	}
	if c.Store == nil || c.Store.EventJournal() == nil {
		if len(rules) > 0 {
			return fmt.Errorf("EventRule requires its transactional journal")
		}
		c.prepared = true
		return ctx.Err()
	}
	if len(rules) > 0 && (c.Bus == nil || !c.Bus.PersistsTo(c.Store.EventJournal())) {
		return fmt.Errorf("EventRule status and bus must share the same journal")
	}
	tip, err := c.Store.EventJournal().EventJournalTip(ctx)
	if err != nil {
		return err
	}
	c.rules, c.initialCursor, c.prepared = rules, tip, true
	return ctx.Err()
}

func prepareRule(resource api.Resource, spec api.EventRuleSpec) (preparedRule, error) {
	r := preparedRule{resource: resource, spec: spec}
	switch spec.Pattern.Operator {
	case OperatorAllOf, OperatorAnyOf, OperatorSequence, OperatorWindow, OperatorAbsence, OperatorThrottle, OperatorDebounce, OperatorCount:
	default:
		return r, fmt.Errorf("%s: unsupported EventRule operator", resource.ID())
	}
	if spec.Pattern.Rate != 0 {
		return r, fmt.Errorf("%s: EventRule rate is not implemented; use interval", resource.ID())
	}
	if spec.Emit.Topic == "" || spec.Pattern.Threshold < 0 {
		return r, fmt.Errorf("%s: invalid EventRule output/threshold", resource.ID())
	}
	for _, value := range []string{spec.Pattern.Duration, spec.Pattern.Window, spec.Pattern.Quiet, spec.Pattern.Interval} {
		if value != "" {
			d, err := time.ParseDuration(value)
			if err != nil || d < 0 {
				return r, fmt.Errorf("%s: invalid EventRule duration %q", resource.ID(), value)
			}
		}
	}
	r.topics = append(r.topics, spec.Pattern.Topics...)
	if spec.Pattern.Topic != "" {
		r.topics = append(r.topics, spec.Pattern.Topic)
	}
	if spec.Pattern.Trigger != "" {
		r.topics = append(r.topics, spec.Pattern.Trigger)
	}
	if len(r.topics) == 0 {
		return r, fmt.Errorf("%s: EventRule requires an input or absence trigger", resource.ID())
	}
	if spec.Pattern.Operator == OperatorAbsence && spec.Pattern.Trigger == "" && spec.Pattern.Topic == "" {
		return r, fmt.Errorf("%s: absence requires trigger or topic", resource.ID())
	}
	if spec.Pattern.CorrelateBy != "" {
		_, valid := correlationKey(daemonapi.DaemonEvent{Attributes: map[string]string{}, Resource: &daemonapi.ResourceRef{}}, api.EventRulePatternSpec{CorrelateBy: spec.Pattern.CorrelateBy, AllowMissingCorrelation: true})
		if !valid {
			return r, fmt.Errorf("%s: invalid EventRule correlation", resource.ID())
		}
	}
	duration := func(value string, fallback time.Duration) time.Duration {
		if value == "" {
			return fallback
		}
		d, _ := time.ParseDuration(value)
		return d
	}
	r.duration = duration(spec.Pattern.Duration, time.Minute)
	r.window = duration(spec.Pattern.Window, r.duration)
	r.interval = duration(spec.Pattern.Interval, r.duration)
	r.quiet = duration(spec.Pattern.Quiet, duration(spec.Pattern.Duration, time.Second))
	data, err := json.Marshal(spec)
	if err != nil {
		return r, err
	}
	r.definition = routerstate.EventRuleDefinition{Name: resource.Metadata.Name, Digest: fmt.Sprintf("%x", sha256.Sum256(data))}
	return r, nil
}

// Start initializes or resumes the selected rules, then owns exactly one loop
// and timer. Cancellation acknowledgement means no rule callback remains live.
func (c *Controller) Start(ctx context.Context) (<-chan struct{}, error) {
	done := make(chan struct{})
	if err := c.Prepare(ctx); err != nil {
		close(done)
		return done, err
	}
	if c.Store == nil || c.Store.EventJournal() == nil {
		close(done)
		return done, nil
	}
	var definitions []routerstate.EventRuleDefinition
	for _, r := range c.rules {
		definitions = append(definitions, r.definition)
	}
	if err := c.Store.EventJournal().InitializeEventRules(ctx, definitions, c.initialCursor); err != nil {
		close(done)
		return done, err
	}
	var checkpoints []routerstate.EventRuleCheckpoint
	for _, r := range c.rules {
		cp, err := c.Store.EventJournal().ReadEventRule(ctx, r.definition.Name)
		if err != nil {
			close(done)
			return done, err
		}
		if err := validateCheckpoint(r, cp); err != nil {
			close(done)
			return done, c.reportFailure(ctx, cp, err)
		}
		if _, _, err := c.Store.EventJournal().EventRuleInputs(ctx, cp.Cursor); err != nil {
			close(done)
			return done, c.reportFailure(ctx, cp, err)
		}
		checkpoints = append(checkpoints, cp)
	}
	// Definition changes can reset a private checkpoint without any new input.
	// Publish the selected checkpoint and declaration metadata immediately, so
	// a preceding definition's fire count cannot remain the current result.
	for _, cp := range checkpoints {
		if _, err := c.commit(ctx, cp, copyCheckpoint(cp), nil, false); err != nil {
			close(done)
			return done, err
		}
	}
	if len(c.rules) == 0 {
		close(done)
		return done, nil
	}
	ch, unsubscribe := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.**"}}, 128)
	go func() {
		defer close(done)
		defer unsubscribe()
		timer := time.NewTimer(0)
		defer timer.Stop()
		var retry eventconsumer.Backoff
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-ch:
				if !ok {
					return
				}
			case <-timer.C:
			}
			if ctx.Err() != nil {
				return
			}
			if retry.Ready() {
				if err := c.drainStoredEvents(ctx); err != nil {
					retry.Failure()
					if ctx.Err() == nil && c.Logger != nil {
						c.Logger.Warn("event rule checkpoint failed", "error", err)
					}
				} else {
					retry.Success()
				}
			}
			poll := c.Poll
			if poll <= 0 {
				poll = time.Second
			}
			c.mu.Lock()
			next := c.nextWake
			c.mu.Unlock()
			if !next.IsZero() {
				poll = min(poll, max(time.Millisecond, next.Sub(c.now())))
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(poll)
		}
	}()
	return done, nil
}

func (c *Controller) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *Controller) drainStoredEvents(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextWake = time.Time{}
rules:
	for _, r := range c.rules {
		if err := ctx.Err(); err != nil {
			return err
		}
		cp, err := c.Store.EventJournal().ReadEventRule(ctx, r.definition.Name)
		if err != nil {
			return err
		}
		if err := validateCheckpoint(r, cp); err != nil {
			return c.reportFailure(ctx, cp, err)
		}
		inputs, _, err := c.Store.EventJournal().EventRuleInputs(ctx, cp.Cursor)
		if err != nil {
			return c.reportFailure(ctx, cp, err)
		}
		if len(inputs) > 0 {
			// A continuously written journal must not let its first rule
			// monopolize this owner. Each rule gets one bounded page per pass.
			for _, input := range inputs {
				next := copyCheckpoint(cp)
				var output *daemonapi.DaemonEvent
				if input.SourceKind != "routerd-eventrule" {
					output, err = evaluate(r, &next, eventconsumer.Event(input))
					if err != nil {
						return c.reportFailure(ctx, cp, err)
					}
				}
				next.Cursor = input.ID
				cp, err = c.commit(ctx, cp, next, output, false)
				if err != nil {
					if errors.Is(err, routerstate.ErrEventRuleConflict) {
						c.wakeAt(c.now())
						continue rules
					}
					return c.reportFailure(ctx, cp, err)
				}
			}
			c.wakeAt(c.now())
			continue
		}
		now := c.now()
		if cp.LastError != "" {
			if _, err := c.commit(ctx, cp, copyCheckpoint(cp), nil, true); err != nil {
				if errors.Is(err, routerstate.ErrEventRuleConflict) {
					c.wakeAt(now)
					continue
				}
				return c.reportFailure(ctx, cp, err)
			}
			c.wakeAt(now)
			continue
		}
		key := ""
		var due time.Time
		for k, state := range cp.Correlations {
			if state.Timer != nil && (due.IsZero() || state.Timer.Due.Before(due) || state.Timer.Due.Equal(due) && k < key) {
				key, due = k, state.Timer.Due
			}
		}
		if due.IsZero() {
			continue
		}
		if due.After(now) {
			c.wakeAt(due)
			continue
		}
		next := copyCheckpoint(cp)
		correlation := next.Correlations[key]
		input := correlation.Timer.Input
		correlation.Timer = nil
		next.Correlations[key] = correlation
		output := emitEvent(r, input, key, 0)
		next.FireCount++
		next.LastFiredAt = now
		if _, err := c.commit(ctx, cp, next, &output, true); err != nil {
			if errors.Is(err, routerstate.ErrEventRuleConflict) {
				c.wakeAt(now)
				continue
			}
			return c.reportFailure(ctx, cp, err)
		}
		c.wakeAt(now)
	}
	return nil
}

// Called only by the loop while holding mu.
func (c *Controller) wakeAt(at time.Time) {
	if c.nextWake.IsZero() || at.Before(c.nextWake) {
		c.nextWake = at
	}
}

func (c *Controller) commit(ctx context.Context, expected, next routerstate.EventRuleCheckpoint, output *daemonapi.DaemonEvent, atTip bool) (routerstate.EventRuleCheckpoint, error) {
	committed, events, err := c.Store.CommitRule(ctx, expected, next, output, atTip)
	if err != nil {
		return expected, err
	}
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return committed, err
		}
		if err := c.Bus.PublishRecorded(ctx, event); err != nil {
			return committed, err
		}
	}
	return committed, nil
}

// Preserve the input position, partial pattern and pending timers while making
// an observable journal failure visible in the same status owner. If the store
// itself cannot be written, return both errors; never claim that error was saved.
func (c *Controller) reportFailure(ctx context.Context, cp routerstate.EventRuleCheckpoint, failure error) error {
	message := failure.Error()
	if len(message) > 2048 {
		message = message[:2048]
	}
	if ctx.Err() != nil || cp.Name == "" || cp.LastError == message {
		return failure
	}
	next := copyCheckpoint(cp)
	next.LastError = message
	_, err := c.commit(ctx, cp, next, nil, false)
	return errors.Join(failure, err)
}

func copyCheckpoint(cp routerstate.EventRuleCheckpoint) routerstate.EventRuleCheckpoint {
	next := cp
	next.LastError = ""
	next.Correlations = make(map[string]routerstate.EventRuleCorrelation, len(cp.Correlations))
	for key, state := range cp.Correlations {
		copy := state
		copy.Latest = make(map[string]bool, len(state.Latest))
		for topic, seen := range state.Latest {
			copy.Latest[topic] = seen
		}
		copy.Window = append([]time.Time(nil), state.Window...)
		if state.Timer != nil {
			timer := *state.Timer
			copy.Timer = &timer
		}
		next.Correlations[key] = copy
	}
	return next
}

func validateCheckpoint(r preparedRule, cp routerstate.EventRuleCheckpoint) error {
	if cp.Definition != r.definition.Digest {
		return fmt.Errorf("EventRule definition differs from its selected checkpoint")
	}
	for _, state := range cp.Correlations {
		if r.spec.Pattern.Operator == OperatorSequence && state.Sequence >= len(r.topics) {
			return fmt.Errorf("EventRule sequence checkpoint is out of range")
		}
		if state.Timer != nil && r.spec.Pattern.Operator != OperatorAbsence && r.spec.Pattern.Operator != OperatorDebounce {
			return fmt.Errorf("EventRule has a timer for a non-timer operator")
		}
	}
	return nil
}

func emitEvent(r preparedRule, input daemonapi.DaemonEvent, correlation string, count int) daemonapi.DaemonEvent {
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd-eventrule", Kind: "routerd-eventrule", Instance: r.resource.Metadata.Name}, r.spec.Emit.Topic, daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "EventRule", Name: r.resource.Metadata.Name}
	event.Reason = "EventRuleFired"
	event.Message = "event rule fired"
	event.Attributes = renderAttributes(r.spec.Emit.Attributes, input, correlation, count)
	if correlation != "" {
		event.Attributes["correlation"] = correlation
	}
	return event
}
