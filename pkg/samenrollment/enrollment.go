// SPDX-License-Identifier: BSD-3-Clause

package samenrollment

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func JoinHMAC(secret []byte, claim api.SAMEnrollmentClaimSpec) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(JoinCanonicalPayload(claim)))
	return hex.EncodeToString(mac.Sum(nil))
}

// ClientIdentityDigest identifies exactly client-authored claim material. A
// join-token policy authenticates this material with JoinHMAC; a token-less
// policy is a trusted control-plane boundary instead. The digest deliberately
// excludes RR-owned admission fields such as expiresAt and revoked, so it is
// suitable for replay protection and revocation lookups before an RR has
// admitted the current claim.
func ClientIdentityDigest(claim api.SAMEnrollmentClaimSpec) string {
	payload := JoinCanonicalPayload(claim) + "\njoinHMAC=" + strings.TrimSpace(claim.JoinHMAC)
	sum := sha256.Sum256([]byte(payload))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ClaimDigest is the stable identity of an enrollment claim after admission.
//
// Unlike JoinHMAC, this includes the submitted signature and the
// RR/controller-owned expiry and revocation fields.  Clients use it when
// asking an RR for a topology snapshot, so an RR can prove that the snapshot
// was projected from this exact accepted claim rather than an older claim with
// the same resource name.
func ClaimDigest(claim api.SAMEnrollmentClaimSpec) string {
	payload := strings.Join([]string{
		JoinCanonicalPayload(claim),
		"joinHMAC=" + strings.TrimSpace(claim.JoinHMAC),
		"expiresAt=" + strings.TrimSpace(claim.ExpiresAt),
		"revoked=" + strconv.FormatBool(claim.Revoked),
	}, "\n")
	sum := sha256.Sum256([]byte(payload))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func JoinCanonicalPayload(claim api.SAMEnrollmentClaimSpec) string {
	owned := append([]string(nil), claim.Mobility.OwnedAddresses...)
	sort.Strings(owned)
	wgAllowed := append([]string(nil), claim.WireGuard.AllowedIPs...)
	sort.Strings(wgAllowed)
	fields := []string{
		"policyRef=" + strings.TrimSpace(claim.PolicyRef),
		"rrSetRef=" + strings.TrimSpace(claim.RRSetRef),
		"leafID=" + strings.TrimSpace(claim.LeafID),
		"joinAudience=" + strings.TrimSpace(claim.JoinAudience),
		"joinNonce=" + strings.TrimSpace(claim.JoinNonce),
		"joinTimestamp=" + strings.TrimSpace(claim.JoinTimestamp),
		"tunnelAddress=" + strings.TrimSpace(claim.TunnelAddress),
		"endpoint=" + strings.TrimSpace(claim.Endpoint),
		"mobility.ownedAddresses=" + strings.Join(owned, ","),
		"bgp.asn=" + strconv.FormatUint(uint64(claim.BGP.ASN), 10),
		"bgp.routerID=" + strings.TrimSpace(claim.BGP.RouterID),
		"wireGuard.publicKey=" + strings.TrimSpace(claim.WireGuard.PublicKey),
		"wireGuard.endpoint=" + strings.TrimSpace(claim.WireGuard.Endpoint),
		"wireGuard.allowedIPs=" + strings.Join(wgAllowed, ","),
		"wireGuard.persistentKeepalive=" + strconv.Itoa(claim.WireGuard.PersistentKeepalive),
	}
	// Keep ordinary (non-direct) claims byte-for-byte compatible with the
	// canonical payload used before direct mesh existed. Direct opt-in changes
	// authorization semantics, so only the affirmative form is signed as an
	// additional field.
	if claim.DirectMesh {
		directFields := make([]string, 0, len(fields)+1)
		directFields = append(directFields, fields[:8]...)
		directFields = append(directFields, "directMesh=true")
		fields = append(directFields, fields[8:]...)
	}
	return strings.Join(fields, "\n")
}

// ClaimExpired applies the fail-closed enrollment lifetime rule shared by
// transport and local WireGuard derivation. Invalid configured lifetime or
// claim timestamps are expired rather than admitted.
func ClaimExpired(policy api.SAMEnrollmentPolicySpec, claim api.SAMEnrollmentClaimSpec, now time.Time) bool {
	if strings.TrimSpace(policy.TTL) != "" && strings.TrimSpace(claim.JoinTimestamp) != "" {
		ttl, ttlErr := time.ParseDuration(strings.TrimSpace(policy.TTL))
		joinedAt, timeErr := ParseTime(claim.JoinTimestamp)
		if ttlErr != nil || timeErr != nil || !joinedAt.Add(ttl).After(now) {
			return true
		}
	}
	if strings.TrimSpace(claim.ExpiresAt) == "" {
		return false
	}
	expiresAt, err := ParseTime(claim.ExpiresAt)
	if err != nil {
		return true
	}
	return !expiresAt.After(now)
}

// ParseTime accepts the two RFC3339 forms accepted by enrollment claims.
func ParseTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	return time.Parse(time.RFC3339, value)
}

// ParsePrefixOrAddress turns an enrollment address into a canonical host
// prefix when the value is not already a prefix.
func ParsePrefixOrAddress(value string) (netip.Prefix, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return netip.Prefix{}, err
		}
		return prefix.Masked(), nil
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Prefix{}, err
	}
	bits := 128
	if addr.Is4() {
		bits = 32
	}
	return netip.PrefixFrom(addr, bits), nil
}

// PrefixContains reports whether an enrollment address belongs to any
// configured prefix. Invalid configured prefixes never authorize an address.
func PrefixContains(prefixes []string, addr netip.Addr) bool {
	for _, value := range prefixes {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err == nil && prefix.Masked().Contains(addr) {
			return true
		}
	}
	return false
}

// ActiveClaims filters enrollment claims for one policy once, so transport and
// WireGuard topology derivation cannot drift on expiry or authorization rules.
// It keeps the original router resource order for accepted claims and sorts
// only the human-facing skip diagnostics.
type ActiveClaim struct {
	ResourceName string
	Claim        api.SAMEnrollmentClaimSpec
	Tunnel       netip.Prefix
}

type ActiveClaimSelection struct {
	Claims         []ActiveClaim
	Skipped        int
	SkippedReasons []string
}

// ActiveClaimNodeSetOptions keeps the two enrollment consumers' endpoint
// conventions explicit while sharing their active-claim topology projection.
type ActiveClaimNodeSetOptions struct {
	UseClaimEndpoint bool
	IncludeWireGuard bool
}

// ActiveClaimNodeSet projects already-authorized claims into topology nodes.
// Callers retain claim selection, status accounting, and interface policy.
func ActiveClaimNodeSet(selection ActiveClaimSelection, policy api.SAMEnrollmentPolicySpec, options ActiveClaimNodeSetOptions) (api.SAMNodeSetSpec, []string) {
	var nodes []api.SAMNodeSpec
	var leafIDs []string
	for _, active := range selection.Claims {
		claim := active.Claim
		endpoint := active.Tunnel.Addr().String()
		if options.UseClaimEndpoint && strings.TrimSpace(claim.Endpoint) != "" {
			endpoint = strings.TrimSpace(claim.Endpoint)
		}
		node := api.SAMNodeSpec{NodeRef: strings.TrimSpace(claim.LeafID), Role: "cloud", SAMEndpoint: endpoint}
		if options.IncludeWireGuard && strings.TrimSpace(claim.WireGuard.PublicKey) != "" {
			keepalive := claim.WireGuard.PersistentKeepalive
			if keepalive == 0 {
				keepalive = policy.WireGuard.PersistentKeepalive
			}
			node.WireGuard = api.SAMNodeWireGuardSpec{PublicKey: strings.TrimSpace(claim.WireGuard.PublicKey), Endpoint: strings.TrimSpace(claim.WireGuard.Endpoint), AllowedIPs: append([]string{active.Tunnel.String()}, claim.WireGuard.AllowedIPs...), PersistentKeepalive: keepalive}
		}
		nodes = append(nodes, node)
		leafIDs = append(leafIDs, strings.TrimSpace(claim.LeafID))
	}
	sort.Strings(leafIDs)
	return api.SAMNodeSetSpec{Nodes: nodes}, leafIDs
}

// DirectMeshTopology is the runtime projection of admitted leaf claims
// used by an opportunistic direct peer group. Nodes carry connectivity material;
// OwnedPrefixesByNode carries the equally important BGP admission boundary.
// Keeping them in one projection prevents a direct session from becoming a
// policy-wide /32 shortcut merely because its transport endpoint is reachable.
type DirectMeshTopology struct {
	Nodes               api.SAMNodeSetSpec
	OwnedPrefixesByNode map[string][]string
}

// ActiveDirectMeshTopology projects the eligible remote leaves for one
// enrollment client. The RR remains outside this topology: it is already
// supplied through the accompanying SAMRRSet and is the fallback path when a
// direct peer is unavailable. Only admitted claims that opted into direct mesh
// are included. A remote leaf with no currently owned IPv4 /32 still needs a
// pair-stable direct transport session so it can be ready before ownership
// appears; its absent ownership entry later compiles to a deny-all route
// boundary rather than a broad import allowlist.
func ActiveDirectMeshTopology(selection ActiveClaimSelection, policy api.SAMEnrollmentPolicySpec, selfLeafID string, includeWireGuard bool) (DirectMeshTopology, []string) {
	selfLeafID = strings.TrimSpace(selfLeafID)
	filtered := ActiveClaimSelection{}
	ownedPrefixesByNode := map[string][]string{}
	for _, active := range selection.Claims {
		if !active.Claim.DirectMesh || strings.TrimSpace(active.Claim.LeafID) == selfLeafID {
			continue
		}
		// A WireGuard direct transport needs a complete peer identity. Do not
		// emit a partial node that validates neither as WireGuard material nor
		// as an established encrypted underlay; its RR fallback remains intact.
		if includeWireGuard && strings.TrimSpace(active.Claim.WireGuard.PublicKey) == "" {
			continue
		}
		owned := directMeshOwnedPrefixes(active.Claim.Mobility.OwnedAddresses)
		filtered.Claims = append(filtered.Claims, active)
		if len(owned) > 0 {
			ownedPrefixesByNode[strings.TrimSpace(active.Claim.LeafID)] = owned
		}
	}
	nodes, leafIDs := ActiveClaimNodeSet(filtered, policy, ActiveClaimNodeSetOptions{
		UseClaimEndpoint: true,
		IncludeWireGuard: includeWireGuard,
	})
	return DirectMeshTopology{Nodes: nodes, OwnedPrefixesByNode: ownedPrefixesByNode}, leafIDs
}

func directMeshOwnedPrefixes(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		prefix, err := ParsePrefixOrAddress(value)
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 {
			continue
		}
		key := prefix.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func ActiveClaims(resources []api.Resource, policyRef string, policy api.SAMEnrollmentPolicySpec, now time.Time) (ActiveClaimSelection, error) {
	_, policyName, _ := strings.Cut(strings.TrimSpace(policyRef), "/")
	selection := ActiveClaimSelection{}
	for _, resource := range resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentClaim" {
			continue
		}
		claim, err := resource.SAMEnrollmentClaimSpec()
		if err != nil {
			return selection, err
		}
		_, claimPolicyName, _ := strings.Cut(strings.TrimSpace(claim.PolicyRef), "/")
		if claimPolicyName != strings.TrimSpace(policyName) {
			continue
		}
		name := strings.TrimSpace(resource.Metadata.Name)
		if claim.Revoked {
			selection.Skipped++
			selection.SkippedReasons = append(selection.SkippedReasons, name+": revoked")
			continue
		}
		if ClaimExpired(policy, claim, now) {
			selection.Skipped++
			selection.SkippedReasons = append(selection.SkippedReasons, name+": expired")
			continue
		}
		tunnel, err := ParsePrefixOrAddress(claim.TunnelAddress)
		if err != nil || !PrefixContains(policy.TunnelAddressPrefixes, tunnel.Addr()) {
			selection.Skipped++
			selection.SkippedReasons = append(selection.SkippedReasons, name+": unauthorized tunnel address")
			continue
		}
		selection.Claims = append(selection.Claims, ActiveClaim{ResourceName: name, Claim: claim, Tunnel: tunnel})
	}
	sort.Strings(selection.SkippedReasons)
	return selection, nil
}
