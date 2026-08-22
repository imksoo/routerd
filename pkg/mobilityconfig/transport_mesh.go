// SPDX-License-Identifier: BSD-3-Clause

package mobilityconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

// DefaultBGPImportLocalPreference is the local preference GoBGP assigns when
// an accepted path has no explicit LocalPref action. Keep direct-mesh
// validation relative to this effective value rather than treating zero as a
// lower preference.
const DefaultBGPImportLocalPreference uint32 = 100

// DefaultSAMTransportDirectLocalPreference makes an established direct leaf
// path preferred over the normal RR import path by default.
const DefaultSAMTransportDirectLocalPreference uint32 = 200

// SAMTransportDirectPeerAnnotation marks the generated BGP peer for an
// optional direct SAM adjacency. It is consumed only at the controller/effect
// boundary; users do not author it in a transport profile.
const SAMTransportDirectPeerAnnotation = "mobility.routerd.net/direct-peer"

// SAMTransportDirectPeerRejectRoutesAnnotation keeps a direct BGP session for
// a signed leaf that currently owns no mobility /32 while making its import
// policy reject every route. This lets pair-stable transport converge before
// ownership appears without creating a broad higher-preference route path.
const SAMTransportDirectPeerRejectRoutesAnnotation = "mobility.routerd.net/direct-peer-reject-routes"

func EffectiveBGPImportLocalPreference(preference uint32) uint32 {
	if preference == 0 {
		return DefaultBGPImportLocalPreference
	}
	return preference
}

func EffectiveSAMTransportDirectLocalPreference(preference uint32) uint32 {
	if preference == 0 {
		return DefaultSAMTransportDirectLocalPreference
	}
	return preference
}

// SAMTransportHasDirectPeerSource reports whether a transport profile has the
// optional direct leaf-mesh source. Direct profiles always retain an RR
// fallback, so their reflected routes must use the reachable adjacent RR as
// the effective next hop.
func SAMTransportHasDirectPeerSource(sources []api.SAMTransportPeersSourceSpec) bool {
	for _, source := range sources {
		if source.Direct {
			return true
		}
	}
	return false
}

// SAMTransportMeshFingerprint identifies the transport properties that both
// ends of a direct SAM adjacency must agree on. It deliberately excludes local
// interface names, endpoints, BGP router references, and import/export policy:
// those values are local concerns and may legitimately differ between leaves.
func SAMTransportMeshFingerprint(spec api.SAMTransportProfileSpec) string {
	shape := struct {
		Mode           string `json:"mode"`
		Encryption     string `json:"encryption"`
		InnerPrefix    string `json:"innerPrefix"`
		AddressingMode string `json:"addressingMode"`
		EncapSport     int    `json:"encapSport"`
		EncapDport     int    `json:"encapDport"`
		PeerASN        uint32 `json:"peerASN"`
		EbgpMultihop   int    `json:"ebgpMultihop"`
	}{
		Mode:           strings.TrimSpace(spec.Mode),
		Encryption:     normalizedSAMTransportMeshEncryption(spec.Encryption),
		InnerPrefix:    canonicalSAMTransportMeshInnerPrefix(spec.InnerPrefix),
		AddressingMode: NormalizeSAMTransportAddressingMode(spec.AddressingMode),
		EncapSport:     spec.EncapSport,
		EncapDport:     spec.EncapDport,
		PeerASN:        spec.BGP.PeerASN,
		EbgpMultihop:   spec.BGP.EbgpMultihop,
	}
	data, err := json.Marshal(shape)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizedSAMTransportMeshEncryption(value string) string {
	if value = strings.TrimSpace(value); value == "" {
		return "none"
	}
	return value
}

// canonicalSAMTransportMeshInnerPrefix mirrors transport derivation, which
// masks a syntactically valid CIDR before assigning pair-stable slots. Keeping
// the mesh fingerprint on that same semantic value lets equivalent authored
// forms such as 10.255.1.0/24 and 10.255.1.5/24 join the same direct mesh.
// Validation reports malformed values separately; retain their trimmed form
// here so the fingerprint helper remains total for diagnostic callers.
func canonicalSAMTransportMeshInnerPrefix(value string) string {
	value = strings.TrimSpace(value)
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return value
	}
	return prefix.Masked().String()
}
