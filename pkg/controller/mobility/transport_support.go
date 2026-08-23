// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

func transportAddressSlots(spec api.SAMTransportProfileSpec, peers []api.SAMTransportPeerSpec, topology []string, inner netip.Prefix) (map[string]int, error) {
	addressingMode, err := transportAddressingMode(spec)
	if err != nil {
		return nil, err
	}
	switch addressingMode {
	case "pair-stable":
		return transportPairStableSlots(spec.SelfNodeRef, peers, inner)
	default:
		return transportEdgeIndex(spec.SelfNodeRef, topology)
	}
}

func transportEdgeIndex(selfNodeRef string, topology []string) (map[string]int, error) {
	if len(topology) < 2 {
		return nil, fmt.Errorf("resolved topology requires at least two nodes")
	}
	sort.Strings(topology)
	seen := map[string]bool{}
	for _, node := range topology {
		if node == "" {
			return nil, fmt.Errorf("resolved topology must not contain empty nodeRefs")
		}
		if seen[node] {
			return nil, fmt.Errorf("resolved topology nodeRef %q is duplicated", node)
		}
		seen[node] = true
	}
	if !seen[strings.TrimSpace(selfNodeRef)] {
		return nil, fmt.Errorf("selfNodeRef %q must be included by a resolved peer source", selfNodeRef)
	}
	out := map[string]int{}
	index := 0
	for i := 0; i < len(topology); i++ {
		for j := i + 1; j < len(topology); j++ {
			out[sortedEdgeKey(topology[i], topology[j])] = index
			index++
		}
	}
	return out, nil
}

func transportPairStableSlots(selfNodeRef string, peers []api.SAMTransportPeerSpec, inner netip.Prefix) (map[string]int, error) {
	capacity := 1 << (31 - inner.Bits())
	self := strings.TrimSpace(selfNodeRef)
	seedPrefix := inner.Masked().String()
	out := map[string]int{}
	used := map[int]string{}
	for _, peer := range peers {
		peerNode := strings.TrimSpace(peer.NodeRef)
		if peerNode == "" || peerNode == self {
			continue
		}
		edgeKey := sortedEdgeKey(self, peerNode)
		if _, exists := out[edgeKey]; exists {
			continue
		}
		slot := mobilityconfig.StableSAMTransportSlot(seedPrefix, self, peerNode, capacity)
		slotPrefix, err := mobilityconfig.SAMTransportSlotPrefix(inner, slot)
		if err != nil {
			return nil, fmt.Errorf("pair-stable slot computation failed for %s: %w", describeEdgeKey(edgeKey), err)
		}
		if previous, conflict := used[slot]; conflict && previous != edgeKey {
			return nil, fmt.Errorf("pair-stable inner /31 slot collision: %s and %s both map to %s; expand spec.innerPrefix",
				describeEdgeKey(previous), describeEdgeKey(edgeKey), slotPrefix)
		}
		used[slot] = edgeKey
		out[edgeKey] = slot
	}
	return out, nil
}

func (c TransportController) remoteEndpoint(peer api.SAMTransportPeerSpec) (string, string) {
	if endpoint := strings.TrimSpace(peer.RemoteEndpoint); endpoint != "" {
		return endpoint, ""
	}
	if strings.TrimSpace(peer.RemoteEndpointFrom.Resource) == "" {
		return "", ""
	}
	value := resourcequery.Value(c.Store, peer.RemoteEndpointFrom)
	if strings.TrimSpace(value) == "" {
		return "", peer.RemoteEndpointFrom.Resource + "." + firstNonEmpty(strings.TrimSpace(peer.RemoteEndpointFrom.Field), "phase")
	}
	return value, ""
}

func (c TransportController) upsertTransportDynamicPart(ctx context.Context, owner api.Resource, source, namePrefix string, resources []api.Resource, now time.Time) (string, error) {
	part := dynamicconfig.NewPart(safeName(namePrefix+"-"+owner.Metadata.Name), source, []api.OwnerRef{{
		APIVersion: api.MobilityAPIVersion,
		Kind:       "SAMTransportProfile",
		Name:       owner.Metadata.Name,
	}}, dynamicGeneration, now, now.Add(DefaultLeaseTTL))
	part.Spec.Resources = append([]api.Resource(nil), resources...)
	part.Spec.ActionPlans = []dynamicconfig.ActionPlan{}
	part.Spec.Digest = digestDynamicPart(part)
	record, err := codec.Encode(part)
	if err != nil {
		return "", err
	}
	previous, err := c.Store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		return "", err
	}
	changed := dynamicPartContentChanged(previous, record, now)
	if err := c.Store.UpsertDynamicConfigPart(record); err != nil {
		return "", err
	}
	if changed {
		publishDynamicConfigPartChanged(ctx, c.Bus, "sam-transport", owner, source, record.Digest, now)
	}
	return record.Digest, nil
}

func (c TransportController) upsertTransportPeerGroupPart(ctx context.Context, owner api.Resource, spec api.SAMTransportProfileSpec, source string, now time.Time) (string, string, error) {
	endpoint, pending, err := c.transportPeerGroupEndpoint(spec)
	if err != nil {
		return "", "", err
	}
	resources := []api.Resource(nil)
	if pending == "" {
		groupName := owner.Metadata.Name
		resources = append(resources, api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"},
			Metadata: api.ObjectMeta{
				Name: groupName,
				OwnerRefs: []api.OwnerRef{{
					APIVersion: api.MobilityAPIVersion,
					Kind:       "SAMTransportProfile",
					Name:       owner.Metadata.Name,
				}},
			},
			Spec: api.SAMPeerGroupSpec{
				Nodes: []api.SAMNodeSpec{{
					NodeRef:     strings.TrimSpace(spec.SelfNodeRef),
					SAMEndpoint: endpoint,
				}},
			},
		})
	}
	digest, err := c.upsertTransportDynamicPart(ctx, owner, source, "sam-peer-group", resources, now)
	if err != nil {
		return "", "", err
	}
	return pending, digest, nil
}

func (c TransportController) transportPeerGroupEndpoint(spec api.SAMTransportProfileSpec) (string, string, error) {
	if endpoint := strings.TrimSpace(spec.LocalEndpoint); endpoint != "" {
		addr, err := endpointAddress(endpoint)
		if err != nil {
			return "", "", fmt.Errorf("publishPeerGroup localEndpoint %q: %w", endpoint, err)
		}
		return addr.String(), "", nil
	}
	if strings.TrimSpace(spec.LocalEndpointFrom.Resource) == "" {
		return "", "localEndpoint", nil
	}
	value := resourcequery.Value(c.Store, spec.LocalEndpointFrom)
	if strings.TrimSpace(value) == "" {
		return "", spec.LocalEndpointFrom.Resource + "." + firstNonEmpty(strings.TrimSpace(spec.LocalEndpointFrom.Field), "phase"), nil
	}
	addr, err := endpointAddress(value)
	if err != nil {
		return "", "", fmt.Errorf("publishPeerGroup localEndpointFrom %s value %q: %w", spec.LocalEndpointFrom.Resource, value, err)
	}
	return addr.String(), "", nil
}

func (c TransportController) deprovisionStaleTransportSources(ctx context.Context, desired map[string]bool, now time.Time) error {
	parts, err := c.Store.ListDynamicConfigParts()
	if err != nil {
		return fmt.Errorf("list dynamic config parts for SAM transport GC: %w", err)
	}
	seen := map[string]bool{}
	for _, part := range parts {
		if !strings.HasPrefix(part.Source, samTransportSourceKind+"/") || desired[part.Source] || seen[part.Source] {
			continue
		}
		seen[part.Source] = true
		if profile, ok := parseTransportPeerGroupSource(part.Source); ok {
			if _, err := c.upsertTransportDynamicPart(ctx, api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile"},
				Metadata: api.ObjectMeta{Name: firstNonEmpty(profile, "deleted-peer-group")},
			}, part.Source, "sam-peer-group", nil, now); err != nil {
				return err
			}
			continue
		}
		profile, self := parseTransportSource(part.Source)
		if _, err := c.upsertTransportDynamicPart(ctx, api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile"},
			Metadata: api.ObjectMeta{Name: firstNonEmpty(profile, "deleted-"+self)},
		}, part.Source, "sam-transport", nil, now); err != nil {
			return err
		}
	}
	return nil
}

func (c TransportController) saveTransportStatus(profileName string, updates map[string]any) error {
	return saveMergedObjectStatus(c.Store, api.MobilityAPIVersion, "SAMTransportProfile", profileName, updates)
}

func TransportDynamicSource(profileName, selfNode string) string {
	return samTransportSourceKind + "/" + strings.TrimSpace(profileName) + "/node/" + strings.TrimSpace(selfNode)
}

func TransportPeerGroupDynamicSource(profileName string) string {
	return samTransportSourceKind + "/" + strings.TrimSpace(profileName) + "/peer-group"
}

func parseTransportSource(source string) (string, string) {
	parts := strings.Split(strings.TrimSpace(source), "/")
	if len(parts) >= 4 && parts[0] == samTransportSourceKind && parts[2] == "node" {
		return parts[1], parts[3]
	}
	return "", ""
}

func parseTransportPeerGroupSource(source string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(source), "/")
	if len(parts) == 3 && parts[0] == samTransportSourceKind && parts[2] == "peer-group" {
		return parts[1], true
	}
	return "", false
}

func sortedEdgeKey(a, b string) string {
	return mobilityconfig.SAMTransportPairKey(a, b)
}

func describeEdgeKey(key string) string {
	parts := strings.SplitN(key, "\x00", 2)
	if len(parts) != 2 {
		return key
	}
	return parts[0] + "<->" + parts[1]
}

func transportAddressingMode(spec api.SAMTransportProfileSpec) (string, error) {
	mode := mobilityconfig.NormalizeSAMTransportAddressingMode(spec.AddressingMode)
	if mode == "" {
		return "", fmt.Errorf("unsupported addressingMode %q", strings.TrimSpace(spec.AddressingMode))
	}
	return mode, nil
}

func derivedInnerAddresses(inner netip.Prefix, self, peer string, index int) (netip.Prefix, netip.Addr, error) {
	base, err := addIPv4(inner.Masked().Addr(), uint32(index*2))
	if err != nil {
		return netip.Prefix{}, netip.Addr{}, err
	}
	other, err := addIPv4(base, 1)
	if err != nil {
		return netip.Prefix{}, netip.Addr{}, err
	}
	lower, higher := base, other
	if self <= peer {
		return netip.PrefixFrom(lower, 31), higher, nil
	}
	return netip.PrefixFrom(higher, 31), lower, nil
}

func addIPv4(addr netip.Addr, offset uint32) (netip.Addr, error) {
	if !addr.Is4() {
		return netip.Addr{}, fmt.Errorf("address %s is not IPv4", addr)
	}
	bytes := addr.As4()
	n := binary.BigEndian.Uint32(bytes[:])
	binary.BigEndian.PutUint32(bytes[:], n+offset)
	return netip.AddrFrom4(bytes), nil
}

func endpointAddress(value string) (netip.Addr, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return netip.Addr{}, err
		}
		if !prefix.Addr().Is4() {
			return netip.Addr{}, fmt.Errorf("must be IPv4")
		}
		return prefix.Addr(), nil
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, err
	}
	if !addr.Is4() {
		return netip.Addr{}, fmt.Errorf("must be IPv4")
	}
	return addr, nil
}

func compactHashedName(prefix string, parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + hex.EncodeToString(h[:])[:11]
}

func transportAnnotations(profile, self, peer string) map[string]string {
	return map[string]string{
		"mobility.routerd.net/transport-profile": profile,
		"mobility.routerd.net/self-node":         self,
		"mobility.routerd.net/peer-node":         peer,
	}
}

func mobilityPoolImportPrefixes(router *api.Router) []string {
	if router == nil {
		return nil
	}
	var prefixes []string
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "MobilityPool" {
			continue
		}
		spec, err := resource.MobilityPoolSpec()
		if err != nil {
			continue
		}
		if prefix := strings.TrimSpace(spec.Prefix); prefix != "" {
			prefixes = append(prefixes, prefix)
		}
	}
	return mergeTransportPolicyStrings(prefixes)
}

func controllerNow(fn func() time.Time) time.Time {
	if fn != nil {
		return fn().UTC()
	}
	return time.Now().UTC()
}
