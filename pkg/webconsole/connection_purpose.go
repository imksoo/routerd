// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"net/netip"
	"strings"

	"github.com/imksoo/routerd/pkg/observe"
)

const (
	connectionPurposeSourceNAT      = "source-nat"
	connectionPurposeLocalRedirect  = "local-redirect"
	connectionPurposeDestinationNAT = "destination-nat"
	connectionPurposeExternal       = "external-without-nat"
	connectionPurposeInternal       = "internal"
	connectionPurposeUnclassified   = "unclassified"
)

func classifyConnectionPurposes(table *observe.ConnectionTable) {
	if table == nil {
		return
	}
	counts := &observe.ConnectionPurposeCounts{}
	for i := range table.Entries {
		entry := &table.Entries[i]
		if strings.ToLower(strings.TrimSpace(entry.Family)) != "ipv4" {
			continue
		}
		counts.IPv4++
		entry.Purpose = connectionPurpose(entry)
		switch entry.Purpose {
		case connectionPurposeSourceNAT:
			counts.SourceNAT++
		case connectionPurposeLocalRedirect:
			counts.LocalRedirect++
		case connectionPurposeDestinationNAT:
			counts.DestinationNAT++
		case connectionPurposeExternal:
			counts.ExternalWithoutNAT++
		case connectionPurposeInternal:
			counts.Internal++
		default:
			counts.Unclassified++
		}
	}
	table.ByPurpose = counts
}

func connectionPurpose(entry *observe.ConnectionEntry) string {
	if entry.Original.Source != "" && entry.Reply.Destination != "" && entry.Original.Source != entry.Reply.Destination {
		return connectionPurposeSourceNAT
	}
	if entry.LocalRedirect != nil {
		return connectionPurposeLocalRedirect
	}
	if entry.Original.Destination != "" && entry.Reply.Source != "" && entry.Original.Destination != entry.Reply.Source {
		return connectionPurposeDestinationNAT
	}
	destination, err := netip.ParseAddr(strings.TrimSpace(entry.Original.Destination))
	if err != nil || !destination.Is4() {
		return connectionPurposeUnclassified
	}
	if isInternalIPv4(destination) {
		return connectionPurposeInternal
	}
	return connectionPurposeExternal
}

func isInternalIPv4(address netip.Addr) bool {
	return address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() ||
		address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() ||
		address.String() == "255.255.255.255"
}
