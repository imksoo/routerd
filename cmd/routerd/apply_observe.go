// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/pdclient"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func recordObservedPrefixDelegationState(router *api.Router, store routerstate.Store, eventBus *bus.Bus) error {
	reader, ok := store.(routerstate.PDLeaseReader)
	if !ok {
		return fmt.Errorf("PD observation reader is unavailable")
	}
	statusStore, ok := store.(controllerchain.Store)
	if !ok {
		return fmt.Errorf("PD status store is unavailable")
	}
	aliases := map[string]string{}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "Interface" {
			continue
		}
		spec, err := resource.InterfaceSpec()
		if err != nil {
			return err
		}
		aliases[resource.Metadata.Name] = spec.IfName
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "DHCPv6PrefixDelegation" {
			continue
		}
		spec, err := resource.DHCPv6PrefixDelegationSpec()
		if err != nil {
			return err
		}
		if !api.ValidIPv6PDClient(spec.Client) {
			return fmt.Errorf("%s spec.client %q is unsupported; use routerd-dhcpv6-client or omit client", resource.ID(), spec.Client)
		}
		profile := defaultString(spec.Profile, api.IPv6PDProfileDefault)
		ifname := defaultString(aliases[spec.Interface], spec.Interface)
		duid, err := configuredPDClientDUID(ifname, spec.ClientDUID)
		if err != nil {
			return fmt.Errorf("observe PD %s client identity: %w", resource.Metadata.Name, err)
		}
		iaid := uint32(1)
		if value, ok := parseUint32Flexible(spec.IAID); ok {
			iaid = value
		}
		previous, _, err := reader.ReadPDLease(resource.Metadata.Name)
		if err != nil {
			return err
		}
		expectedDUID := ""
		if api.IsNTTIPv6PDProfile(profile) {
			expectedDUID, err = interfacePDClientDUID(ifname)
			if err != nil {
				return fmt.Errorf("observe PD %s expected identity: %w", resource.Metadata.Name, err)
			}
		}
		lease, _ := previous.Configured(routerstate.PDLeaseConfig{Client: api.IPv6PDClientRouterd, Profile: profile, UplinkIfname: ifname, PrefixLength: api.EffectiveIPv6PDPrefixLength(profile, spec.PrefixLength), ExpectedDUID: expectedDUID, ClientDUID: duid, IAID: iaid})
		var snapshot *pdclient.Snapshot
		if observed, found := managedPDClientSnapshot(resource.Metadata.Name, ifname, spec, duid); found {
			snapshot = &observed
		}
		// Missing files do not grant or revoke authority. The same atomic owner
		// commits any already-known lifetime expiry or configured source reset.
		if err := controllerchain.RecordPDLeaseObservation(statusStore, eventBus, router, resource.Metadata.Name, previous.Config(), lease, snapshot, daemonapi.ResourceStatus{}); err != nil {
			return fmt.Errorf("observe PD %s: %w", resource.Metadata.Name, err)
		}
	}
	return nil
}

func configuredPDClientDUID(ifname, configured string) (string, error) {
	value := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(configured), ":", ""))
	if value != "" {
		return value, nil
	}
	return interfacePDClientDUID(ifname)
}

func interfacePDClientDUID(ifname string) (string, error) {
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return "", fmt.Errorf("read interface %s for default DUID: %w", ifname, err)
	}
	return hex.EncodeToString(append([]byte{0, 3, 0, 1}, ifi.HardwareAddr...)), nil
}

func managedPDClientSnapshot(resource, ifname string, spec api.DHCPv6PrefixDelegationSpec, duid string) (pdclient.Snapshot, bool) {
	if api.EffectiveIPv6PDClient("", spec.Profile, spec.Client) != api.IPv6PDClientRouterd {
		return pdclient.Snapshot{}, false
	}
	path := filepath.Join(pdClientLeaseDir, resource, "lease.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return pdclient.Snapshot{}, false
	}
	var snapshot pdclient.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return pdclient.Snapshot{}, false
	}
	if !managedPDClientSnapshotIdentityMatches(snapshot, resource, ifname, spec, duid) {
		return pdclient.Snapshot{}, false
	}
	return snapshot, true
}

func managedPDClientSnapshotIdentityMatches(snapshot pdclient.Snapshot, resource, ifname string, spec api.DHCPv6PrefixDelegationSpec, expectedDUID string) bool {
	if snapshot.Resource != resource || snapshot.Interface != ifname {
		return false
	}
	expectedIAID := uint32(1)
	if parsed, ok := parseUint32Flexible(spec.IAID); ok {
		expectedIAID = parsed
	}
	return expectedDUID != "" && strings.EqualFold(snapshot.ClientDUID, expectedDUID) && snapshot.IAID == expectedIAID
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
