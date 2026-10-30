// SPDX-License-Identifier: BSD-3-Clause

package vxlan

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
)

// SegmentConfig is the native link input shared by the VXLAN resource owners.
// Remotes are the complete unicast flood set; Group selects multicast instead.
type SegmentConfig struct {
	IfName     string   `json:"ifname"`
	VNI        int      `json:"vni"`
	Local      string   `json:"local"`
	Remotes    []string `json:"remotes"`
	Group      string   `json:"group,omitempty"`
	Underlay   string   `json:"underlay"`
	Port       int      `json:"port"`
	MTU        int      `json:"mtu"`
	Bridge     string   `json:"bridge,omitempty"`
	NoLearning bool     `json:"noLearning,omitempty"`
	OuterDF    string   `json:"outerDF,omitempty"`
}

// SegmentIdentity binds an operation to the actual kernel object and the
// topology observed before acquiring it. Index zero is allowed only while a
// creation is pending; the native cookie still has to match before recovery.
type SegmentIdentity struct {
	Scope         string `json:"scope"`
	Token         string `json:"token"`
	CreationName  string `json:"creationName"`
	Index         int    `json:"index"`
	UnderlayIndex int    `json:"underlayIndex"`
	BridgeIndex   int    `json:"bridgeIndex"`
	Pending       bool   `json:"pending"`
}

type SegmentLink struct {
	Exists bool
	Scope  string
	Index  int
	Token  string
	Kind   string
	Config SegmentConfig
	// Linux omits the source attribute when the driver selects it per packet.
	// Keep that positive native observation distinct from an unreadable source.
	LocalWildcard bool
	// The public port controls both receive and transmit ports.
	LocalPort int
	// These indexes are independent of the display names in Config.
	UnderlayIndex int
	BridgeIndex   int
	Up            bool
	Running       bool
	Flood         []string
}

// SegmentNative is the actual OS boundary. Both implementations check the
// supplied native identity immediately before mutating a link or its FDB.
type SegmentNative interface {
	KernelScope(context.Context) (string, error)
	Resolve(context.Context, SegmentConfig, string) (SegmentIdentity, error)
	Observe(context.Context, string) (SegmentLink, error)
	ObserveOwned(context.Context, SegmentConfig, SegmentIdentity) (SegmentLink, error)
	Create(context.Context, SegmentConfig, SegmentIdentity) error
	Configure(context.Context, SegmentConfig, SegmentIdentity, func(context.Context) error) error
	Remove(context.Context, SegmentConfig, SegmentIdentity) error
}

func SegmentProof(token string) string { return "routerd.vxlan:" + token }

// CheckSegmentIdentity is a prerequisite for mutation, never a substitute for
// native readback. An unfinished creation can contain only part of its input.
func CheckSegmentIdentity(config SegmentConfig, owner SegmentIdentity, link SegmentLink) error {
	named := link.Config.IfName == config.IfName || owner.Pending && link.Config.IfName == owner.CreationName
	if !link.Exists || link.Scope != owner.Scope || link.Token != owner.Token || link.Kind != "vxlan" || !named || link.Index <= 0 || owner.Index > 0 && owner.Index != link.Index {
		return fmt.Errorf("VXLAN %s has no matching native ownership", config.IfName)
	}
	if link.BridgeIndex != 0 && link.BridgeIndex != owner.BridgeIndex {
		return fmt.Errorf("VXLAN %s has a foreign bridge attachment", config.IfName)
	}
	// A removed underlay may leave the owned tunnel detached. It can still be
	// withdrawn by token/index, but cannot pass activation readback below.
	localMatches := link.Config.Local == config.Local
	if link.LocalWildcard {
		local, err := netip.ParseAddr(config.Local)
		localMatches = err == nil && local.IsUnspecified()
	}
	if !owner.Pending && (link.Config.VNI != config.VNI || !localMatches || link.Config.Group != config.Group || link.Config.Port != config.Port || link.LocalPort != config.Port || link.Config.NoLearning != config.NoLearning || config.OuterDF != "" && link.Config.OuterDF != config.OuterDF || link.UnderlayIndex != 0 && link.UnderlayIndex != owner.UnderlayIndex) {
		return fmt.Errorf("VXLAN %s native tunnel tuple changed", config.IfName)
	}
	return nil
}

func CheckSegmentReadback(config SegmentConfig, owner SegmentIdentity, link SegmentLink) error {
	owner.Pending = false
	if err := CheckSegmentIdentity(config, owner, link); err != nil {
		return err
	}
	if !link.Up || !link.Running || link.UnderlayIndex != owner.UnderlayIndex || link.BridgeIndex != owner.BridgeIndex || config.MTU > 0 && link.Config.MTU != config.MTU {
		return fmt.Errorf("VXLAN %s native activation readback differs", config.IfName)
	}
	want, actual := append([]string(nil), config.Remotes...), append([]string(nil), link.Flood...)
	slices.Sort(want)
	slices.Sort(actual)
	if !slices.Equal(want, actual) {
		return fmt.Errorf("VXLAN %s native flood destinations differ", config.IfName)
	}
	return nil
}
