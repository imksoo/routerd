// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

// DNS consumes the same durable lease projection that the event journal commits
// before local callback delivery. A status-only store cannot establish absence.
func (s eventedStore) ReadDHCPLeaseObservations(ctx context.Context) ([]daemonapi.DHCPLeaseUpdate, error) {
	reader, ok := s.Store.(interface {
		ReadDHCPLeaseObservations(context.Context) ([]daemonapi.DHCPLeaseUpdate, error)
	})
	if !ok {
		return nil, fmt.Errorf("DHCP lease journal observation unavailable")
	}
	return reader.ReadDHCPLeaseObservations(ctx)
}

func (s eventedStore) DHCPLeaseJournalPath() (string, error) {
	reader, ok := s.Store.(interface{ DHCPLeaseJournalPath() (string, error) })
	if !ok {
		return "", fmt.Errorf("DHCP lease journal identity unavailable")
	}
	return reader.DHCPLeaseJournalPath()
}
