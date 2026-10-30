// SPDX-License-Identifier: BSD-3-Clause

package bgpdaemon

import (
	"crypto/sha256"
	"encoding/binary"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	gobgpapi "github.com/osrg/gobgp/v4/api"
	"net/netip"
)

func PathFromAppliedPath(appliedPath AppliedPath) (*gobgpapi.Path, error) {
	appliedPath = NormalizeAppliedPath(appliedPath)
	if err := ValidateAppliedPath(appliedPath); err != nil {
		return nil, err
	}
	parsed, err := netip.ParsePrefix(appliedPath.Prefix)
	if err != nil {
		return nil, err
	}
	parsed = parsed.Masked()
	nlri := ipAddressNLRI(parsed)
	attrs := []*gobgpapi.Attribute{originAttribute()}
	nextHop := "0.0.0.0"
	if parsed.Addr().Is6() {
		nextHop = "::"
	}
	if appliedPath.Attrs.NextHop != "" {
		nextHop = appliedPath.Attrs.NextHop
	}
	attrs = append(attrs, nextHopAttribute(nextHop))
	if appliedPath.Attrs.LocalPref > 0 {
		attrs = append(attrs, localPrefAttribute(appliedPath.Attrs.LocalPref))
	}
	if appliedPath.Attrs.MED > 0 {
		attrs = append(attrs, medAttribute(appliedPath.Attrs.MED))
	}
	communities, err := bgpstate.StandardCommunities(appliedPath.Attrs.Communities)
	if err != nil {
		return nil, err
	}
	if len(communities) > 0 {
		attrs = append(attrs, communitiesAttribute(communities))
	}
	path := &gobgpapi.Path{Family: pathFamily(parsed), Nlri: nlri, Pattrs: attrs}
	if IsMobilityPathSource(appliedPath.Source) {
		path.Identifier = NativePathIdentifier(appliedPath.Source)
	}
	return path, nil
}

func ipAddressNLRI(prefix netip.Prefix) *gobgpapi.NLRI {
	return &gobgpapi.NLRI{Nlri: &gobgpapi.NLRI_Prefix{Prefix: &gobgpapi.IPAddressPrefix{
		Prefix: prefix.Addr().String(), PrefixLen: uint32(prefix.Bits()),
	}}}
}

func originAttribute() *gobgpapi.Attribute {
	return &gobgpapi.Attribute{Attr: &gobgpapi.Attribute_Origin{Origin: &gobgpapi.OriginAttribute{Origin: 0}}}
}

func nextHopAttribute(nextHop string) *gobgpapi.Attribute {
	return &gobgpapi.Attribute{Attr: &gobgpapi.Attribute_NextHop{NextHop: &gobgpapi.NextHopAttribute{NextHop: nextHop}}}
}

func localPrefAttribute(localPref uint32) *gobgpapi.Attribute {
	return &gobgpapi.Attribute{Attr: &gobgpapi.Attribute_LocalPref{LocalPref: &gobgpapi.LocalPrefAttribute{LocalPref: localPref}}}
}

func medAttribute(med uint32) *gobgpapi.Attribute {
	return &gobgpapi.Attribute{Attr: &gobgpapi.Attribute_MultiExitDisc{MultiExitDisc: &gobgpapi.MultiExitDiscAttribute{Med: med}}}
}

func communitiesAttribute(communities []uint32) *gobgpapi.Attribute {
	return &gobgpapi.Attribute{Attr: &gobgpapi.Attribute_Communities{Communities: &gobgpapi.CommunitiesAttribute{Communities: communities}}}
}

func NativePathIdentifier(source string) uint32 {
	if source == AppliedPathSourceStatic {
		return 0
	}
	digest := sha256.Sum256([]byte(source))
	id := binary.BigEndian.Uint32(digest[:4])
	if id == 0 {
		return 1
	}
	return id
}

func pathFamily(prefix netip.Prefix) *gobgpapi.Family {
	afi := gobgpapi.Family_AFI_IP
	if prefix.Addr().Is6() {
		afi = gobgpapi.Family_AFI_IP6
	}
	return &gobgpapi.Family{Afi: afi, Safi: gobgpapi.Family_SAFI_UNICAST}
}
