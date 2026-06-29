// SPDX-License-Identifier: BSD-3-Clause

package samenrollment

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

func JoinHMAC(secret []byte, claim api.SAMEnrollmentClaimSpec) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(JoinCanonicalPayload(claim)))
	return hex.EncodeToString(mac.Sum(nil))
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
	return strings.Join(fields, "\n")
}
