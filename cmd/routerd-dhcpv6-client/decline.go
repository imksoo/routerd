// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"slices"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/pdclient"
)

func decodeDeclineRequest(body io.Reader) (daemonapi.DHCPv6DeclineRequest, error) {
	var request daemonapi.DHCPv6DeclineRequest
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return request, errors.New("Decline requires one JSON request")
	}
	if len(request.Addresses) == 0 || len(request.Addresses) > 2048 {
		return request, errors.New("Decline requires 1..2048 address observations")
	}
	return request, nil
}

// Caller holds d.mu, so validation and lease withdrawal observe one version.
func (d *dhcpv6Daemon) declineLocked(ctx context.Context, request daemonapi.DHCPv6DeclineRequest) error {
	if request.SupervisorOwner != d.opts.supervisorOwner {
		return errors.New("Decline worker ownership changed")
	}
	server, err := hex.DecodeString(request.ServerDUID)
	if err != nil || len(server) == 0 || !bytes.Equal(server, d.client.Lease.ServerDUID) {
		return errors.New("Decline allocating server changed")
	}
	addresses := make([]netip.Addr, 0, len(request.Addresses))
	for _, reported := range request.Addresses {
		address, err := netip.ParseAddr(reported.Address)
		if err != nil {
			return err
		}
		if reported.AcquiredAt.IsZero() || !slices.ContainsFunc(d.client.Lease.Addresses, func(lease pdclient.AddressLease) bool {
			return lease.Address == address && lease.AcquiredAt.Equal(reported.AcquiredAt)
		}) {
			return fmt.Errorf("Decline acquisition changed for %s", address)
		}
		addresses = append(addresses, address)
	}
	return d.client.Decline(ctx, addresses)
}
