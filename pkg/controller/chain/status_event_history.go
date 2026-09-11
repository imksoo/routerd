// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type atomicStatusEventStore interface {
	bus.EventStore
	SaveObjectStatusAndEvent(string, string, string, map[string]any, routerstate.StatusEventBuilder) (*daemonapi.DaemonEvent, error)
	MergeObjectStatusAndEvent(string, string, string, map[string]any, routerstate.StatusEventBuilder) (*daemonapi.DaemonEvent, error)
}

// EventRule count/sequence/window/absence patterns consume history. Their
// inputs cannot be reconstructed from a later status rescan. Only rules that
// actually match status transitions require this capability; notification-only
// controllers and lightweight unused stores retain their existing contract.
func statusEventHistoryRequired(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "EventRule" {
			continue
		}
		spec, err := resource.EventRuleSpec()
		if err != nil {
			continue
		} // Configuration validation owns malformed rules.
		topics := append([]string{spec.Pattern.Topic, spec.Pattern.Trigger, spec.Pattern.Expected}, spec.Pattern.Topics...)
		for _, topic := range topics {
			if bus.MatchTopic(topic, "routerd.resource.status.changed") {
				return true
			}
		}
	}
	return false
}

func (s eventedStore) saveStatusWithHistory(apiVersion, kind, name string, updates map[string]any, merge bool) error {
	store, ok := s.Store.(atomicStatusEventStore)
	if !ok || s.Bus == nil || !s.Bus.PersistsTo(store) {
		return fmt.Errorf("EventRule status history requires status and bus to share a transactional event store")
	}
	build := func(current, next map[string]any) (bool, *daemonapi.DaemonEvent) {
		if newerStatus(current, next) {
			return false, nil
		}
		if !shouldPublishStatusChangedEvent(apiVersion, kind, current, next) {
			return true, nil
		}
		changedFields := statusChangedFieldsForEvent(apiVersion, kind, current, next)
		event := daemonapi.DaemonEvent{
			TypeMeta: daemonapi.TypeMeta{APIVersion: daemonapi.APIVersion, Kind: daemonapi.KindDaemonEvent},
			Daemon:   daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "store"},
			Type:     "routerd.resource.status.changed",
			Severity: statusChangedEventSeverity(apiVersion, kind, current, next, changedFields),
		}
		event.Resource = &daemonapi.ResourceRef{APIVersion: apiVersion, Kind: kind, Name: name}
		event.Attributes = map[string]string{"phase": fmt.Sprint(next["phase"]), "previousPhase": fmt.Sprint(current["phase"]), "changedFields": strings.Join(changedFields, ",")}
		return true, &event
	}
	var event *daemonapi.DaemonEvent
	var err error
	if merge {
		event, err = store.MergeObjectStatusAndEvent(apiVersion, kind, name, updates, build)
	} else {
		event, err = store.SaveObjectStatusAndEvent(apiVersion, kind, name, updates, build)
	}
	if err != nil {
		return fmt.Errorf("commit status and required EventRule history: %w", err)
	}
	if event == nil {
		return nil
	}
	return s.Bus.PublishRecorded(context.Background(), *event)
}
