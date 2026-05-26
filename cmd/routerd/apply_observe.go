// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/pdclient"
	routerstate "routerd/pkg/state"
)

type stateChange struct {
	Name  string
	Value routerstate.Value
}

func recordObservedPrefixDelegationState(router *api.Router, store routerstate.Store) ([]stateChange, error) {
	aliases := map[string]string{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "Interface" {
			continue
		}
		spec, err := res.InterfaceSpec()
		if err != nil {
			return nil, err
		}
		aliases[res.Metadata.Name] = spec.IfName
	}
	delegatedByPD := map[string][]api.Resource{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv6DelegatedAddress" {
			continue
		}
		spec, err := res.IPv6DelegatedAddressSpec()
		if err != nil {
			return nil, err
		}
		delegatedByPD[spec.PrefixDelegation] = append(delegatedByPD[spec.PrefixDelegation], res)
	}

	var changes []stateChange
	for _, res := range router.Spec.Resources {
		if res.Kind != "DHCPv6PrefixDelegation" {
			continue
		}
		spec, err := res.DHCPv6PrefixDelegationSpec()
		if err != nil {
			return nil, err
		}
		profile := defaultString(spec.Profile, api.IPv6PDProfileDefault)
		client := defaultString(spec.Client, "networkd")
		prefixLength := api.EffectiveIPv6PDPrefixLength(profile, spec.PrefixLength)
		base := "ipv6PrefixDelegation." + res.Metadata.Name
		lease, _ := routerstate.PDLeaseFromStore(store, base)
		if snapshot, ok := managedPDClientSnapshot(res.Metadata.Name); ok {
			lease = mergePDLeaseSnapshot(lease, snapshot)
		}
		previousClient := store.Get(base + ".client").Value
		if previousClient != "" && previousClient != client {
			var cleared bool
			lease, cleared = routerstate.ClearPDLeaseObservedIdentity(lease)
			if cleared {
				if recorder, ok := store.(routerstate.EventRecorder); ok {
					_ = recorder.RecordEvent(res.APIVersion, res.Kind, res.Metadata.Name, "Normal", "PDClientChanged", fmt.Sprintf("cleared observed DHCPv6 identity after client changed from %s to %s", previousClient, client))
				}
			}
		}
		if ifname := aliases[spec.Interface]; ifname != "" {
			changes = append(changes, stateChange{Name: base + ".uplinkIfname", Value: store.Set(base+".uplinkIfname", ifname, res.ID()+": observed uplink interface")})
			identity := observedPrefixDelegationIdentity(ifname, client, spec.IAID)
			if identity.IAID != "" {
				lease.IAID = identity.IAID
			}
			if identity.DUID != "" {
				lease.DUID = identity.DUID
			}
			if identity.DUIDText != "" {
				lease.DUIDText = identity.DUIDText
			}
			if expected := expectedPrefixDelegationDUID(ifname, profile); expected != "" {
				lease.ExpectedDUID = expected
			}
		}
		changes = append(changes, stateChange{Name: base + ".client", Value: store.Set(base+".client", client, res.ID()+": configured DHCPv6-PD client")})
		changes = append(changes, stateChange{Name: base + ".profile", Value: store.Set(base+".profile", profile, res.ID()+": configured DHCPv6-PD profile")})
		if prefixLength > 0 {
			changes = append(changes, stateChange{Name: base + ".prefixLength", Value: store.Set(base+".prefixLength", strconv.Itoa(prefixLength), res.ID()+": configured prefix length")})
		}

		var observedPrefix, observedIfname string
		for _, delegated := range delegatedByPD[res.Metadata.Name] {
			delegatedSpec, err := delegated.IPv6DelegatedAddressSpec()
			if err != nil {
				return nil, err
			}
			ifname := aliases[delegatedSpec.Interface]
			if ifname == "" {
				continue
			}
			prefix, ok := delegatedPrefixFromObservedInterface(ifname, prefixLength, delegatedAddressSuffixes(delegatedByPD[res.Metadata.Name]))
			if ok {
				observedPrefix = prefix
				observedIfname = ifname
				break
			}
		}
		if observedPrefix == "" {
			if client == "dhcpcd" {
				if ifname := aliases[spec.Interface]; ifname != "" {
					if prefix, leaseUpdate, ok := observedDHCPCDDelegatedPrefix(ifname, prefixLength); ok {
						observedPrefix = prefix
						lease.T1 = firstNonEmptyString(leaseUpdate.T1, lease.T1)
						lease.T2 = firstNonEmptyString(leaseUpdate.T2, lease.T2)
						lease.PLTime = firstNonEmptyString(leaseUpdate.PLTime, lease.PLTime)
						lease.VLTime = firstNonEmptyString(leaseUpdate.VLTime, lease.VLTime)
					}
				}
			}
		}
		if observedPrefix == "" && lease.CurrentPrefix != "" && lease.HasFreshTransactionEvidence(store.Now()) {
			observedPrefix = lease.CurrentPrefix
		}
		if observedPrefix == "" {
			if recorder, ok := store.(routerstate.EventRecorder); ok {
				_ = recorder.RecordEvent(res.APIVersion, res.Kind, res.Metadata.Name, "Warning", "PrefixMissing", "delegated IPv6 prefix is not observable")
			}
			lease.CurrentPrefix = ""
			changes = append(changes, stateChange{Name: base + ".lease", Value: store.Set(base+".lease", routerstate.EncodePDLease(lease), res.ID()+": no delegated prefix observable")})
			continue
		}
		observedAt := store.Now().Format(time.RFC3339)
		previousPrefix := lease.LastPrefix
		// Stale-detection: a local prefix is observable but no DHCPv6 Reply
		// evidence backs it. Treat as not-observable so dnsmasq, RA, and the
		// LAN delegated-address rendering all stop advertising broken IPv6
		// to downstream clients. The local LastPrefix history is preserved.
		if !lease.HasFreshTransactionEvidence(store.Now()) {
			if recorder, ok := store.(routerstate.EventRecorder); ok {
				_ = recorder.RecordEvent(res.APIVersion, res.Kind, res.Metadata.Name, "Warning", "PrefixStale", "delegated IPv6 prefix "+observedPrefix+" lacks recent DHCPv6 Reply / valid lifetime; not advertising on LAN")
			}
			lease.CurrentPrefix = ""
			lease.LastPrefix = observedPrefix
			lease.LastObservedAt = observedAt
			changes = append(changes, stateChange{Name: base + ".lease", Value: store.Set(base+".lease", routerstate.EncodePDLease(lease), res.ID()+": stale delegated prefix without transaction evidence")})
			continue
		}
		lease.CurrentPrefix = observedPrefix
		lease.LastPrefix = observedPrefix
		lease.LastObservedAt = observedAt
		if recorder, ok := store.(routerstate.EventRecorder); ok && previousPrefix != observedPrefix {
			_ = recorder.RecordEvent(res.APIVersion, res.Kind, res.Metadata.Name, "Normal", "PrefixObserved", "observed delegated IPv6 prefix "+observedPrefix)
		}
		changes = append(changes,
			stateChange{Name: base + ".lease", Value: store.Set(base+".lease", routerstate.EncodePDLease(lease), res.ID()+": observed delegated prefix lease")},
			stateChange{Name: base + ".downstreamIfname", Value: store.Set(base+".downstreamIfname", observedIfname, res.ID()+": observed delegated prefix")},
		)
	}
	return changes, nil
}

func managedPDClientSnapshot(resource string) (pdclient.Snapshot, bool) {
	path := filepath.Join(pdClientLeaseDir, resource, "lease.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return pdclient.Snapshot{}, false
	}
	var snapshot pdclient.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return pdclient.Snapshot{}, false
	}
	if snapshot.CurrentPrefix == "" || snapshot.State != pdclient.StateBound {
		return pdclient.Snapshot{}, false
	}
	return snapshot, true
}

func mergePDLeaseSnapshot(lease routerstate.PDLease, snapshot pdclient.Snapshot) routerstate.PDLease {
	lease.CurrentPrefix = snapshot.CurrentPrefix
	lease.LastPrefix = snapshot.CurrentPrefix
	if !snapshot.UpdatedAt.IsZero() {
		lease.LastObservedAt = snapshot.UpdatedAt.Format(time.RFC3339)
	}
	if !snapshot.AcquiredAt.IsZero() {
		lease.LastReplyAt = snapshot.AcquiredAt.Format(time.RFC3339)
	} else if !snapshot.UpdatedAt.IsZero() {
		lease.LastReplyAt = snapshot.UpdatedAt.Format(time.RFC3339)
	}
	if snapshot.T1Seconds > 0 {
		lease.T1 = strconv.FormatInt(snapshot.T1Seconds, 10)
	}
	if snapshot.T2Seconds > 0 {
		lease.T2 = strconv.FormatInt(snapshot.T2Seconds, 10)
	}
	if snapshot.Preferred > 0 {
		lease.PLTime = strconv.FormatInt(snapshot.Preferred, 10)
	}
	if snapshot.Valid > 0 {
		lease.VLTime = strconv.FormatInt(snapshot.Valid, 10)
	}
	if snapshot.ServerDUID != "" {
		lease.DUID = snapshot.ServerDUID
	}
	if snapshot.IAID != 0 {
		lease.IAID = strconv.FormatUint(uint64(snapshot.IAID), 10)
	}
	return lease
}

func observedPrefixDelegationIdentity(ifname, client, configuredIAID string) dhcpIdentity {
	switch client {
	case "networkd":
		return observeNetworkdDHCPIdentity(ifname)
	case "dhcp6c":
		return observeFreeBSDDHCPv6CIdentity(configuredIAID)
	default:
		return dhcpIdentity{}
	}
}

type dhcpIdentity struct {
	IAID     string
	DUID     string
	DUIDText string
	Source   string
}

func observeNetworkdDHCPIdentity(ifname string) dhcpIdentity {
	ifindex := strings.TrimSpace(readFirstString(filepath.Join("/sys/class/net", ifname, "ifindex")))
	if ifindex == "" {
		return dhcpIdentity{}
	}
	leaseValues := parseKeyValueFile(filepath.Join("/run/systemd/netif/leases", ifindex))
	identity := parseRFC4361ClientID(leaseValues["CLIENTID"])
	if identity.Source != "" {
		identity.Source = "systemd-networkd-lease"
	}
	linkValues := parseKeyValueFile(filepath.Join("/run/systemd/netif/links", ifindex))
	if value := strings.Trim(linkValues["DHCPv6_CLIENT_DUID"], `"`); value != "" {
		identity.DUIDText = value
		if identity.Source == "" {
			identity.Source = "systemd-networkd-link"
		}
	}
	return identity
}

func observeFreeBSDDHCPv6CIdentity(configuredIAID string) dhcpIdentity {
	identity := dhcpIdentity{IAID: configuredOrDefaultDHCPv6CIAID(configuredIAID)}
	data, err := os.ReadFile("/var/db/dhcp6c_duid")
	if err != nil || len(data) == 0 {
		if identity.IAID != "" {
			identity.Source = "configured-iaid"
		}
		return identity
	}
	duid := freeBSDDHCPv6CDUIDPayload(data)
	if len(duid) == 0 {
		return identity
	}
	identity.DUID = hex.EncodeToString(duid)
	identity.DUIDText = colonHex(duid)
	identity.Source = "dhcp6c-duid-file"
	return identity
}

func configuredOrDefaultDHCPv6CIAID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "0"
	}
	parsed, ok := parseUint32Flexible(value)
	if !ok {
		return value
	}
	return strconv.FormatUint(uint64(parsed), 10)
}

func parseUint32Flexible(value string) (uint32, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return 0, false
	}
	base := 10
	if strings.HasPrefix(value, "0x") {
		base = 16
		value = strings.TrimPrefix(value, "0x")
	} else if len(value) == 8 && isLowerHex(value) {
		base = 16
	}
	parsed, err := strconv.ParseUint(value, base, 32)
	if err != nil {
		return 0, false
	}
	return uint32(parsed), true
}

func freeBSDDHCPv6CDUIDPayload(data []byte) []byte {
	if len(data) < 3 {
		return data
	}
	lengthLE := int(binary.LittleEndian.Uint16(data[:2]))
	lengthBE := int(binary.BigEndian.Uint16(data[:2]))
	switch {
	case lengthLE == len(data)-2:
		return data[2:]
	case lengthBE == len(data)-2:
		return data[2:]
	default:
		return data
	}
}

func colonHex(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	parts := make([]string, 0, len(data))
	for _, b := range data {
		parts = append(parts, fmt.Sprintf("%02x", b))
	}
	return strings.Join(parts, ":")
}

func parseRFC4361ClientID(value string) dhcpIdentity {
	value = strings.ToLower(strings.TrimSpace(strings.Trim(value, `"`)))
	if len(value) < 12 || !strings.HasPrefix(value, "ff") {
		return dhcpIdentity{}
	}
	iaid := value[2:10]
	duid := value[10:]
	if !isLowerHex(iaid) || !isLowerHex(duid) {
		return dhcpIdentity{}
	}
	return dhcpIdentity{IAID: iaid, DUID: duid, Source: "rfc4361-clientid"}
}

func expectedPrefixDelegationDUID(ifname, profile string) string {
	if !api.IsNTTIPv6PDProfile(profile) {
		return ""
	}
	mac := strings.TrimSpace(readFirstString(filepath.Join("/sys/class/net", ifname, "address")))
	return linkLayerDUIDFromMAC(mac)
}

func linkLayerDUIDFromMAC(mac string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(mac)), ":")
	if len(parts) != 6 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("00030001")
	for _, part := range parts {
		if len(part) != 2 || !isLowerHex(part) {
			return ""
		}
		builder.WriteString(part)
	}
	return builder.String()
}

func isLowerHex(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func readFirstString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func parseKeyValueFile(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return values
}

func delegatedAddressSuffixes(resources []api.Resource) map[uint64]bool {
	out := map[uint64]bool{}
	for _, res := range resources {
		if res.Kind != "IPv6DelegatedAddress" {
			continue
		}
		spec, err := res.IPv6DelegatedAddressSpec()
		if err != nil {
			continue
		}
		addr, err := netip.ParseAddr(defaultString(spec.AddressSuffix, "::"))
		if err != nil || !addr.Is6() {
			continue
		}
		out[ipv6HostSuffix64(addr)] = true
	}
	return out
}

func delegatedPrefixFromObservedInterface(ifname string, prefixLength int, managedSuffixes map[uint64]bool) (string, bool) {
	entries := ipv6AddressEntries(ifname)
	if prefix, ok := delegatedPrefixFromAddressEntries(entries, prefixLength, managedSuffixes); ok {
		return prefix, true
	}
	return delegatedPrefixFromObserved(ipv6Prefixes(ifname), ipv6Addresses(ifname), prefixLength)
}

func observedDHCPCDDelegatedPrefix(ifname string, prefixLength int) (string, routerstate.PDLease, bool) {
	out, err := exec.Command("dhcpcd", "-U", "-6", ifname).CombinedOutput()
	if err != nil {
		return "", routerstate.PDLease{}, false
	}
	return parseDHCPCDDumpLeasePD(out, prefixLength)
}

func parseDHCPCDDumpLeasePD(out []byte, prefixLength int) (string, routerstate.PDLease, bool) {
	values := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	lease := routerstate.PDLease{
		T1:     values["dhcp6_ia_pd1_t1"],
		T2:     values["dhcp6_ia_pd1_t2"],
		PLTime: values["dhcp6_ia_pd1_prefix1_pltime"],
		VLTime: values["dhcp6_ia_pd1_prefix1_vltime"],
	}
	prefixAddr := values["dhcp6_ia_pd1_prefix1"]
	if prefixAddr == "" {
		return "", lease, false
	}
	bits := prefixLength
	if bits <= 0 {
		if parsed, err := strconv.Atoi(values["dhcp6_ia_pd1_prefix1_length"]); err == nil {
			bits = parsed
		}
	}
	prefix, err := netip.ParsePrefix(fmt.Sprintf("%s/%d", prefixAddr, bits))
	if err != nil || !prefix.Addr().Is6() {
		return "", lease, false
	}
	return prefix.Masked().String(), lease, true
}

func delegatedPrefixFromAddressEntries(entries []ipv6AddressEntry, prefixLength int, ignoredSuffixes map[uint64]bool) (string, bool) {
	for _, entry := range entries {
		addr, err := netip.ParseAddr(entry.Address)
		if err != nil || !addr.Is6() || addr.IsLinkLocalUnicast() || entry.PrefixLen >= 128 {
			continue
		}
		if ignoredSuffixes[ipv6HostSuffix64(addr)] {
			continue
		}
		bits := entry.PrefixLen
		if prefixLength > 0 && prefixLength <= bits {
			bits = prefixLength
		}
		return netip.PrefixFrom(addr, bits).Masked().String(), true
	}
	return "", false
}

func delegatedPrefixFromObserved(prefixes, addresses []string, prefixLength int) (string, bool) {
	for _, value := range prefixes {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !prefix.Addr().Is6() || prefix.Addr().IsLinkLocalUnicast() || prefix.Bits() >= 128 {
			continue
		}
		if prefixLength > 0 && prefixLength <= prefix.Bits() {
			prefix = netip.PrefixFrom(prefix.Addr(), prefixLength)
		}
		return prefix.Masked().String(), true
	}
	return "", false
}
