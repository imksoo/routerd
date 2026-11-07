// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"context"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

func (s mapStore) ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error) {
	return s.ObjectStatus(apiVersion, kind, name), nil
}

func (s mapStore) ReadDHCPLeaseObservations(context.Context) ([]daemonapi.DHCPLeaseUpdate, error) {
	return nil, nil
}

func (s mapStore) DHCPLeaseJournalPath() (string, error) { return "/unqualified-test/dhcp.sqlite", nil }
