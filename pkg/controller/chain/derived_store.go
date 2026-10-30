// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// DerivedEvent owns its checkpoint and output together. The existing status
// decorator contributes declaration metadata and its normal status notification.
type derivedStore struct{ eventedStore }

func (s derivedStore) EventJournal() *routerstate.SQLiteStore {
	provider, ok := s.Store.(interface {
		EventJournal() *routerstate.SQLiteStore
	})
	if !ok {
		return nil
	}
	return provider.EventJournal()
}

func (s derivedStore) CommitDerived(ctx context.Context, name string, expected, next routerstate.DerivedCheckpoint, output *daemonapi.DaemonEvent) ([]daemonapi.DaemonEvent, error) {
	journal := s.EventJournal()
	if journal == nil {
		return nil, fmt.Errorf("DerivedEvent journal unavailable")
	}
	resource, found := s.resourceForStatus(api.NetAPIVersion, "DerivedEvent", name)
	metadata := statusWithLifecycle(api.NetAPIVersion, "DerivedEvent", name, resource, found, nil)
	build := statusTransitionEventBuilder(api.NetAPIVersion, "DerivedEvent", name)
	return journal.CommitDerivedStatus(ctx, name, expected, next, output, metadata, func(current, status map[string]any) (bool, *daemonapi.DaemonEvent) {
		write, event := build(current, status)
		if event != nil {
			event.Daemon.Kind = "routerd-derived"
		}
		return write, event
	})
}
