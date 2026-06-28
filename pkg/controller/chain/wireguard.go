// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/lifecycle"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"github.com/imksoo/routerd/pkg/wireguard"
)

type WireGuardController struct {
	Router       *api.Router
	Bus          *bus.Bus
	Store        Store
	DryRun       bool
	Command      wireguard.CommandRunner
	CommandStdin wireguard.CommandStdinRunner
	Logger       *slog.Logger
}

type wireGuardPeersFromStatus struct {
	Resource  string   `json:"resource"`
	Optional  bool     `json:"optional,omitempty"`
	Phase     string   `json:"phase"`
	PeerCount int      `json:"peerCount,omitempty"`
	Skipped   int      `json:"skipped,omitempty"`
	LeafIDs   []string `json:"leafIDs,omitempty"`
	Reason    string   `json:"reason,omitempty"`
}

type wireGuardPeerResolution struct {
	Router         *api.Router
	PeersFrom      map[string][]wireGuardPeersFromStatus
	PendingSources map[string][]string
}

func (c WireGuardController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	resolved, err := c.resolvePeerResources()
	if err != nil {
		return err
	}
	current := c
	current.Router = resolved.Router
	if err := current.cleanupStaleResources(ctx); err != nil {
		return err
	}
	for _, resource := range current.Router.Spec.Resources {
		if resource.Kind != "WireGuardInterface" {
			continue
		}
		if err := current.reconcileInterface(ctx, resource, resolved.PeersFrom[resource.Metadata.Name], resolved.PendingSources[resource.Metadata.Name]); err != nil {
			return err
		}
	}
	return nil
}

func (c WireGuardController) resolvePeerResources() (wireGuardPeerResolution, error) {
	resolution := wireGuardPeerResolution{
		Router:         c.Router,
		PeersFrom:      map[string][]wireGuardPeersFromStatus{},
		PendingSources: map[string][]string{},
	}
	if c.Router == nil {
		return resolution, nil
	}
	generated := []api.Resource{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "WireGuardInterface" {
			continue
		}
		spec, err := resource.WireGuardInterfaceSpec()
		if err != nil {
			return resolution, err
		}
		peers, statuses, pending, err := c.resolvePeersFrom(resource.Metadata.Name, spec)
		if err != nil {
			resolution.PeersFrom[resource.Metadata.Name] = statuses
			resolution.PendingSources[resource.Metadata.Name] = pending
			return resolution, err
		}
		resolution.PeersFrom[resource.Metadata.Name] = statuses
		resolution.PendingSources[resource.Metadata.Name] = pending
		generated = append(generated, peers...)
	}
	if len(generated) == 0 {
		return resolution, nil
	}
	merged := make([]api.Resource, 0, len(c.Router.Spec.Resources)+len(generated))
	genIndex := map[string]int{}
	addGenerated := func(res api.Resource) {
		name := strings.TrimSpace(res.Metadata.Name)
		if name == "" {
			return
		}
		res.Metadata.Name = name
		key := res.Kind + "/" + name
		if existing, ok := genIndex[key]; ok {
			merged[existing] = res
			return
		}
		genIndex[key] = len(merged)
		merged = append(merged, res)
	}
	for _, res := range generated {
		addGenerated(res)
	}
	for _, resource := range c.Router.Spec.Resources {
		key := resource.Kind + "/" + resource.Metadata.Name
		if resource.Kind == "WireGuardPeer" {
			addGenerated(resource)
			continue
		}
		if _, overrides := genIndex[key]; overrides {
			addGenerated(resource)
			continue
		}
		merged = append(merged, resource)
	}
	router := *c.Router
	router.Spec.Resources = merged
	resolution.Router = &router
	return resolution, nil
}

func (c WireGuardController) resolvePeersFrom(iface string, spec api.WireGuardInterfaceSpec) ([]api.Resource, []wireGuardPeersFromStatus, []string, error) {
	peers := []api.Resource{}
	statuses := make([]wireGuardPeersFromStatus, 0, len(spec.PeersFrom))
	pending := []string{}
	self := strings.TrimSpace(spec.SelfNodeRef)
	if self == "" && c.Router != nil {
		self = strings.TrimSpace(c.Router.Metadata.Name)
	}
	for _, source := range spec.PeersFrom {
		ref := strings.TrimSpace(source.Resource)
		status := wireGuardPeersFromStatus{
			Resource: ref,
			Optional: source.Optional,
			Phase:    "Resolved",
		}
		sourceKind, _, sourceOK := strings.Cut(ref, "/")
		if sourceOK && sourceKind == "SAMRRSet" {
			rrSet, found, err := c.samRRSet(ref)
			if err != nil {
				status.Phase = "Invalid"
				status.Reason = err.Error()
				statuses = append(statuses, status)
				return peers, statuses, pending, err
			}
			if !found {
				status.Phase = "Missing"
				status.Reason = "SAMRRSet not found"
				statuses = append(statuses, status)
				if !source.Optional {
					pending = append(pending, ref)
				}
				continue
			}
			for _, member := range rrSet.Members {
				nodeRef := strings.TrimSpace(member.NodeRef)
				if nodeRef == "" || nodeRef == self {
					continue
				}
				if strings.TrimSpace(member.WireGuard.PublicKey) == "" {
					continue
				}
				tunnel, err := parseEnrollmentTunnelAddress(member.TunnelAddress)
				if err != nil {
					status.Phase = "Invalid"
					status.Reason = err.Error()
					statuses = append(statuses, status)
					return peers, statuses, pending, err
				}
				allowedIPs := []string{tunnel.String()}
				allowedIPs = append(allowedIPs, member.WireGuard.AllowedIPs...)
				peers = append(peers, api.Resource{
					TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardPeer"},
					Metadata: api.ObjectMeta{
						Name: nodeRef,
						Annotations: map[string]string{
							"routerd.net/generated-from": ref,
						},
					},
					Spec: api.WireGuardPeerSpec{
						Interface:           iface,
						PublicKey:           strings.TrimSpace(member.WireGuard.PublicKey),
						AllowedIPs:          allowedIPs,
						Endpoint:            strings.TrimSpace(member.WireGuard.Endpoint),
						PersistentKeepalive: member.WireGuard.PersistentKeepalive,
					},
				})
				status.PeerCount++
			}
			statuses = append(statuses, status)
			continue
		}
		if sourceOK && sourceKind == "SAMEnrollmentPolicy" {
			nodeSet, found, skipped, leafIDs, err := c.samEnrollmentNodeSet(ref, iface)
			if err != nil {
				status.Phase = "Invalid"
				status.Reason = err.Error()
				statuses = append(statuses, status)
				return peers, statuses, pending, err
			}
			if !found {
				status.Phase = "Missing"
				status.Reason = "SAMEnrollmentPolicy not found"
				statuses = append(statuses, status)
				if !source.Optional {
					pending = append(pending, ref)
				}
				continue
			}
			status.Skipped = skipped
			status.LeafIDs = append([]string(nil), leafIDs...)
			for _, node := range nodeSet.Nodes {
				nodeRef := strings.TrimSpace(node.NodeRef)
				wg := node.WireGuard
				if nodeRef == "" || nodeRef == self || !samNodeWireGuardConfigured(wg) {
					continue
				}
				peers = append(peers, api.Resource{
					TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardPeer"},
					Metadata: api.ObjectMeta{
						Name: nodeRef,
						Annotations: map[string]string{
							"routerd.net/generated-from": ref,
						},
					},
					Spec: api.WireGuardPeerSpec{
						Interface:           iface,
						PublicKey:           strings.TrimSpace(wg.PublicKey),
						AllowedIPs:          append([]string(nil), wg.AllowedIPs...),
						Endpoint:            strings.TrimSpace(wg.Endpoint),
						PersistentKeepalive: wg.PersistentKeepalive,
					},
				})
				status.PeerCount++
			}
			statuses = append(statuses, status)
			_ = c.saveSAMEnrollmentPolicyStatus(ref, status)
			continue
		}
		nodeSet, found, err := c.samNodeSet(ref)
		if err != nil {
			status.Phase = "Invalid"
			status.Reason = err.Error()
			statuses = append(statuses, status)
			return peers, statuses, pending, err
		}
		if !found {
			status.Phase = "Missing"
			status.Reason = "SAMNodeSet not found"
			statuses = append(statuses, status)
			if !source.Optional {
				pending = append(pending, ref)
			}
			continue
		}
		for _, node := range nodeSet.Nodes {
			nodeRef := strings.TrimSpace(node.NodeRef)
			wg := node.WireGuard
			if nodeRef == "" || nodeRef == self || !samNodeWireGuardConfigured(wg) {
				continue
			}
			peers = append(peers, api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardPeer"},
				Metadata: api.ObjectMeta{
					Name: nodeRef,
					Annotations: map[string]string{
						"routerd.net/generated-from": ref,
					},
				},
				Spec: api.WireGuardPeerSpec{
					Interface:           iface,
					PublicKey:           strings.TrimSpace(wg.PublicKey),
					AllowedIPs:          append([]string(nil), wg.AllowedIPs...),
					Endpoint:            strings.TrimSpace(wg.Endpoint),
					PersistentKeepalive: wg.PersistentKeepalive,
				},
			})
			if ep := strings.TrimSpace(node.SAMEndpoint); ep != "" {
				if addr, err := netip.ParseAddr(ep); err == nil {
					peers = append(peers, api.Resource{
						TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
						Metadata: api.ObjectMeta{
							Name: "wg-sam-endpoint-" + nodeRef,
							Annotations: map[string]string{
								"routerd.net/generated-from": ref,
							},
						},
						Spec: api.IPv4RouteSpec{
							Destination: netip.PrefixFrom(addr, 32).String(),
							Device:      iface,
						},
					})
				}
			}
			status.PeerCount++
		}
		statuses = append(statuses, status)
	}
	sort.Strings(pending)
	return peers, statuses, pending, nil
}

func (c WireGuardController) samNodeSet(ref string) (api.SAMNodeSetSpec, bool, error) {
	if c.Router == nil {
		return api.SAMNodeSetSpec{}, false, nil
	}
	return lookupSAMNodeSet(c.Router, ref)
}

func (c WireGuardController) samRRSet(ref string) (api.SAMRRSetSpec, bool, error) {
	if c.Router == nil {
		return api.SAMRRSetSpec{}, false, nil
	}
	return lookupSAMRRSet(c.Router, ref)
}

func (c WireGuardController) samEnrollmentNodeSet(ref, iface string) (api.SAMNodeSetSpec, bool, int, []string, error) {
	if c.Router == nil {
		return api.SAMNodeSetSpec{}, false, 0, nil, nil
	}
	policy, found, err := lookupSAMEnrollmentPolicy(c.Router, ref)
	if err != nil || !found {
		return api.SAMNodeSetSpec{}, found, 0, nil, err
	}
	if want := strings.TrimSpace(policy.WireGuard.Interface); want != "" && want != strings.TrimSpace(iface) {
		return api.SAMNodeSetSpec{}, true, 0, nil, fmt.Errorf("%s spec.wireGuard.interface %q does not match WireGuardInterface %q", ref, want, iface)
	}
	nodeSet, skipped, leafIDs, err := enrollmentPolicyNodeSet(c.Router, ref, policy, time.Now().UTC())
	return nodeSet, true, skipped, leafIDs, err
}

func (c WireGuardController) saveSAMEnrollmentPolicyStatus(ref string, status wireGuardPeersFromStatus) error {
	if c.Store == nil {
		return nil
	}
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != "SAMEnrollmentPolicy" || strings.TrimSpace(name) == "" {
		return nil
	}
	return c.Store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentPolicy", strings.TrimSpace(name), map[string]any{
		"phase":          status.Phase,
		"acceptedClaims": status.PeerCount,
		"skippedClaims":  status.Skipped,
		"leafIDs":        append([]string(nil), status.LeafIDs...),
		"updatedAt":      time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func lookupSAMNodeSet(router *api.Router, ref string) (api.SAMNodeSetSpec, bool, error) {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != "SAMNodeSet" || strings.TrimSpace(name) == "" {
		return api.SAMNodeSetSpec{}, false, fmt.Errorf("peersFrom resource must reference SAMNodeSet/<name>")
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMNodeSet" || resource.Metadata.Name != strings.TrimSpace(name) {
			continue
		}
		spec, err := resource.SAMNodeSetSpec()
		if err != nil {
			return api.SAMNodeSetSpec{}, true, fmt.Errorf("%s spec: %w", ref, err)
		}
		return spec, true, nil
	}
	return api.SAMNodeSetSpec{}, false, nil
}

func lookupSAMEnrollmentPolicy(router *api.Router, ref string) (api.SAMEnrollmentPolicySpec, bool, error) {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != "SAMEnrollmentPolicy" || strings.TrimSpace(name) == "" {
		return api.SAMEnrollmentPolicySpec{}, false, fmt.Errorf("peersFrom resource must reference SAMEnrollmentPolicy/<name>")
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentPolicy" || resource.Metadata.Name != strings.TrimSpace(name) {
			continue
		}
		spec, err := resource.SAMEnrollmentPolicySpec()
		if err != nil {
			return api.SAMEnrollmentPolicySpec{}, true, fmt.Errorf("%s spec: %w", ref, err)
		}
		return spec, true, nil
	}
	return api.SAMEnrollmentPolicySpec{}, false, nil
}

func lookupSAMRRSet(router *api.Router, ref string) (api.SAMRRSetSpec, bool, error) {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != "SAMRRSet" || strings.TrimSpace(name) == "" {
		return api.SAMRRSetSpec{}, false, fmt.Errorf("peersFrom resource must reference SAMRRSet/<name>")
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMRRSet" || resource.Metadata.Name != strings.TrimSpace(name) {
			continue
		}
		spec, err := resource.SAMRRSetSpec()
		if err != nil {
			return api.SAMRRSetSpec{}, true, fmt.Errorf("%s spec: %w", ref, err)
		}
		return spec, true, nil
	}
	return api.SAMRRSetSpec{}, false, nil
}

func enrollmentPolicyNodeSet(router *api.Router, ref string, policy api.SAMEnrollmentPolicySpec, now time.Time) (api.SAMNodeSetSpec, int, []string, error) {
	_, policyName, _ := strings.Cut(strings.TrimSpace(ref), "/")
	var nodes []api.SAMNodeSpec
	var leafIDs []string
	skipped := 0
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentClaim" {
			continue
		}
		claim, err := resource.SAMEnrollmentClaimSpec()
		if err != nil {
			return api.SAMNodeSetSpec{}, skipped, leafIDs, err
		}
		_, claimPolicyName, _ := strings.Cut(strings.TrimSpace(claim.PolicyRef), "/")
		if claimPolicyName != strings.TrimSpace(policyName) {
			continue
		}
		if claim.Revoked || enrollmentClaimExpired(claim, now) {
			skipped++
			continue
		}
		tunnel, err := parseEnrollmentTunnelAddress(claim.TunnelAddress)
		if err != nil || !enrollmentPrefixContains(policy.TunnelAddressPrefixes, tunnel.Addr()) {
			skipped++
			continue
		}
		allowed := []string{tunnel.String()}
		allowed = append(allowed, claim.WireGuard.AllowedIPs...)
		keepalive := claim.WireGuard.PersistentKeepalive
		if keepalive == 0 {
			keepalive = policy.WireGuard.PersistentKeepalive
		}
		nodes = append(nodes, api.SAMNodeSpec{
			NodeRef:     strings.TrimSpace(claim.LeafID),
			Role:        "cloud",
			SAMEndpoint: tunnel.Addr().String(),
			WireGuard: api.SAMNodeWireGuardSpec{
				PublicKey:           strings.TrimSpace(claim.WireGuard.PublicKey),
				Endpoint:            strings.TrimSpace(claim.WireGuard.Endpoint),
				AllowedIPs:          allowed,
				PersistentKeepalive: keepalive,
			},
		})
		leafIDs = append(leafIDs, strings.TrimSpace(claim.LeafID))
	}
	sort.Strings(leafIDs)
	return api.SAMNodeSetSpec{Nodes: nodes}, skipped, leafIDs, nil
}

func enrollmentClaimExpired(claim api.SAMEnrollmentClaimSpec, now time.Time) bool {
	if strings.TrimSpace(claim.ExpiresAt) == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(claim.ExpiresAt))
	if err != nil {
		expiresAt, err = time.Parse(time.RFC3339, strings.TrimSpace(claim.ExpiresAt))
		if err != nil {
			return true
		}
	}
	return !expiresAt.After(now)
}

func parseEnrollmentTunnelAddress(value string) (netip.Prefix, error) {
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
	return netip.PrefixFrom(addr, 32), nil
}

func enrollmentPrefixContains(prefixes []string, addr netip.Addr) bool {
	for _, value := range prefixes {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err == nil && prefix.Masked().Contains(addr) {
			return true
		}
	}
	return false
}

// resolveWireGuardSAMResources resolves peersFrom SAMNodeSet references on
// WireGuardInterface resources, generating WireGuardPeer (peer),
// IPv4Route (peer samEndpoint), and IPv4StaticAddress (self samEndpoint)
// resources. Called from effectiveRouterForReconcile so all controllers
// see the generated resources and each independently reconciles its own
// resource types.
func resolveWireGuardSAMResources(router *api.Router) (*api.Router, error) {
	if router == nil {
		return router, nil
	}
	var generated []api.Resource
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "WireGuardInterface" {
			continue
		}
		spec, err := resource.WireGuardInterfaceSpec()
		if err != nil {
			return nil, err
		}
		if len(spec.PeersFrom) == 0 {
			continue
		}
		iface := resource.Metadata.Name
		self := strings.TrimSpace(spec.SelfNodeRef)
		if self == "" {
			self = strings.TrimSpace(router.Metadata.Name)
		}
		for _, source := range spec.PeersFrom {
			ref := strings.TrimSpace(source.Resource)
			sourceKind, _, sourceOK := strings.Cut(ref, "/")
			if sourceOK && sourceKind == "SAMRRSet" {
				rrSet, found, err := lookupSAMRRSet(router, ref)
				if err != nil {
					if source.Optional {
						continue
					}
					return nil, err
				}
				if !found {
					continue
				}
				for _, member := range rrSet.Members {
					nodeRef := strings.TrimSpace(member.NodeRef)
					if nodeRef == "" || nodeRef == self {
						continue
					}
					if strings.TrimSpace(member.WireGuard.PublicKey) == "" {
						continue
					}
					tunnel, err := parseEnrollmentTunnelAddress(member.TunnelAddress)
					if err != nil {
						if source.Optional {
							continue
						}
						return nil, err
					}
					allowedIPs := []string{tunnel.String()}
					allowedIPs = append(allowedIPs, member.WireGuard.AllowedIPs...)
					generated = append(generated, api.Resource{
						TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardPeer"},
						Metadata: api.ObjectMeta{
							Name: nodeRef,
							Annotations: map[string]string{
								"routerd.net/generated-from": ref,
							},
						},
						Spec: api.WireGuardPeerSpec{
							Interface:           iface,
							PublicKey:           strings.TrimSpace(member.WireGuard.PublicKey),
							AllowedIPs:          allowedIPs,
							Endpoint:            strings.TrimSpace(member.WireGuard.Endpoint),
							PersistentKeepalive: member.WireGuard.PersistentKeepalive,
						},
					})
				}
				continue
			}
			if sourceOK && sourceKind == "SAMEnrollmentPolicy" {
				policy, found, err := lookupSAMEnrollmentPolicy(router, ref)
				if err != nil {
					if source.Optional {
						continue
					}
					return nil, err
				}
				if !found {
					continue
				}
				if want := strings.TrimSpace(policy.WireGuard.Interface); want != "" && want != strings.TrimSpace(iface) {
					if source.Optional {
						continue
					}
					return nil, fmt.Errorf("%s spec.wireGuard.interface %q does not match WireGuardInterface %q", ref, want, iface)
				}
				nodeSet, _, _, err := enrollmentPolicyNodeSet(router, ref, policy, time.Now().UTC())
				if err != nil {
					if source.Optional {
						continue
					}
					return nil, err
				}
				for _, node := range nodeSet.Nodes {
					nodeRef := strings.TrimSpace(node.NodeRef)
					if nodeRef == "" || nodeRef == self {
						continue
					}
					wg := node.WireGuard
					if !samNodeWireGuardConfigured(wg) {
						continue
					}
					generated = append(generated, api.Resource{
						TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardPeer"},
						Metadata: api.ObjectMeta{
							Name: nodeRef,
							Annotations: map[string]string{
								"routerd.net/generated-from": ref,
							},
						},
						Spec: api.WireGuardPeerSpec{
							Interface:           iface,
							PublicKey:           strings.TrimSpace(wg.PublicKey),
							AllowedIPs:          append([]string(nil), wg.AllowedIPs...),
							Endpoint:            strings.TrimSpace(wg.Endpoint),
							PersistentKeepalive: wg.PersistentKeepalive,
						},
					})
					if ep := strings.TrimSpace(node.SAMEndpoint); ep != "" {
						if addr, parseErr := netip.ParseAddr(ep); parseErr == nil {
							generated = append(generated, api.Resource{
								TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
								Metadata: api.ObjectMeta{
									Name: "wg-sam-endpoint-" + nodeRef,
									Annotations: map[string]string{
										"routerd.net/generated-from": ref,
									},
								},
								Spec: api.IPv4RouteSpec{
									Destination: netip.PrefixFrom(addr, 32).String(),
									Device:      iface,
								},
							})
						}
					}
				}
				continue
			}
			nodeSet, found, err := lookupSAMNodeSet(router, ref)
			if err != nil {
				if source.Optional {
					continue
				}
				return nil, err
			}
			if !found {
				continue
			}
			for _, node := range nodeSet.Nodes {
				nodeRef := strings.TrimSpace(node.NodeRef)
				if nodeRef == "" {
					continue
				}
				if nodeRef == self {
					if ep := strings.TrimSpace(node.SAMEndpoint); ep != "" {
						if addr, parseErr := netip.ParseAddr(ep); parseErr == nil {
							generated = append(generated, api.Resource{
								TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
								Metadata: api.ObjectMeta{
									Name: "wg-sam-addr-" + iface,
									Annotations: map[string]string{
										"routerd.net/generated-from": ref,
									},
								},
								Spec: api.IPv4StaticAddressSpec{
									Interface: iface,
									Address:   netip.PrefixFrom(addr, 32).String(),
								},
							})
						}
					}
					continue
				}
				wg := node.WireGuard
				if !samNodeWireGuardConfigured(wg) {
					continue
				}
				generated = append(generated, api.Resource{
					TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardPeer"},
					Metadata: api.ObjectMeta{
						Name: nodeRef,
						Annotations: map[string]string{
							"routerd.net/generated-from": ref,
						},
					},
					Spec: api.WireGuardPeerSpec{
						Interface:           iface,
						PublicKey:           strings.TrimSpace(wg.PublicKey),
						AllowedIPs:          append([]string(nil), wg.AllowedIPs...),
						Endpoint:            strings.TrimSpace(wg.Endpoint),
						PersistentKeepalive: wg.PersistentKeepalive,
					},
				})
				if ep := strings.TrimSpace(node.SAMEndpoint); ep != "" {
					if addr, parseErr := netip.ParseAddr(ep); parseErr == nil {
						generated = append(generated, api.Resource{
							TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
							Metadata: api.ObjectMeta{
								Name: "wg-sam-endpoint-" + nodeRef,
								Annotations: map[string]string{
									"routerd.net/generated-from": ref,
								},
							},
							Spec: api.IPv4RouteSpec{
								Destination: netip.PrefixFrom(addr, 32).String(),
								Device:      iface,
							},
						})
					}
				}
			}
		}
	}
	if len(generated) == 0 {
		return router, nil
	}
	merged := make([]api.Resource, 0, len(router.Spec.Resources)+len(generated))
	genIndex := map[string]int{}
	addGenerated := func(res api.Resource) {
		name := strings.TrimSpace(res.Metadata.Name)
		if name == "" {
			return
		}
		res.Metadata.Name = name
		key := res.Kind + "/" + name
		if existing, ok := genIndex[key]; ok {
			merged[existing] = res
			return
		}
		genIndex[key] = len(merged)
		merged = append(merged, res)
	}
	for _, res := range generated {
		addGenerated(res)
	}
	for _, resource := range router.Spec.Resources {
		key := resource.Kind + "/" + resource.Metadata.Name
		if resource.Kind == "WireGuardPeer" {
			addGenerated(resource)
			continue
		}
		if _, overrides := genIndex[key]; overrides {
			addGenerated(resource)
			continue
		}
		merged = append(merged, resource)
	}
	result := *router
	result.Spec.Resources = merged
	return &result, nil
}

func samNodeWireGuardConfigured(spec api.SAMNodeWireGuardSpec) bool {
	return strings.TrimSpace(spec.PublicKey) != "" ||
		strings.TrimSpace(spec.Endpoint) != "" ||
		len(spec.AllowedIPs) > 0 ||
		spec.PersistentKeepalive != 0
}

func (c WireGuardController) cleanupStaleResources(ctx context.Context) error {
	lister, ok := c.Store.(routerstate.ObjectStatusLister)
	if !ok {
		return nil
	}
	deleter, ok := c.Store.(routerstate.ObjectDeleteStore)
	if !ok {
		return nil
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return err
	}
	desiredInterfaces := map[string]struct{}{}
	desiredPeers := map[string]struct{}{}
	desiredListenPorts := map[int]struct{}{}
	desired := map[string]bool{}
	for _, resource := range c.Router.Spec.Resources {
		switch resource.Kind {
		case "WireGuardInterface":
			desiredInterfaces[resource.Metadata.Name] = struct{}{}
			desired[lifecycle.OwnerKey(api.NetAPIVersion, "WireGuardInterface", resource.Metadata.Name)] = true
			if spec, err := resource.WireGuardInterfaceSpec(); err == nil && spec.ListenPort > 0 {
				desiredListenPorts[spec.ListenPort] = struct{}{}
			}
			if ifname := interfaceIfName(c.Router, resource.Metadata.Name); ifname != "" {
				desiredInterfaces[ifname] = struct{}{}
				desired[lifecycle.OwnerKey(api.NetAPIVersion, "WireGuardInterface", ifname)] = true
			}
		case "WireGuardPeer":
			desiredPeers[resource.Metadata.Name] = struct{}{}
			desired[lifecycle.OwnerKey(api.NetAPIVersion, "WireGuardPeer", resource.Metadata.Name)] = true
		}
	}
	staleInterfaces := map[string]struct{}{}
	plan := lifecycle.PlanResourceTeardownGC(desired, statuses)
	for _, action := range plan.Actions {
		if action.Type != lifecycle.GCActionTeardownResource {
			continue
		}
		item := action.Status
		if item.APIVersion != api.NetAPIVersion || item.Kind != "WireGuardInterface" || !routerdManagedObjectStatus(item) {
			continue
		}
		ifname := firstNonEmpty(statusString(item.Status, "ifname"), statusString(item.Status, "interface"), item.Name)
		if err := c.teardownWireGuardInterface(ctx, item, ifname, desiredListenPorts, deleter); err != nil {
			return err
		}
		staleInterfaces[item.Name] = struct{}{}
		staleInterfaces[ifname] = struct{}{}
	}
	for _, action := range plan.Actions {
		if action.Type != lifecycle.GCActionTeardownResource {
			continue
		}
		item := action.Status
		if item.APIVersion != api.NetAPIVersion || item.Kind != "WireGuardPeer" || !routerdManagedObjectStatus(item) {
			continue
		}
		if err := c.teardownWireGuardPeer(ctx, item, deleter); err != nil {
			return err
		}
	}
	for _, item := range statuses {
		if item.APIVersion != api.NetAPIVersion || item.Kind != "WireGuardPeer" || !routerdManagedObjectStatus(item) {
			continue
		}
		if _, peerStillDesired := desiredPeers[item.Name]; !peerStillDesired {
			continue
		}
		if _, interfaceRemoved := staleInterfaces[statusString(item.Status, "interface")]; !interfaceRemoved {
			continue
		}
		if err := c.teardownWireGuardPeer(ctx, item, deleter); err != nil {
			return err
		}
	}
	return nil
}

func (c WireGuardController) teardownWireGuardInterface(ctx context.Context, item routerstate.ObjectStatus, ifname string, desiredListenPorts map[int]struct{}, deleter routerstate.ObjectDeleteStore) error {
	if ifname != "" && !c.DryRun {
		if err := c.deleteWireGuardInterface(ctx, ifname); err != nil {
			return err
		}
	}
	if port := wireGuardHostFirewallPort(item.Status); port > 0 {
		if _, stillDesired := desiredListenPorts[port]; !stillDesired && !c.DryRun {
			if err := c.deleteWireGuardInputAccept(ctx, port); err != nil {
				return err
			}
		}
	}
	if err := deleter.DeleteObject(item.APIVersion, item.Kind, item.Name); err != nil {
		return err
	}
	return c.publishWireGuardRemoved(ctx, item, map[string]string{"interface": ifname})
}

func (c WireGuardController) teardownWireGuardPeer(ctx context.Context, item routerstate.ObjectStatus, deleter routerstate.ObjectDeleteStore) error {
	if err := deleter.DeleteObject(item.APIVersion, item.Kind, item.Name); err != nil {
		return err
	}
	return c.publishWireGuardRemoved(ctx, item, map[string]string{"interface": statusString(item.Status, "interface")})
}

func (c WireGuardController) publishWireGuardRemoved(ctx context.Context, item routerstate.ObjectStatus, attrs map[string]string) error {
	if c.Bus == nil {
		return nil
	}
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.wireguard.resource.removed", daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: item.APIVersion, Kind: item.Kind, Name: item.Name}
	event.Attributes = attrs
	return c.Bus.Publish(ctx, event)
}

func (c WireGuardController) deleteWireGuardInterface(ctx context.Context, ifname string) error {
	run := c.Command
	if run == nil {
		run = wireguard.DefaultCommandRunner
	}
	out, err := run(ctx, "ip", "link", "delete", "dev", ifname)
	if err == nil || wireGuardDeleteMissingLink(out, err) {
		return nil
	}
	return fmt.Errorf("delete stale WireGuard interface %s: %w: %s", ifname, err, strings.TrimSpace(string(out)))
}

func wireGuardDeleteMissingLink(out []byte, err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(string(out)) + " " + err.Error())
	for _, needle := range []string{"cannot find device", "does not exist", "not found", "no such device"} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func routerdManagedObjectStatus(item routerstate.ObjectStatus) bool {
	if managed, ok := statusBool(item.Status["managed"]); ok && !managed {
		return false
	}
	managedBy := firstNonEmpty(item.ManagedBy, statusString(item.Status, "managedBy"))
	if strings.EqualFold(managedBy, "external") {
		return false
	}
	management := firstNonEmpty(item.Management, statusString(item.Status, "management"))
	if strings.EqualFold(management, "adopted") {
		return false
	}
	if managedBy == "" && management == "" && resourceOwnerController(item.Kind) == "" {
		return false
	}
	return true
}

func statusString(status map[string]any, key string) string {
	if status == nil {
		return ""
	}
	value, ok := status[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func (c WireGuardController) reconcileInterface(ctx context.Context, resource api.Resource, peersFrom []wireGuardPeersFromStatus, pendingSources []string) error {
	cfg, err := wireguard.BuildInterface(resource, c.Router.Spec.Resources)
	if err != nil {
		return err
	}
	if err := c.saveUnconfiguredPeerStatuses(resource.Metadata.Name); err != nil {
		return err
	}
	status := map[string]any{
		"phase":      "Pending",
		"interface":  resource.Metadata.Name,
		"ifname":     cfg.Name,
		"listenPort": cfg.ListenPort,
		"mtu":        cfg.MTU,
		"fwmark":     cfg.FwMark,
		"peerCount":  len(cfg.Peers),
		"dryRun":     c.DryRun,
	}
	spec, err := resource.WireGuardInterfaceSpec()
	if err != nil {
		return err
	}
	if self := c.selfNodeRef(spec); self != "" {
		status["selfNodeRef"] = self
	}
	if len(peersFrom) > 0 {
		status["peersFrom"] = wireGuardPeersFromStatusMaps(peersFrom)
	}
	if len(pendingSources) > 0 {
		status["pendingSources"] = append([]string(nil), pendingSources...)
		status["reason"] = "PeersFromPending"
		status["message"] = "peersFrom source is not resolved"
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardInterface", resource.Metadata.Name, status); err != nil {
			return err
		}
		c.savePeerPendingStatuses(resource.Metadata.Name, cfg.Peers, "PeersFromPending")
		return nil
	}
	configHash := wireGuardConfigHash(cfg, c.DryRun)
	if configHash != "" {
		status["configHash"] = configHash
	}
	if cfg.PrivateKey == "" && cfg.PrivateKeyFile == "" {
		status["reason"] = "PrivateKeyMissing"
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardInterface", resource.Metadata.Name, status); err != nil {
			return err
		}
		c.savePeerPendingStatuses(resource.Metadata.Name, cfg.Peers, "InterfacePending")
		return nil
	}
	controller := wireguard.Controller{Command: c.Command, CommandStdin: c.CommandStdin, DryRun: c.DryRun}
	observed, statusErr := c.interfaceStatus(ctx, cfg.Name)
	applied := false
	current := c.Store.ObjectStatus(api.NetAPIVersion, "WireGuardInterface", resource.Metadata.Name)
	if !c.DryRun && statusErr == nil && configHash != "" && fmt.Sprint(current["configHash"]) == configHash && c.interfaceMatchesDesired(ctx, cfg, observed) {
		status["reason"] = "AlreadyConfigured"
	} else if _, err := controller.Apply(ctx, cfg); err != nil {
		status["phase"] = "Error"
		status["reason"] = "ApplyFailed"
		status["error"] = err.Error()
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardInterface", resource.Metadata.Name, status); err != nil {
			return err
		}
		c.savePeerPendingStatuses(resource.Metadata.Name, cfg.Peers, "InterfaceError")
		return nil
	} else {
		applied = true
		observed, statusErr = c.interfaceStatus(ctx, cfg.Name)
	}
	firewallStatus := map[string]any{
		"managedBy": "routerd",
		"protocol":  "udp",
		"port":      cfg.ListenPort,
		"chain":     "INPUT",
	}
	if cfg.ListenPort > 0 {
		if platform.CurrentOS() != platform.OSLinux {
			firewallStatus["phase"] = "NotApplicable"
			firewallStatus["reason"] = "HostFirewallManagedOnLinuxOnly"
		} else if c.DryRun {
			firewallStatus["phase"] = "Planned"
		} else if err := c.ensureWireGuardInputAccept(ctx, cfg.ListenPort); err != nil {
			firewallStatus["phase"] = "Error"
			firewallStatus["lastError"] = err.Error()
			status["hostFirewall"] = firewallStatus
			status["phase"] = "Error"
			status["reason"] = "HostFirewallApplyFailed"
			status["error"] = err.Error()
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardInterface", resource.Metadata.Name, status); err != nil {
				return err
			}
			c.savePeerPendingStatuses(resource.Metadata.Name, cfg.Peers, "InterfaceError")
			return nil
		} else {
			firewallStatus["phase"] = "Applied"
		}
		status["hostFirewall"] = firewallStatus
	}
	status["phase"] = "Up"
	if c.DryRun {
		status["phase"] = "Planned"
	}
	if statusErr == nil {
		if observed.PublicKey != "" {
			status["publicKey"] = observed.PublicKey
		}
		if observed.ListenPort != 0 {
			status["listenPort"] = observed.ListenPort
		}
		if observed.FwMark != "" {
			status["fwmark"] = observed.FwMark
		}
		status["peerCount"] = len(observed.Peers)
		c.savePeerObservedStatuses(resource.Metadata.Name, cfg.Peers, observed.Peers)
	} else if !c.DryRun {
		status["statusError"] = statusErr.Error()
		c.savePeerPendingStatuses(resource.Metadata.Name, cfg.Peers, "StatusUnavailable")
	} else {
		c.savePeerPendingStatuses(resource.Metadata.Name, cfg.Peers, "DryRun")
	}
	if _, ok := status["publicKey"]; !ok {
		if publicKey := wireGuardPublicKeyFromConfig(cfg); publicKey != "" {
			status["publicKey"] = publicKey
		}
	}
	if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardInterface", resource.Metadata.Name, status); err != nil {
		return err
	}
	if applied && c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.wireguard.interface.applied", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface", Name: resource.Metadata.Name}
		event.Attributes = map[string]string{
			"interface": cfg.Name,
			"peers":     fmt.Sprintf("%d", len(cfg.Peers)),
			"dryRun":    fmt.Sprintf("%t", c.DryRun),
		}
		return c.Bus.Publish(ctx, event)
	}
	return nil
}

func wireGuardHostFirewallPort(status map[string]any) int {
	hostFirewall := statusMap(status["hostFirewall"])
	if !strings.EqualFold(statusString(hostFirewall, "managedBy"), "routerd") {
		return 0
	}
	if !strings.EqualFold(statusString(hostFirewall, "protocol"), "udp") {
		return 0
	}
	if !strings.EqualFold(statusString(hostFirewall, "chain"), "INPUT") {
		return 0
	}
	port, ok := statusInt(hostFirewall["port"])
	if !ok || port <= 0 {
		return 0
	}
	return port
}

func (c WireGuardController) selfNodeRef(spec api.WireGuardInterfaceSpec) string {
	if self := strings.TrimSpace(spec.SelfNodeRef); self != "" {
		return self
	}
	if c.Router == nil {
		return ""
	}
	return strings.TrimSpace(c.Router.Metadata.Name)
}

func (c WireGuardController) ensureWireGuardInputAccept(ctx context.Context, port int) error {
	if port <= 0 {
		return nil
	}
	check := []string{"-C", "INPUT", "-p", "udp", "--dport", strconv.Itoa(port), "-j", "ACCEPT"}
	if out, err := c.runWireGuardHostCommand(ctx, "iptables", check...); err == nil {
		return nil
	} else if !wireGuardIPTablesRuleMissing(out, err) {
		return fmt.Errorf("iptables %s: %w: %s", strings.Join(check, " "), err, strings.TrimSpace(string(out)))
	}
	insert := []string{"-I", "INPUT", "1", "-p", "udp", "--dport", strconv.Itoa(port), "-j", "ACCEPT"}
	if out, err := c.runWireGuardHostCommand(ctx, "iptables", insert...); err != nil {
		return fmt.Errorf("iptables %s: %w: %s", strings.Join(insert, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c WireGuardController) deleteWireGuardInputAccept(ctx context.Context, port int) error {
	if port <= 0 || platform.CurrentOS() != platform.OSLinux {
		return nil
	}
	deleteRule := []string{"-D", "INPUT", "-p", "udp", "--dport", strconv.Itoa(port), "-j", "ACCEPT"}
	if out, err := c.runWireGuardHostCommand(ctx, "iptables", deleteRule...); err != nil && !wireGuardIPTablesRuleMissing(out, err) {
		return fmt.Errorf("iptables %s: %w: %s", strings.Join(deleteRule, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c WireGuardController) runWireGuardHostCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	run := c.Command
	if run == nil {
		run = wireguard.DefaultCommandRunner
	}
	return run(ctx, name, args...)
}

func wireGuardIPTablesRuleMissing(out []byte, err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(string(out)) + " " + err.Error())
	return strings.Contains(msg, "bad rule") ||
		strings.Contains(msg, "does a matching rule exist") ||
		strings.Contains(msg, "no chain/target/match") ||
		strings.Contains(msg, "does not exist")
}

func wireGuardPeersFromStatusMaps(statuses []wireGuardPeersFromStatus) []map[string]any {
	out := make([]map[string]any, 0, len(statuses))
	for _, status := range statuses {
		item := map[string]any{
			"resource": status.Resource,
			"phase":    status.Phase,
		}
		if status.Optional {
			item["optional"] = true
		}
		if status.PeerCount > 0 {
			item["peerCount"] = status.PeerCount
		}
		if status.Reason != "" {
			item["reason"] = status.Reason
		}
		out = append(out, item)
	}
	return out
}

func (c WireGuardController) saveUnconfiguredPeerStatuses(iface string) error {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "WireGuardPeer" {
			continue
		}
		spec, err := resource.WireGuardPeerSpec()
		if err != nil {
			return err
		}
		if spec.Interface != iface || wireguard.PeerSpecConfigured(spec) {
			continue
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardPeer", resource.Metadata.Name, map[string]any{
			"phase":     "NotConfigured",
			"reason":    "PeerSpecEmpty",
			"interface": iface,
			"ifname":    interfaceIfName(c.Router, iface),
			"dryRun":    c.DryRun,
		}); err != nil {
			return err
		}
	}
	return nil
}

func wireGuardConfigHash(cfg wireguard.InterfaceConfig, dryRun bool) string {
	resolved, err := wireguard.ResolveKeyFiles(cfg)
	if err != nil {
		if !dryRun {
			return ""
		}
		resolved = cfg
		if resolved.PrivateKey == "" && resolved.PrivateKeyFile != "" {
			resolved.PrivateKey = "REDACTED_FROM_FILE"
		}
		for i := range resolved.Peers {
			if resolved.Peers[i].PresharedKey == "" && resolved.Peers[i].PresharedKeyFile != "" {
				resolved.Peers[i].PresharedKey = "REDACTED_FROM_FILE"
			}
		}
	}
	conf, err := wireguard.RenderSetConf(resolved)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(conf)
	return hex.EncodeToString(sum[:])
}

func wireGuardPublicKeyFromConfig(cfg wireguard.InterfaceConfig) string {
	resolved, err := wireguard.ResolveKeyFiles(cfg)
	if err != nil {
		return ""
	}
	publicKey, err := wireguard.PublicKeyFromPrivateKey(resolved.PrivateKey)
	if err != nil {
		return ""
	}
	return publicKey
}

func (c WireGuardController) interfaceMatchesDesired(ctx context.Context, cfg wireguard.InterfaceConfig, observed wireguard.InterfaceStatus) bool {
	if cfg.ListenPort != 0 && observed.ListenPort != cfg.ListenPort {
		return false
	}
	if cfg.FwMark != 0 && !fwmarkMatches(observed.FwMark, cfg.FwMark) {
		return false
	}
	if cfg.MTU != 0 && !c.linkMTUMatches(ctx, cfg.Name, cfg.MTU) {
		return false
	}
	byKey := map[string]wireguard.PeerStatus{}
	for _, peer := range observed.Peers {
		byKey[peer.PublicKey] = peer
	}
	if len(byKey) != len(cfg.Peers) {
		return false
	}
	for _, desired := range cfg.Peers {
		current, ok := byKey[desired.PublicKey]
		if !ok {
			return false
		}
		if !stringSetEqual(desired.AllowedIPs, current.AllowedIPs) {
			return false
		}
		if desired.PersistentKeepalive != current.PersistentKeepalive {
			return false
		}
	}
	return true
}

func (c WireGuardController) linkMTUMatches(ctx context.Context, ifname string, mtu int) bool {
	run := c.Command
	if run == nil {
		run = wireguard.DefaultCommandRunner
	}
	out, err := run(ctx, "ip", "-o", "link", "show", "dev", ifname)
	if err != nil {
		return false
	}
	fields := strings.Fields(string(out))
	for i, field := range fields {
		if field == "mtu" && i+1 < len(fields) {
			got, _ := strconv.Atoi(fields[i+1])
			return got == mtu
		}
	}
	return false
}

func fwmarkMatches(current string, desired int) bool {
	current = strings.TrimSpace(strings.ToLower(current))
	if current == "" {
		return desired == 0
	}
	return current == fmt.Sprintf("0x%x", desired) || current == fmt.Sprintf("%d", desired)
}

func stringSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	left := append([]string(nil), a...)
	right := append([]string(nil), b...)
	sort.Strings(left)
	sort.Strings(right)
	for i := range left {
		if strings.TrimSpace(left[i]) != strings.TrimSpace(right[i]) {
			return false
		}
	}
	return true
}

func (c WireGuardController) interfaceStatus(ctx context.Context, ifname string) (wireguard.InterfaceStatus, error) {
	run := c.Command
	if run == nil {
		run = wireguard.DefaultCommandRunner
	}
	out, err := run(ctx, "wg", "show", ifname, "dump")
	if err != nil {
		return wireguard.InterfaceStatus{Name: ifname}, err
	}
	return wireguard.ParseInterfaceDump(ifname, out)
}

func (c WireGuardController) savePeerPendingStatuses(iface string, peers []wireguard.PeerConfig, reason string) {
	ifname := interfaceIfName(c.Router, iface)
	for _, peer := range peers {
		_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardPeer", peer.Name, map[string]any{
			"phase":      "Pending",
			"reason":     reason,
			"interface":  iface,
			"ifname":     ifname,
			"publicKey":  peer.PublicKey,
			"allowedIPs": append([]string(nil), peer.AllowedIPs...),
			"endpoint":   peer.Endpoint,
			"dryRun":     c.DryRun,
		})
	}
}

func (c WireGuardController) savePeerObservedStatuses(iface string, desired []wireguard.PeerConfig, observed []wireguard.PeerStatus) {
	ifname := interfaceIfName(c.Router, iface)
	byKey := map[string]wireguard.PeerStatus{}
	for _, peer := range observed {
		byKey[peer.PublicKey] = peer
	}
	for _, peer := range desired {
		status := map[string]any{
			"phase":               "Configured",
			"interface":           iface,
			"ifname":              ifname,
			"publicKey":           peer.PublicKey,
			"allowedIPs":          append([]string(nil), peer.AllowedIPs...),
			"endpoint":            peer.Endpoint,
			"persistentKeepalive": peer.PersistentKeepalive,
			"dryRun":              c.DryRun,
		}
		if got, ok := byKey[peer.PublicKey]; ok {
			if got.LatestEndpoint != "" {
				status["latestEndpoint"] = got.LatestEndpoint
			}
			if !got.LatestHandshake.IsZero() {
				status["phase"] = "Connected"
				status["latestHandshake"] = got.LatestHandshake.Format(time.RFC3339)
				status["handshakeAgeSeconds"] = int(time.Since(got.LatestHandshake).Seconds())
			} else if strings.TrimSpace(peer.Endpoint) != "" {
				status["phase"] = "Waiting"
				status["reason"] = "NoHandshakeYet"
			}
			status["transferRxBytes"] = got.TransferRxBytes
			status["transferTxBytes"] = got.TransferTxBytes
		} else {
			status["phase"] = "Pending"
			status["reason"] = "PeerNotObserved"
		}
		_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardPeer", peer.Name, status)
	}
}
