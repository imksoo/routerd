// SPDX-License-Identifier: BSD-3-Clause

package ipsec

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/strongswan/govici/vici"
)

// saID is the kernel lookup identity reported by an owned VICI CHILD_SA.
// It is never inferred from an endpoint pair alone.
type saID struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Protocol    string `json:"protocol"`
	SPI         uint32 `json:"spi"`
	Mark        uint32 `json:"mark,omitempty"`
	Mask        uint32 `json:"mask,omitempty"`
	IfID        uint32 `json:"ifID,omitempty"`
}

// No traffic counter or key material is persisted. Fingerprint binds the exact
// immutable native configuration, including a digest of the native key bytes.
type saReceipt struct {
	ID          saID   `json:"id"`
	Scope       string `json:"scope"`
	ReqID       uint32 `json:"reqID"`
	AddTime     uint64 `json:"addTime"`
	Fingerprint string `json:"fingerprint"`
}

type nativeSAStore interface {
	Scope(context.Context) (string, error)
	ForEndpoints(context.Context, string, string) ([]saID, error)
	Observe(context.Context, saID) (saReceipt, bool, error)
	Delete(context.Context, saReceipt) error
}

func (id saID) valid() bool {
	source, err := netip.ParseAddr(id.Source)
	if err != nil || source.IsUnspecified() {
		return false
	}
	destination, err := netip.ParseAddr(id.Destination)
	return err == nil && !destination.IsUnspecified() && source.Is4() == destination.Is4() && id.SPI != 0 && (id.Protocol == "ESP" || id.Protocol == "AH")
}

// A provider name identifies the configured owner, not an independent peer
// pair. Moving an endpoint cannot silently extend native deletion authority.
func (id saID) matchesEndpoints(local, remote string) bool {
	a, err := netip.ParseAddr(local)
	if err != nil {
		return false
	}
	b, err := netip.ParseAddr(remote)
	if err != nil {
		return false
	}
	src, err := netip.ParseAddr(id.Source)
	if err != nil {
		return false
	}
	dst, err := netip.ParseAddr(id.Destination)
	if err != nil {
		return false
	}
	a, b, src, dst = a.Unmap(), b.Unmap(), src.Unmap(), dst.Unmap()
	return (src == a && dst == b) || (src == b && dst == a)
}

func (state saReceipt) valid() bool {
	decoded, err := hex.DecodeString(state.Fingerprint)
	return err == nil && len(decoded) == 32 && state.ID.valid() && state.Scope != "" && state.AddTime != 0
}

type reportedSA struct {
	ID    saID
	ReqID uint32
}

func connectionSAIDs(ctx context.Context, session *vici.Session, name string) ([]reportedSA, error) {
	request, err := vici.MarshalMessage(struct {
		IKE string `vici:"ike"`
	}{name})
	if err != nil {
		return nil, err
	}
	var result []reportedSA
	for message, err := range session.CallStreaming(ctx, "list-sas", "list-sa", request) {
		if err != nil {
			return nil, fmt.Errorf("IPsec list-sas: %w", err)
		}
		// These maps describe the provider's named sections only at the VICI boundary.
		type child struct {
			Protocol string `vici:"protocol"`
			ReqID    uint32 `vici:"reqid"`
			SPIIn    string `vici:"spi-in"`
			SPIOut   string `vici:"spi-out"`
			MarkIn   string `vici:"mark-in"`
			MarkOut  string `vici:"mark-out"`
			MaskIn   string `vici:"mark-mask-in"`
			MaskOut  string `vici:"mark-mask-out"`
			IfIn     string `vici:"if-id-in"`
			IfOut    string `vici:"if-id-out"`
		}
		type ike struct {
			Local    string           `vici:"local-host"`
			Remote   string           `vici:"remote-host"`
			Children map[string]child `vici:"child-sas"`
		}
		entries := map[string]ike{}
		if err := vici.UnmarshalMessage(message, entries); err != nil {
			return nil, fmt.Errorf("decode IPsec SAs: %w", err)
		}
		for key, entry := range entries {
			if key != name {
				return nil, fmt.Errorf("IPsec SA observation returned unrelated connection %s", key)
			}
			for _, child := range entry.Children {
				if child.SPIIn == "" && child.SPIOut == "" {
					continue
				} // IKE/CHILD negotiation has not installed an SA.
				for _, direction := range []struct{ source, destination, spi, mark, mask, ifid string }{
					{entry.Remote, entry.Local, child.SPIIn, child.MarkIn, child.MaskIn, child.IfIn},
					{entry.Local, entry.Remote, child.SPIOut, child.MarkOut, child.MaskOut, child.IfOut},
				} {
					parse := func(value string) (uint32, error) {
						if value == "" {
							return 0, nil
						}
						n, err := strconv.ParseUint(value, 16, 32)
						return uint32(n), err
					}
					spi, err := parse(direction.spi)
					if err != nil {
						return nil, err
					}
					mark, err := parse(direction.mark)
					if err != nil {
						return nil, err
					}
					mask, err := parse(direction.mask)
					if err != nil {
						return nil, err
					}
					if direction.mark != "" && direction.mask == "" {
						mask = ^uint32(0)
					}
					ifid, err := parse(direction.ifid)
					if err != nil {
						return nil, err
					}
					id := saID{Source: direction.source, Destination: direction.destination, Protocol: strings.ToUpper(child.Protocol), SPI: spi, Mark: mark, Mask: mask, IfID: ifid}
					if !id.valid() {
						return nil, errors.New("IPsec provider SA identity is incomplete")
					}
					result = append(result, reportedSA{ID: id, ReqID: child.ReqID})
				}
			}
		}
	}
	return result, nil
}

func (c Controller) kernelSAs() nativeSAStore {
	if c.nativeSAs != nil {
		return c.nativeSAs
	}
	return defaultNativeSAs()
}

// Refresh only already journaled connections. A same-endpoint kernel SA is not
// adopted: the provider must report its exact SPI as belonging to this name.
func (c Controller) observeOwnedSAs(ctx context.Context, state receipt) (receipt, error) {
	native := c.kernelSAs()
	if native == nil {
		for _, conn := range state.Connections {
			if len(conn.SAs) != 0 {
				return state, errors.New("recorded native IPsec SAs require their original kernel boundary")
			}
		}
		return state, nil
	}
	state.Connections = slices.Clone(state.Connections)
	for i := range state.Connections {
		conn := &state.Connections[i]
		kept := []saReceipt{}
		scope, err := native.Scope(ctx)
		if err != nil {
			return state, err
		}
		if conn.Scope == "" || conn.Scope != scope {
			return state, errors.New("recorded IPsec connection has no matching kernel scope")
		}
		if !conn.Acknowledged {
			continue
		} // A name from an intent is not acquisition proof.
		if len(conn.SAs) != 0 {
			for _, old := range conn.SAs {
				if old.Scope != scope {
					return state, errors.New("recorded IPsec SA belongs to a different kernel scope")
				}
				current, found, err := native.Observe(ctx, old.ID)
				if err != nil {
					return state, err
				}
				if found {
					if current != old {
						return state, errors.New("recorded IPsec SA was replaced")
					}
					kept = append(kept, old)
				}
			}
		}
		reported, err := connectionSAIDs(ctx, c.Session, conn.Name)
		if err != nil {
			return state, err
		}
		for _, sa := range reported {
			if !sa.ID.matchesEndpoints(conn.Local, conn.Remote) {
				return state, errors.New("IPsec provider SA endpoints do not match the recorded connection; native ownership is unknown")
			}
			current, found, err := native.Observe(ctx, sa.ID)
			if err != nil {
				return state, err
			}
			if !found {
				continue
			} // A normal rekey/expiry may retire the sampled SPI.
			if !current.valid() || current.Scope != conn.Scope || current.ReqID != sa.ReqID {
				return state, errors.New("IPsec VICI and kernel SA identities do not match")
			}
			if !slices.Contains(kept, current) {
				kept = append(kept, current)
			}
		}
		if len(reported) != 0 {
			confirmed, err := connectionSAIDs(ctx, c.Session, conn.Name)
			if err != nil {
				return state, err
			}
			for _, sa := range reported {
				if !slices.Contains(confirmed, sa) {
					return state, errors.New("IPsec SA ownership changed during native observation")
				}
			}
		}
		after, err := native.Scope(ctx)
		if err != nil {
			return state, err
		}
		if after != conn.Scope {
			return state, errors.New("IPsec kernel scope changed during VICI/native observation")
		}
		conn.SAs = kept
	}
	return state, nil
}

func (c Controller) withdrawRecordedSAs(ctx context.Context, conn connectionReceipt, connections []connectionReceipt) error {
	native := c.kernelSAs()
	if native == nil {
		if len(conn.SAs) != 0 || conn.Scope != "" {
			return errors.New("native IPsec SA withdrawal is unsupported")
		}
		return nil
	}
	scope, err := native.Scope(ctx)
	if err != nil {
		return err
	}
	if conn.Scope == "" || conn.Scope != scope {
		return errors.New("recorded IPsec connection has no matching kernel scope")
	}
	for _, state := range conn.SAs {
		if state.Scope != scope {
			return errors.New("recorded IPsec SA belongs to a different kernel scope")
		}
		if err := native.Delete(ctx, state); err != nil {
			return err
		}
		current, found, err := native.Observe(ctx, state.ID)
		if err != nil {
			return err
		}
		if found {
			return fmt.Errorf("recorded IPsec SA remains after deletion: %s SPI %x", current.ID.Protocol, current.ID.SPI)
		}
	}
	// Endpoint equality detects uncertainty only. It never grants deletion authority.
	remaining, err := native.ForEndpoints(ctx, conn.Local, conn.Remote)
	if err != nil {
		return err
	}
	after, err := native.Scope(ctx)
	if err != nil {
		return err
	}
	if after != conn.Scope {
		return errors.New("IPsec kernel scope changed during withdrawal inventory")
	}
	for _, id := range remaining {
		knownOther := false
		for _, other := range connections {
			if other.Name == conn.Name {
				continue
			}
			for _, recorded := range other.SAs {
				if recorded.ID != id {
					continue
				}
				current, found, err := native.Observe(ctx, id)
				if err != nil {
					return err
				}
				if found && current != recorded {
					return errors.New("another recorded IPsec SA was replaced during withdrawal")
				}
				knownOther = true
			}
		}
		if !knownOther {
			return fmt.Errorf("IPsec kernel SAs remain without current ownership proof; retain pending receipt (%s SPI %x)", id.Protocol, id.SPI)
		}
	}
	return nil
}
