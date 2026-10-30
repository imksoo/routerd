// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"slices"

	"github.com/imksoo/routerd/pkg/api"
)

func tunnelNativeApplied(record tunnelNativeReceipt, link tunnelNativeLink) bool {
	return link.Exists && link.Observed.Up && tunnelLinkMatches(link.Observed, record.Plan) && (record.Plan.MTU == 0 || link.Observed.MTU == record.Plan.MTU) && (record.Plan.Address == "" || slices.Contains(link.Addresses, record.Plan.Address))
}

// Current admission reads current intent and sources. Recorded cleanup uses only
// the exact old receipt/native identity, even after declaration/source removal.
func (c TunnelInterfaceController) observeChildParent(ctx context.Context, resource api.Resource, name, token string) (nativeParentLink, error) {
	active := resource.Kind != ""
	var desired tunnelDesired
	var err error
	if active {
		var pending bool
		desired, pending, _, err = c.currentTunnelDesired(resource.Metadata.Name)
		if err != nil {
			return nativeParentLink{}, err
		}
		if pending {
			return nativeParentLink{}, errors.New("TunnelInterface parent source is not current")
		}
		name = desired.Name
	}
	scope, err := c.tunnelKernelScope(ctx)
	if err != nil {
		return nativeParentLink{}, err
	}
	absent := nativeParentLink{Scope: scope}
	records, err := c.tunnelNativeReceipts()
	if err != nil {
		return absent, err
	}
	var record tunnelNativeReceipt
	for _, candidate := range records {
		if candidate.Plan.Name == name && (active && candidate.Resource == resource.Metadata.Name || !active && candidate.Token == token) {
			record = candidate
			break
		}
	}
	if record.Resource != "" && active && (record.Pending || record.Withdrawing || record.Index < 1 || record.Plan != desired) {
		return absent, errors.New("TunnelInterface parent is pending or differs from current intent")
	}
	link, err := c.observeOwnedTunnel(ctx, name)
	if err != nil {
		return absent, err
	}
	if record.Resource == "" {
		if link.Scope != scope || link.Exists {
			return absent, errors.New("TunnelInterface parent is present without its native receipt")
		}
		return absent, nil
	}
	if err := verifyTunnelNative(record, link); err != nil {
		return absent, err
	}
	if active {
		if !tunnelNativeApplied(record, link) {
			return absent, errors.New("TunnelInterface parent native configuration differs from applied intent")
		}
		if listener, uses := fouListenerForDesired(record.Plan); uses {
			present, matching, err := c.observeTunnelNativeListener(ctx, listener)
			if err != nil || !present || !matching {
				return absent, errors.Join(err, errors.New("TunnelInterface listener is not current"))
			}
		}
		fresh, pending, _, err := c.currentTunnelDesired(resource.Metadata.Name)
		if err != nil || pending || fresh != desired {
			return absent, errors.Join(err, errors.New("TunnelInterface source changed while observing parent"))
		}
	}
	// Source/listener observations may wait. Re-read the actual native object,
	// then the receipt, before returning proof to a child effect owner.
	link, err = c.checkedOwnedTunnel(ctx, record)
	if err != nil {
		return absent, err
	}
	if active {
		fresh, pending, _, err := c.currentTunnelDesired(resource.Metadata.Name)
		if err != nil || pending || fresh != desired || !tunnelNativeApplied(record, link) {
			return absent, errors.Join(err, errors.New("TunnelInterface current parent proof changed after native observation"))
		}
	}
	if err := c.checkTunnelNativeReceipt(ctx, record); err != nil {
		return absent, err
	}
	if !link.Exists {
		return absent, nil
	}
	return nativeParentLink{Exists: true, IfName: name, Scope: link.Scope, Index: link.Index, Token: "tunnel:" + record.Token}, nil
}
