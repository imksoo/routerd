// SPDX-License-Identifier: BSD-3-Clause

package eventrule

import (
	"time"

	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// evaluate mutates only a candidate copy. No timer, cursor, public status or
// output changes until the journal owner commits that candidate atomically.
func evaluate(rule preparedRule, checkpoint *routerstate.EventRuleCheckpoint, event daemonapi.DaemonEvent) (*daemonapi.DaemonEvent, error) {
	pattern := rule.spec.Pattern
	matched := false
	for _, topic := range rule.topics {
		matched = matched || bus.MatchTopic(topic, event.Type)
	}
	expected := pattern.Operator == OperatorAbsence && pattern.Expected != "" && bus.MatchTopic(pattern.Expected, event.Type)
	if !matched && !expected {
		return nil, nil
	}
	key, ok := correlationKey(event, pattern)
	if !ok {
		checkpoint.WarningCount++
		return nil, nil
	}
	state := checkpoint.Correlations[key]
	state.LastSeen = event.Time
	if state.LastSeen.IsZero() {
		state.LastSeen = time.Now().UTC()
	}
	fired, count := false, 0
	switch pattern.Operator {
	case OperatorAllOf:
		if state.Latest == nil {
			state.Latest = map[string]bool{}
		}
		for _, topic := range rule.topics {
			if bus.MatchTopic(topic, event.Type) {
				state.Latest[topic] = true
			}
		}
		fired = true
		for _, topic := range rule.topics {
			fired = fired && state.Latest[topic]
		}
	case OperatorAnyOf:
		fired = matched
	case OperatorSequence:
		if bus.MatchTopic(rule.topics[state.Sequence], event.Type) {
			state.Sequence++
			if state.Sequence == len(rule.topics) {
				state.Sequence = 0
				fired = true
			}
		} else if pattern.Strict {
			state.Sequence = 0
		}
	case OperatorWindow:
		cutoff := event.Time.Add(-rule.window)
		kept := make([]time.Time, 0, len(state.Window)+1)
		for _, at := range state.Window {
			if !at.Before(cutoff) {
				kept = append(kept, at)
			}
		}
		state.Window = append(kept, event.Time)
		threshold := max(1, pattern.Threshold)
		if len(state.Window) >= threshold {
			count = len(state.Window)
			state.Window = nil
			fired = true
		}
	case OperatorAbsence:
		if expected {
			state.Timer = nil
		} else {
			trigger := pattern.Trigger
			if trigger == "" {
				trigger = pattern.Topic
			}
			if bus.MatchTopic(trigger, event.Type) {
				state.Timer = &routerstate.EventRuleTimer{Due: event.Time.Add(rule.duration), Input: event}
			}
		}
	case OperatorThrottle:
		if state.LastEmit.IsZero() || !event.Time.Before(state.LastEmit.Add(rule.interval)) {
			state.LastEmit = event.Time
			fired = true
		}
	case OperatorDebounce:
		state.Timer = &routerstate.EventRuleTimer{Due: event.Time.Add(rule.quiet), Input: event}
	case OperatorCount:
		state.Count++
		if state.Count >= max(1, pattern.Threshold) {
			count = state.Count
			state.Count = 0
			fired = true
		}
	}
	checkpoint.Correlations[key] = state
	for len(checkpoint.Correlations) > maxRuleCorrelationKeys {
		oldest := ""
		var at time.Time
		first := true
		for candidate, value := range checkpoint.Correlations {
			if first || value.LastSeen.Before(at) || value.LastSeen.Equal(at) && candidate < oldest {
				oldest, at, first = candidate, value.LastSeen, false
			}
		}
		delete(checkpoint.Correlations, oldest)
	}
	if !fired {
		return nil, nil
	}
	checkpoint.FireCount++
	checkpoint.LastFiredAt = event.Time
	output := emitEvent(rule, event, key, count)
	return &output, nil
}
