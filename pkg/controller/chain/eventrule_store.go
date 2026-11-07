// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type eventRuleJournalProvider interface {
	EventJournal() *routerstate.SQLiteStore
}

// The existing status decorator owns declaration metadata. Its concrete
// journal getter also proves that atomic results and bus wakeups share a DB.
type eventRuleStore struct{ eventedStore }

func (s eventRuleStore) EventJournal() *routerstate.SQLiteStore {
	provider, ok := s.Store.(eventRuleJournalProvider)
	if !ok {
		return nil
	}
	return provider.EventJournal()
}

func (s eventRuleStore) CommitRule(ctx context.Context, expected, next routerstate.EventRuleCheckpoint, output *daemonapi.DaemonEvent, atTip bool) (routerstate.EventRuleCheckpoint, []daemonapi.DaemonEvent, error) {
	journal := s.EventJournal()
	if journal == nil {
		return expected, nil, fmt.Errorf("EventRule journal unavailable")
	}
	resource, found := s.resourceForStatus(api.NetAPIVersion, "EventRule", next.Name)
	metadata := statusWithLifecycle(api.NetAPIVersion, "EventRule", next.Name, resource, found, nil)
	build := statusTransitionEventBuilder(api.NetAPIVersion, "EventRule", next.Name)
	return journal.CommitEventRule(ctx, expected, next, output, atTip, metadata, func(current, status map[string]any) (bool, *daemonapi.DaemonEvent) {
		write, event := build(current, status)
		// This is the same rule owner output, not new independent input for a
		// rule matching its own status notification.
		if event != nil {
			event.Daemon.Kind = "routerd-eventrule"
		}
		return write, event
	})
}
