// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

// Child effects consume proof from each concrete native owner. This reader has
// no journal, cache, effects, or authority derived from display status.
type nativeParentReader struct {
	Router    *api.Router
	Store     Store
	Segments  *VXLANSegmentController
	DSLite    *DSLiteTunnelController
	Bridges   *BridgeController
	WireGuard *WireGuardController
	VRFs      *VRFController
	Tunnels   *TunnelInterfaceController
}
type nativeParentLink struct {
	Exists, Declared     bool
	IfName, Scope, Token string
	Index                int
}

func validNativeParentToken(token string) bool {
	size := 16
	if raw, ok := strings.CutPrefix(token, "dslite:"); ok {
		token, size = raw, 24
	} else if raw, ok := strings.CutPrefix(token, "bridge:"); ok {
		token, size = raw, 24
	} else if raw, ok := strings.CutPrefix(token, "wireguard:"); ok {
		token = raw
	} else if raw, ok := strings.CutPrefix(token, "vrf:"); ok {
		token = raw
	} else if raw, ok := strings.CutPrefix(token, "tunnel:"); ok {
		token = raw
	}
	decoded, err := hex.DecodeString(token)
	return err == nil && len(decoded) == size
}

func (r nativeParentReader) segmentOwner() VXLANSegmentController {
	owner := VXLANSegmentController{}
	if r.Segments != nil {
		owner = *r.Segments
	}
	owner.Router, owner.Store = r.Router, r.Store
	return owner
}
func (r nativeParentReader) dsliteOwner() DSLiteTunnelController {
	owner := DSLiteTunnelController{}
	if r.DSLite != nil {
		owner = *r.DSLite
	}
	owner.Router, owner.DeclaredRouter, owner.Store = r.Router, r.Router, r.Store
	return owner
}
func (r nativeParentReader) bridgeOwner() BridgeController {
	owner := BridgeController{}
	if r.Bridges != nil {
		owner = *r.Bridges
	}
	owner.Router, owner.Store = r.Router, r.Store
	return owner
}
func (r nativeParentReader) wireGuardOwner() (WireGuardController, error) {
	owner := WireGuardController{}
	if r.WireGuard != nil {
		owner = *r.WireGuard
	}
	store, ok := r.Store.(wireGuardStore)
	if !ok {
		return owner, errors.New("WireGuard parent journal reader is unavailable")
	}
	owner.Router, owner.Store = r.Router, store
	return owner, nil
}
func (r nativeParentReader) vrfOwner() VRFController {
	owner := VRFController{}
	if r.VRFs != nil {
		owner = *r.VRFs
	}
	owner.Router, owner.Store = r.Router, r.Store
	return owner
}
func (r nativeParentReader) tunnelOwner() TunnelInterfaceController {
	owner := TunnelInterfaceController{}
	if r.Tunnels != nil {
		owner = *r.Tunnels
	}
	owner.Router, owner.Store = r.Router, r.Store
	return owner
}
func (r nativeParentReader) observeDeclared(ctx context.Context, ifname string) (nativeParentLink, error) {
	if r.Router == nil {
		return nativeParentLink{}, errors.New("child parent router is unavailable")
	}
	var selected api.Resource
	for _, resource := range r.Router.Spec.Resources {
		var name, configuredAlias string
		var err error
		switch resource.Kind {
		case "TunnelInterface":
			if r.tunnelOwner().targetOS() != platform.OSLinux {
				continue
			}
			name = resource.Metadata.Name
		case "VRF":
			if r.vrfOwner().currentOS() != platform.OSLinux {
				continue
			}
			var spec api.VRFSpec
			spec, err = resource.VRFSpec()
			name = firstNonEmpty(spec.IfName, resource.Metadata.Name)
		case "WireGuardInterface":
			if r.WireGuard != nil && r.WireGuard.currentOS() != platform.OSLinux || r.WireGuard == nil && platform.CurrentOS() != platform.OSLinux {
				continue
			}
			var spec api.WireGuardInterfaceSpec
			spec, err = resource.WireGuardInterfaceSpec()
			name = firstNonEmpty(spec.IfName, resource.Metadata.Name)
		case "Bridge":
			// Only Linux has a Bridge receipt owner here. FreeBSD's existing
			// boot backend remains outside this owner's qualification scope.
			if r.bridgeOwner().currentOS() != platform.OSLinux {
				continue
			}
			var spec api.BridgeSpec
			spec, err = resource.BridgeSpec()
			name = firstNonEmpty(spec.IfName, resource.Metadata.Name)
		case "VXLANSegment", "VXLANTunnel":
			name, err = nativeVXLANInterface(resource)
		case "DSLiteTunnel":
			var spec api.DSLiteTunnelSpec
			spec, err = resource.DSLiteTunnelSpec()
			owner := r.dsliteOwner()
			name = owner.nativeTunnelName(resource.Metadata.Name, spec)
			configuredAlias = firstNonEmpty(spec.TunnelName, spec.Interface, resource.Metadata.Name)
		default:
			continue
		}
		if err != nil {
			return nativeParentLink{}, err
		}
		if name != ifname && configuredAlias != ifname {
			continue
		}
		if selected.Kind != "" {
			return nativeParentLink{}, errors.New("multiple virtual parents declare the same native interface")
		}
		selected = resource
	}
	switch selected.Kind {
	case "TunnelInterface":
		owner := r.tunnelOwner()
		link, err := owner.observeChildParent(ctx, selected, "", "")
		link.Declared = true
		return link, err
	case "VRF":
		owner := r.vrfOwner()
		return owner.observeChildParent(ctx, selected, "", "")
	case "WireGuardInterface":
		owner, err := r.wireGuardOwner()
		if err != nil {
			return nativeParentLink{}, err
		}
		return owner.observeChildParent(ctx, selected, "", "")
	case "Bridge":
		owner := r.bridgeOwner()
		link, err := owner.observeChildParent(ctx, selected, "", "")
		link.Declared = true
		return link, err
	case "VXLANSegment", "VXLANTunnel":
		owner := r.segmentOwner()
		link, err := owner.ObserveOwnedInterface(ctx, selected.ID())
		return nativeParentLink{Declared: true, Exists: link.Exists, IfName: link.Config.IfName, Scope: link.Scope, Token: link.Token, Index: link.Index}, err
	case "DSLiteTunnel":
		owner := r.dsliteOwner()
		link, err := owner.observeChildParent(ctx, selected, "", "")
		link.Declared = true
		return link, err
	default:
		return nativeParentLink{}, nil
	}
}
func (r nativeParentReader) observeRecorded(ctx context.Context, ifname, token string) (nativeParentLink, error) {
	if !validNativeParentToken(token) {
		return nativeParentLink{}, errors.New("invalid recorded native parent token")
	}
	if raw, ok := strings.CutPrefix(token, "tunnel:"); ok {
		owner := r.tunnelOwner()
		return owner.observeChildParent(ctx, api.Resource{}, ifname, raw)
	}
	if raw, ok := strings.CutPrefix(token, "dslite:"); ok {
		owner := r.dsliteOwner()
		return owner.observeChildParent(ctx, api.Resource{}, ifname, raw)
	}
	if raw, ok := strings.CutPrefix(token, "bridge:"); ok {
		owner := r.bridgeOwner()
		return owner.observeChildParent(ctx, api.Resource{}, ifname, raw)
	}
	if raw, ok := strings.CutPrefix(token, "wireguard:"); ok {
		owner, err := r.wireGuardOwner()
		if err != nil {
			return nativeParentLink{}, err
		}
		return owner.observeChildParent(ctx, api.Resource{}, ifname, raw)
	}
	if raw, ok := strings.CutPrefix(token, "vrf:"); ok {
		owner := r.vrfOwner()
		return owner.observeChildParent(ctx, api.Resource{}, ifname, raw)
	}
	owner := r.segmentOwner()
	link, err := owner.ObserveRecordedInterface(ctx, ifname, token)
	return nativeParentLink{Declared: true, Exists: link.Exists, IfName: link.Config.IfName, Scope: link.Scope, Token: link.Token, Index: link.Index}, err
}
