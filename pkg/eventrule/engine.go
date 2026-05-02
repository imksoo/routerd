package eventrule

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
)

const (
	OperatorAllOf    = "all_of"
	OperatorAnyOf    = "any_of"
	OperatorSequence = "sequence"
	OperatorWindow   = "window"
	OperatorAbsence  = "absence"
	OperatorThrottle = "throttle"
	OperatorDebounce = "debounce"
	OperatorCount    = "count"

	PhaseActive = "Active"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type Controller struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
	Now    func() time.Time
	Logger *slog.Logger

	mu    sync.Mutex
	state map[string]*ruleState
}

type ruleState struct {
	latest       map[string]daemonapi.DaemonEvent
	sequence     map[string]int
	windows      map[string][]time.Time
	lastEmit     map[string]time.Time
	counts       map[string]int
	absence      map[string]*time.Timer
	debounce     map[string]*time.Timer
	fireCount    int
	lastFiredAt  time.Time
	warningCount int
}

type Engine interface {
	Reconcile(ctx context.Context, rule api.Resource, event daemonapi.DaemonEvent) ([]daemonapi.DaemonEvent, error)
}

func (c *Controller) Start(ctx context.Context) {
	if c.Router == nil || c.Bus == nil || c.Store == nil {
		return
	}
	c.init()
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.**"}}, 128)
	go func() {
		for {
			select {
			case event, ok := <-ch:
				if !ok {
					return
				}
				if event.Daemon.Kind == "routerd-eventrule" {
					continue
				}
				if err := c.Reconcile(ctx, event); err != nil && c.Logger != nil {
					c.Logger.Warn("event rule reconcile failed", "error", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c *Controller) Reconcile(ctx context.Context, event daemonapi.DaemonEvent) error {
	c.init()
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "EventRule" {
			continue
		}
		events, err := c.reconcileRule(ctx, resource, event)
		if err != nil {
			return err
		}
		for _, emitted := range events {
			if err := c.publish(ctx, resource, emitted); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Controller) reconcileRule(ctx context.Context, resource api.Resource, event daemonapi.DaemonEvent) ([]daemonapi.DaemonEvent, error) {
	_ = ctx
	spec, err := resource.EventRuleSpec()
	if err != nil {
		return nil, err
	}
	key, ok := correlationKey(event, spec.Pattern)
	if !ok {
		c.warn(resource.Metadata.Name)
		return nil, nil
	}
	state := c.ruleState(resource.Metadata.Name)
	now := c.eventTime(event)
	var out []daemonapi.DaemonEvent
	switch spec.Pattern.Operator {
	case OperatorAllOf:
		out = c.evalAllOf(resource, spec, state, key, event)
	case OperatorAnyOf:
		if eventMatchesAny(spec.Pattern, event.Type) {
			out = []daemonapi.DaemonEvent{c.emitEvent(resource, spec, event, key, 0)}
		}
	case OperatorSequence:
		out = c.evalSequence(resource, spec, state, key, event)
	case OperatorWindow:
		out = c.evalWindow(resource, spec, state, key, event, now)
	case OperatorAbsence:
		c.evalAbsence(resource, spec, state, key, event)
	case OperatorThrottle:
		out = c.evalThrottle(resource, spec, state, key, event, now)
	case OperatorDebounce:
		c.evalDebounce(resource, spec, state, key, event)
	case OperatorCount:
		out = c.evalCount(resource, spec, state, key, event)
	}
	return out, nil
}

func (c *Controller) evalAllOf(resource api.Resource, spec api.EventRuleSpec, state *ruleState, key string, event daemonapi.DaemonEvent) []daemonapi.DaemonEvent {
	topics := inputTopics(spec.Pattern)
	if !topicInList(event.Type, topics) {
		return nil
	}
	slot := key + "\x00" + event.Type
	state.latest[slot] = event
	for _, topic := range topics {
		if _, ok := state.latest[key+"\x00"+topic]; !ok {
			return nil
		}
	}
	return []daemonapi.DaemonEvent{c.emitEvent(resource, spec, event, key, 0)}
}

func (c *Controller) evalSequence(resource api.Resource, spec api.EventRuleSpec, state *ruleState, key string, event daemonapi.DaemonEvent) []daemonapi.DaemonEvent {
	topics := inputTopics(spec.Pattern)
	if len(topics) == 0 {
		return nil
	}
	idx := state.sequence[key]
	if event.Type == topics[idx] {
		idx++
		if idx == len(topics) {
			state.sequence[key] = 0
			return []daemonapi.DaemonEvent{c.emitEvent(resource, spec, event, key, 0)}
		}
		state.sequence[key] = idx
		return nil
	}
	if spec.Pattern.Strict && topicInList(event.Type, topics) {
		state.sequence[key] = 0
	}
	return nil
}

func (c *Controller) evalWindow(resource api.Resource, spec api.EventRuleSpec, state *ruleState, key string, event daemonapi.DaemonEvent, now time.Time) []daemonapi.DaemonEvent {
	if !eventMatchesAny(spec.Pattern, event.Type) {
		return nil
	}
	window := durationOr(spec.Pattern.Window, durationOr(spec.Pattern.Duration, time.Minute))
	threshold := spec.Pattern.Threshold
	if threshold == 0 {
		threshold = 1
	}
	cutoff := now.Add(-window)
	events := append(state.windows[key], now)
	kept := events[:0]
	for _, at := range events {
		if !at.Before(cutoff) {
			kept = append(kept, at)
		}
	}
	state.windows[key] = kept
	if len(kept) < threshold {
		return nil
	}
	state.windows[key] = nil
	return []daemonapi.DaemonEvent{c.emitEvent(resource, spec, event, key, len(kept))}
}

func (c *Controller) evalAbsence(resource api.Resource, spec api.EventRuleSpec, state *ruleState, key string, event daemonapi.DaemonEvent) {
	if spec.Pattern.Expected != "" && bus.MatchTopic(spec.Pattern.Expected, event.Type) {
		if timer := state.absence[key]; timer != nil {
			timer.Stop()
			delete(state.absence, key)
		}
		return
	}
	trigger := spec.Pattern.Trigger
	if trigger == "" {
		trigger = spec.Pattern.Topic
	}
	if trigger == "" || !bus.MatchTopic(trigger, event.Type) {
		return
	}
	if timer := state.absence[key]; timer != nil {
		timer.Stop()
	}
	delay := durationOr(spec.Pattern.Duration, time.Minute)
	state.absence[key] = time.AfterFunc(delay, func() {
		emitted := c.emitEvent(resource, spec, event, key, 0)
		_ = c.publish(context.Background(), resource, emitted)
	})
}

func (c *Controller) evalThrottle(resource api.Resource, spec api.EventRuleSpec, state *ruleState, key string, event daemonapi.DaemonEvent, now time.Time) []daemonapi.DaemonEvent {
	if !eventMatchesAny(spec.Pattern, event.Type) {
		return nil
	}
	interval := durationOr(spec.Pattern.Interval, durationOr(spec.Pattern.Duration, time.Minute))
	last := state.lastEmit[key]
	if !last.IsZero() && now.Sub(last) < interval {
		return nil
	}
	state.lastEmit[key] = now
	return []daemonapi.DaemonEvent{c.emitEvent(resource, spec, event, key, 0)}
}

func (c *Controller) evalDebounce(resource api.Resource, spec api.EventRuleSpec, state *ruleState, key string, event daemonapi.DaemonEvent) {
	if !eventMatchesAny(spec.Pattern, event.Type) {
		return
	}
	if timer := state.debounce[key]; timer != nil {
		timer.Stop()
	}
	quiet := durationOr(spec.Pattern.Quiet, durationOr(spec.Pattern.Duration, time.Second))
	state.debounce[key] = time.AfterFunc(quiet, func() {
		emitted := c.emitEvent(resource, spec, event, key, 0)
		_ = c.publish(context.Background(), resource, emitted)
	})
}

func (c *Controller) evalCount(resource api.Resource, spec api.EventRuleSpec, state *ruleState, key string, event daemonapi.DaemonEvent) []daemonapi.DaemonEvent {
	if !eventMatchesAny(spec.Pattern, event.Type) {
		return nil
	}
	state.counts[key]++
	threshold := spec.Pattern.Threshold
	if threshold == 0 {
		threshold = 1
	}
	if state.counts[key] < threshold {
		return nil
	}
	count := state.counts[key]
	state.counts[key] = 0
	return []daemonapi.DaemonEvent{c.emitEvent(resource, spec, event, key, count)}
}

func (c *Controller) emitEvent(resource api.Resource, spec api.EventRuleSpec, input daemonapi.DaemonEvent, correlation string, count int) daemonapi.DaemonEvent {
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd-eventrule", Kind: "routerd-eventrule", Instance: resource.Metadata.Name}, spec.Emit.Topic, daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "EventRule", Name: resource.Metadata.Name}
	event.Reason = "EventRuleFired"
	event.Message = "event rule fired"
	event.Attributes = renderAttributes(spec.Emit.Attributes, input, correlation, count)
	if correlation != "" {
		event.Attributes["correlation"] = correlation
	}
	return event
}

func (c *Controller) publish(ctx context.Context, resource api.Resource, event daemonapi.DaemonEvent) error {
	state := c.ruleState(resource.Metadata.Name)
	state.fireCount++
	state.lastFiredAt = c.eventTime(event)
	status := map[string]any{
		"phase":       PhaseActive,
		"lastFiredAt": state.lastFiredAt.UTC().Format(time.RFC3339Nano),
		"fireCount":   state.fireCount,
	}
	if state.warningCount > 0 {
		status["warningCount"] = state.warningCount
	}
	if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "EventRule", resource.Metadata.Name, status); err != nil {
		return err
	}
	return c.Bus.Publish(ctx, event)
}

func (c *Controller) warn(name string) {
	state := c.ruleState(name)
	state.warningCount++
	_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "EventRule", name, map[string]any{"phase": PhaseActive, "warningCount": state.warningCount})
}

func (c *Controller) ruleState(name string) *ruleState {
	c.init()
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.state[name]
	if state == nil {
		state = &ruleState{
			latest:   map[string]daemonapi.DaemonEvent{},
			sequence: map[string]int{},
			windows:  map[string][]time.Time{},
			lastEmit: map[string]time.Time{},
			counts:   map[string]int{},
			absence:  map[string]*time.Timer{},
			debounce: map[string]*time.Timer{},
		}
		c.state[name] = state
	}
	return state
}

func (c *Controller) init() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == nil {
		c.state = map[string]*ruleState{}
	}
}

func (c *Controller) eventTime(event daemonapi.DaemonEvent) time.Time {
	if !event.Time.IsZero() {
		return event.Time
	}
	if c.Now != nil {
		return c.Now()
	}
	return time.Now().UTC()
}

func eventMatchesAny(pattern api.EventRulePatternSpec, topic string) bool {
	for _, candidate := range inputTopics(pattern) {
		if bus.MatchTopic(candidate, topic) {
			return true
		}
	}
	return false
}

func inputTopics(pattern api.EventRulePatternSpec) []string {
	var topics []string
	topics = append(topics, pattern.Topics...)
	if pattern.Topic != "" {
		topics = append(topics, pattern.Topic)
	}
	if pattern.Trigger != "" {
		topics = append(topics, pattern.Trigger)
	}
	return topics
}

func topicInList(topic string, topics []string) bool {
	for _, candidate := range topics {
		if candidate == topic {
			return true
		}
	}
	return false
}

func correlationKey(event daemonapi.DaemonEvent, pattern api.EventRulePatternSpec) (string, bool) {
	path := strings.TrimSpace(pattern.CorrelateBy)
	if path == "" {
		return "default", true
	}
	switch {
	case strings.HasPrefix(path, "attributes."):
		key := strings.TrimPrefix(path, "attributes.")
		value := event.Attributes[key]
		if value == "" && !pattern.AllowMissingCorrelation {
			return "", false
		}
		return value, true
	case path == "resource.name":
		if event.Resource == nil {
			return "", pattern.AllowMissingCorrelation
		}
		return event.Resource.Name, true
	case path == "resource.kind":
		if event.Resource == nil {
			return "", pattern.AllowMissingCorrelation
		}
		return event.Resource.Kind, true
	case path == "resource.apiVersion":
		if event.Resource == nil {
			return "", pattern.AllowMissingCorrelation
		}
		return event.Resource.APIVersion, true
	case path == "daemon.instance":
		if event.Daemon.Instance == "" && !pattern.AllowMissingCorrelation {
			return "", false
		}
		return event.Daemon.Instance, true
	case path == "daemon.kind":
		if event.Daemon.Kind == "" && !pattern.AllowMissingCorrelation {
			return "", false
		}
		return event.Daemon.Kind, true
	default:
		return "", false
	}
}

func renderAttributes(template map[string]string, input daemonapi.DaemonEvent, correlation string, count int) map[string]string {
	out := map[string]string{}
	for key, value := range template {
		out[key] = renderValue(value, input, correlation, count)
	}
	if count > 0 {
		out["count"] = strconv.Itoa(count)
	}
	return out
}

func renderValue(value string, input daemonapi.DaemonEvent, correlation string, count int) string {
	replacements := map[string]string{
		"${event.type}":  input.Type,
		"${correlation}": correlation,
		"${count}":       strconv.Itoa(count),
	}
	if input.Resource != nil {
		replacements["${resource.name}"] = input.Resource.Name
		replacements["${resource.kind}"] = input.Resource.Kind
		replacements["${resource.apiVersion}"] = input.Resource.APIVersion
	}
	for key, attr := range input.Attributes {
		replacements["${attributes."+key+"}"] = attr
	}
	for old, replacement := range replacements {
		value = strings.ReplaceAll(value, old, replacement)
	}
	return value
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

func (c *Controller) StopTimers() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, state := range c.state {
		for _, timer := range state.absence {
			timer.Stop()
		}
		for _, timer := range state.debounce {
			timer.Stop()
		}
	}
}
