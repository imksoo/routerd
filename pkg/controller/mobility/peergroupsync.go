// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"github.com/imksoo/routerd/pkg/wireguard"
)

const (
	PeerGroupSyncPort = 19652
	peerGroupSyncPath = "/v1/peer-groups"
	memberSetSyncPath = "/v1/member-sets"
)

type peerGroupPartStore interface {
	ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error)
}

type peerGroupSyncStore interface {
	UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord) error
}

type dynamicConfigSourceStore interface {
	GetDynamicConfigPartsBySource(string) ([]routerstate.DynamicConfigPartRecord, error)
}

type PeerGroupSyncResponse struct {
	PeerGroups []api.Resource `json:"peerGroups"`
}

type MemberSetSyncResponse struct {
	MemberSets []api.Resource `json:"memberSets"`
}

type PeerGroupSyncServer struct {
	Store peerGroupPartStore
	Now   func() time.Time
}

func NewPeerGroupSyncServer(store peerGroupPartStore) *PeerGroupSyncServer {
	return &PeerGroupSyncServer{Store: store}
}

func (s *PeerGroupSyncServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case peerGroupSyncPath, memberSetSyncPath:
	default:
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case peerGroupSyncPath:
		groups, err := s.PeerGroups()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(PeerGroupSyncResponse{PeerGroups: groups})
	case memberSetSyncPath:
		sets, err := s.MemberSets()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(MemberSetSyncResponse{MemberSets: sets})
	}
}

func (s *PeerGroupSyncServer) PeerGroups() ([]api.Resource, error) {
	if s == nil || s.Store == nil {
		return nil, nil
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	records, err := s.Store.ListDynamicConfigParts()
	if err != nil {
		return nil, err
	}
	var out []api.Resource
	for _, record := range records {
		if _, ok := parseTransportPeerGroupSource(record.Source); !ok {
			continue
		}
		if record.EffectiveStatus(now) != "active" {
			continue
		}
		var resources []api.Resource
		if err := json.Unmarshal([]byte(record.ResourcesJSON), &resources); err != nil {
			return nil, fmt.Errorf("decode peer group dynamic resources from %s: %w", record.Source, err)
		}
		for _, resource := range resources {
			if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "SAMPeerGroup" {
				out = append(out, resource)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Metadata.Name < out[j].Metadata.Name })
	return out, nil
}

func (s *PeerGroupSyncServer) MemberSets() ([]api.Resource, error) {
	if s == nil || s.Store == nil {
		return nil, nil
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	records, err := s.Store.ListDynamicConfigParts()
	if err != nil {
		return nil, err
	}
	var out []api.Resource
	for _, record := range records {
		if _, ok := parseMobilityMemberSetSource(record.Source); !ok {
			continue
		}
		if record.EffectiveStatus(now) != "active" {
			continue
		}
		var resources []api.Resource
		if err := json.Unmarshal([]byte(record.ResourcesJSON), &resources); err != nil {
			return nil, fmt.Errorf("decode member set dynamic resources from %s: %w", record.Source, err)
		}
		for _, resource := range resources {
			if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "MobilityMemberSet" {
				out = append(out, resource)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Metadata.Name < out[j].Metadata.Name })
	return out, nil
}

type PeerGroupEndpointDiscovery func(ctx context.Context, router *api.Router, underlayInterface string) ([]netip.Addr, error)

type PeerGroupSyncClient struct {
	Store      peerGroupSyncStore
	HTTPClient *http.Client
	Discover   PeerGroupEndpointDiscovery
	Port       int
	Now        func() time.Time
}

func NewPeerGroupSyncClient(store peerGroupSyncStore) *PeerGroupSyncClient {
	return &PeerGroupSyncClient{
		Store:      store,
		HTTPClient: &http.Client{Timeout: 2 * time.Second},
		Discover:   DiscoverWireGuardPeerGroupSyncEndpoints,
		Port:       PeerGroupSyncPort,
	}
}

func PeerGroupSyncDynamicSource(groupName string) string {
	return "peer-group-sync/" + strings.TrimSpace(groupName)
}

func MemberSetSyncDynamicSource(setName string) string {
	return "member-set-sync/" + strings.TrimSpace(setName)
}

func (c *PeerGroupSyncClient) SyncPeerGroup(ctx context.Context, router *api.Router, underlayInterface, groupName string) (api.SAMPeerGroupSpec, bool, error) {
	groupName = strings.TrimSpace(groupName)
	if c == nil || c.Store == nil || groupName == "" {
		return api.SAMPeerGroupSpec{}, false, nil
	}
	discover := c.Discover
	if discover == nil {
		discover = DiscoverWireGuardPeerGroupSyncEndpoints
	}
	endpoints, err := discover(ctx, router, underlayInterface)
	if err != nil {
		return api.SAMPeerGroupSpec{}, false, err
	}
	if len(endpoints) == 0 {
		return api.SAMPeerGroupSpec{}, false, nil
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	port := c.Port
	if port == 0 {
		port = PeerGroupSyncPort
	}
	type result struct {
		resource api.Resource
		found    bool
		err      error
	}
	results := make(chan result, len(endpoints))
	var wg sync.WaitGroup
	for _, endpoint := range endpoints {
		endpoint := endpoint
		wg.Add(1)
		go func() {
			defer wg.Done()
			resource, found, err := fetchPeerGroupFromEndpoint(ctx, client, endpoint, port, groupName)
			results <- result{resource: resource, found: found, err: err}
		}()
	}
	wg.Wait()
	close(results)
	var firstErr error
	for res := range results {
		if res.err != nil && firstErr == nil {
			firstErr = res.err
			continue
		}
		if !res.found {
			continue
		}
		spec, err := res.resource.SAMPeerGroupSpec()
		if err != nil {
			return api.SAMPeerGroupSpec{}, false, err
		}
		if err := c.savePeerGroup(ctx, groupName, res.resource); err != nil {
			return api.SAMPeerGroupSpec{}, false, err
		}
		return spec, true, nil
	}
	return api.SAMPeerGroupSpec{}, false, firstErr
}

func (c *PeerGroupSyncClient) SyncMemberSet(ctx context.Context, router *api.Router, setName string) (api.MobilityMemberSetSpec, bool, error) {
	setName = strings.TrimSpace(setName)
	if c == nil || c.Store == nil || setName == "" {
		return api.MobilityMemberSetSpec{}, false, nil
	}
	discover := c.Discover
	if discover == nil {
		discover = DiscoverWireGuardPeerGroupSyncEndpoints
	}
	endpoints, err := discover(ctx, router, "")
	if err != nil {
		return api.MobilityMemberSetSpec{}, false, err
	}
	if len(endpoints) == 0 {
		return api.MobilityMemberSetSpec{}, false, nil
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	port := c.Port
	if port == 0 {
		port = PeerGroupSyncPort
	}
	type result struct {
		resource api.Resource
		found    bool
		err      error
	}
	results := make(chan result, len(endpoints))
	var wg sync.WaitGroup
	for _, endpoint := range endpoints {
		endpoint := endpoint
		wg.Add(1)
		go func() {
			defer wg.Done()
			resource, found, err := fetchMemberSetFromEndpoint(ctx, client, endpoint, port, setName)
			results <- result{resource: resource, found: found, err: err}
		}()
	}
	wg.Wait()
	close(results)
	var firstErr error
	for res := range results {
		if res.err != nil && firstErr == nil {
			firstErr = res.err
			continue
		}
		if !res.found {
			continue
		}
		spec, err := res.resource.MobilityMemberSetSpec()
		if err != nil {
			return api.MobilityMemberSetSpec{}, false, err
		}
		if err := c.saveMemberSet(ctx, setName, res.resource); err != nil {
			return api.MobilityMemberSetSpec{}, false, err
		}
		return spec, true, nil
	}
	return api.MobilityMemberSetSpec{}, false, firstErr
}

func fetchPeerGroupFromEndpoint(ctx context.Context, client *http.Client, endpoint netip.Addr, port int, groupName string) (api.Resource, bool, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	url := "http://" + net.JoinHostPort(endpoint.String(), strconv.Itoa(port)) + peerGroupSyncPath
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return api.Resource{}, false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return api.Resource{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<12))
		return api.Resource{}, false, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	var payload PeerGroupSyncResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return api.Resource{}, false, err
	}
	for _, resource := range payload.PeerGroups {
		if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "SAMPeerGroup" && resource.Metadata.Name == groupName {
			return resource, true, nil
		}
	}
	return api.Resource{}, false, nil
}

func fetchMemberSetFromEndpoint(ctx context.Context, client *http.Client, endpoint netip.Addr, port int, setName string) (api.Resource, bool, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	url := "http://" + net.JoinHostPort(endpoint.String(), strconv.Itoa(port)) + memberSetSyncPath
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return api.Resource{}, false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return api.Resource{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<12))
		return api.Resource{}, false, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	var payload MemberSetSyncResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return api.Resource{}, false, err
	}
	for _, resource := range payload.MemberSets {
		if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "MobilityMemberSet" && resource.Metadata.Name == setName {
			return resource, true, nil
		}
	}
	return api.Resource{}, false, nil
}

func (c *PeerGroupSyncClient) savePeerGroup(_ context.Context, groupName string, resource api.Resource) error {
	now := time.Now().UTC()
	if c.Now != nil {
		now = c.Now().UTC()
	}
	resource.TypeMeta = api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"}
	resource.Metadata.Name = strings.TrimSpace(groupName)
	part := dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{
			Name: safeName("peer-group-sync-" + groupName),
		},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:      PeerGroupSyncDynamicSource(groupName),
			Generation:  dynamicGeneration,
			ObservedAt:  now,
			ExpiresAt:   now.Add(DefaultLeaseTTL),
			Resources:   []api.Resource{resource},
			Directives:  []dynamicconfig.DynamicConfigDirective{},
			ActionPlans: []dynamicconfig.ActionPlan{},
		},
	}
	part.Spec.Digest = digestDynamicPart(part)
	record, err := dynamicPartRecord(part)
	if err != nil {
		return err
	}
	return c.Store.UpsertDynamicConfigPart(record)
}

func (c *PeerGroupSyncClient) saveMemberSet(_ context.Context, setName string, resource api.Resource) error {
	now := time.Now().UTC()
	if c.Now != nil {
		now = c.Now().UTC()
	}
	resource.TypeMeta = api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityMemberSet"}
	resource.Metadata.Name = strings.TrimSpace(setName)
	part := dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{
			Name: safeName("member-set-sync-" + setName),
		},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:      MemberSetSyncDynamicSource(setName),
			Generation:  dynamicGeneration,
			ObservedAt:  now,
			ExpiresAt:   now.Add(DefaultLeaseTTL),
			Resources:   []api.Resource{resource},
			Directives:  []dynamicconfig.DynamicConfigDirective{},
			ActionPlans: []dynamicconfig.ActionPlan{},
		},
	}
	part.Spec.Digest = digestDynamicPart(part)
	record, err := dynamicPartRecord(part)
	if err != nil {
		return err
	}
	return c.Store.UpsertDynamicConfigPart(record)
}

func DiscoverWireGuardPeerGroupSyncEndpoints(ctx context.Context, router *api.Router, underlayInterface string) ([]netip.Addr, error) {
	if strings.TrimSpace(underlayInterface) == "" && router != nil {
		seen := map[netip.Addr]bool{}
		var out []netip.Addr
		for _, resource := range router.Spec.Resources {
			if resource.APIVersion != api.NetAPIVersion || resource.Kind != "WireGuardInterface" {
				continue
			}
			name := resource.Metadata.Name
			if spec, err := resource.WireGuardInterfaceSpec(); err == nil {
				name = firstNonEmpty(strings.TrimSpace(spec.IfName), name)
			}
			addrs, err := DiscoverWireGuardPeerGroupSyncEndpoints(ctx, router, name)
			if err != nil {
				return nil, err
			}
			for _, addr := range addrs {
				if !seen[addr] {
					seen[addr] = true
					out = append(out, addr)
				}
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
		return out, nil
	}
	iface := wireGuardInterfaceName(router, underlayInterface)
	if strings.TrimSpace(iface) == "" {
		iface = strings.TrimSpace(underlayInterface)
	}
	if iface == "" {
		return nil, nil
	}
	out, err := exec.CommandContext(ctx, "wg", "show", iface, "dump").Output()
	if err != nil {
		return nil, fmt.Errorf("wg show %s dump: %w", iface, err)
	}
	status, err := wireguard.ParseInterfaceDump(iface, out)
	if err != nil {
		return nil, err
	}
	return firstAllowedIPAddrs(status.Peers), nil
}

func wireGuardInterfaceName(router *api.Router, underlayInterface string) string {
	name := strings.TrimSpace(underlayInterface)
	if router == nil || name == "" {
		return name
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "WireGuardInterface" {
			continue
		}
		spec, err := resource.WireGuardInterfaceSpec()
		if err != nil {
			continue
		}
		if resource.Metadata.Name == name || strings.TrimSpace(spec.IfName) == name {
			return firstNonEmpty(strings.TrimSpace(spec.IfName), resource.Metadata.Name)
		}
	}
	return name
}

func firstAllowedIPAddrs(peers []wireguard.PeerStatus) []netip.Addr {
	seen := map[netip.Addr]bool{}
	var out []netip.Addr
	for _, peer := range peers {
		if addr, ok := firstAllowedIPAddr(peer.AllowedIPs); ok && !seen[addr] {
			seen[addr] = true
			out = append(out, addr)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func firstAllowedIPAddr(allowedIPs []string) (netip.Addr, bool) {
	for _, raw := range allowedIPs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		if prefix.Addr().Is4() && prefix.Bits() == 32 {
			return prefix.Addr(), true
		}
	}
	for _, raw := range allowedIPs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		if prefix.Addr().Is6() && prefix.Bits() == 128 {
			return prefix.Addr(), true
		}
	}
	return netip.Addr{}, false
}

func HasPublishedPeerGroups(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMTransportProfile" {
			continue
		}
		spec, err := resource.SAMTransportProfileSpec()
		if err == nil && spec.PublishPeerGroup {
			return true
		}
	}
	return false
}

func HasPublishedMemberSets(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "MobilityPool" {
			continue
		}
		spec, err := resource.MobilityPoolSpec()
		if err == nil && spec.PublishMemberSet {
			return true
		}
	}
	return false
}

func latestSyncedMobilityResource(store dynamicConfigSourceStore, source, kind, name string, now time.Time) (api.Resource, string, bool, error) {
	if store == nil || strings.TrimSpace(source) == "" || strings.TrimSpace(kind) == "" || strings.TrimSpace(name) == "" {
		return api.Resource{}, "", false, nil
	}
	records, err := store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		return api.Resource{}, "", false, err
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].ObservedAt.Equal(records[j].ObservedAt) {
			return records[i].ID > records[j].ID
		}
		return records[i].ObservedAt.After(records[j].ObservedAt)
	})
	for _, record := range records {
		var resources []api.Resource
		if err := json.Unmarshal([]byte(record.ResourcesJSON), &resources); err != nil {
			return api.Resource{}, "", false, fmt.Errorf("decode last-known-good %s dynamic resources from %s: %w", kind, source, err)
		}
		for _, resource := range resources {
			if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == kind && resource.Metadata.Name == name {
				return resource, record.EffectiveStatus(now), true, nil
			}
		}
	}
	return api.Resource{}, "", false, nil
}
