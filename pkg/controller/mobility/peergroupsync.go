// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	neturl "net/url"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"github.com/imksoo/routerd/pkg/wireguard"
)

const (
	PeerGroupSyncPort         = 19652
	peerGroupSyncPath         = "/v1/peer-groups"
	peerGroupSyncResourceKind = "SAMPeerGroup"
	peerGroupSyncCacheKind    = "peer-group-sync"
)

func PeerGroupSyncDynamicSource(groupName string) string {
	return peerGroupSyncCacheKind + "/" + strings.TrimSpace(groupName)
}

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

type PeerGroupSyncServer struct {
	Store peerGroupPartStore
	Now   func() time.Time
}

func NewPeerGroupSyncServer(store peerGroupPartStore) *PeerGroupSyncServer {
	return &PeerGroupSyncServer{Store: store}
}

func (s *PeerGroupSyncServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != peerGroupSyncPath {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	resources, err := s.syncResources()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resources = filterSyncResources(resources, r.URL.Query().Get("name"))
	_ = json.NewEncoder(w).Encode(PeerGroupSyncResponse{PeerGroups: resources})
}

func resourceDigest(resource api.Resource) string {
	data, _ := json.Marshal(resource)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum[:])
}

func filterSyncResources(resources []api.Resource, name string) []api.Resource {
	name = strings.TrimSpace(name)
	if name == "" {
		return resources
	}
	out := make([]api.Resource, 0, 1)
	for _, resource := range resources {
		if resource.Metadata.Name == name {
			out = append(out, resource)
		}
	}
	return out
}

func stampSourceGeneration(resource *api.Resource, record routerstate.DynamicConfigPartRecord) {
	if resource.Metadata.Annotations == nil {
		resource.Metadata.Annotations = map[string]string{}
	}
	resource.Metadata.Annotations["routerd.net/source-generation"] = strconv.FormatInt(record.Generation, 10)
	if !record.ExpiresAt.IsZero() {
		resource.Metadata.Annotations["routerd.net/source-valid-until"] = record.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
}

func (s *PeerGroupSyncServer) syncResources() ([]api.Resource, error) {
	if s == nil || s.Store == nil {
		return nil, nil
	}
	now := controllerNow(s.Now)
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
		resources, err := codec.DecodeGenericResources(record)
		if err != nil {
			return nil, fmt.Errorf("decode %s dynamic resources from %s: %w", peerGroupSyncResourceKind, record.Source, err)
		}
		for _, resource := range resources {
			if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == peerGroupSyncResourceKind {
				stampSourceGeneration(&resource, record)
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

type syncMetadata struct {
	PublisherID string
	Revision    int64
	ValidUntil  time.Time
	Digest      string
}

type syncCandidate struct {
	resource api.Resource
	meta     syncMetadata
}

func syncMetadataForResource(resource api.Resource, publisherID string) syncMetadata {
	meta := syncMetadata{
		PublisherID: strings.TrimSpace(publisherID),
		Digest:      resourceDigest(resource),
	}
	if resource.Metadata.Annotations == nil {
		return meta
	}
	meta.Revision, _ = strconv.ParseInt(resource.Metadata.Annotations["routerd.net/source-generation"], 10, 64)
	meta.ValidUntil, _ = time.Parse(time.RFC3339Nano, resource.Metadata.Annotations["routerd.net/source-valid-until"])
	return meta
}

func selectSyncCandidate(candidates []syncCandidate, now time.Time) (syncCandidate, error) {
	active := candidates[:0]
	for _, candidate := range candidates {
		if !candidate.meta.ValidUntil.IsZero() && !candidate.meta.ValidUntil.After(now) {
			continue
		}
		if candidate.meta.Digest == "" {
			candidate.meta.Digest = resourceDigest(candidate.resource)
		}
		active = append(active, candidate)
	}
	if len(active) == 0 {
		return syncCandidate{}, fmt.Errorf("no non-expired sync response")
	}
	sort.Slice(active, func(i, j int) bool {
		if active[i].meta.Revision != active[j].meta.Revision {
			return active[i].meta.Revision > active[j].meta.Revision
		}
		return active[i].meta.PublisherID < active[j].meta.PublisherID
	})
	for _, candidate := range active[1:] {
		if candidate.meta.Revision != active[0].meta.Revision {
			break
		}
		if candidate.meta.Digest != active[0].meta.Digest {
			return syncCandidate{}, fmt.Errorf("peer sync conflict at revision %d: digest %s from %s differs from %s from %s",
				active[0].meta.Revision, active[0].meta.Digest, active[0].meta.PublisherID, candidate.meta.Digest, candidate.meta.PublisherID)
		}
	}
	return active[0], nil
}

func NewPeerGroupSyncClient(store peerGroupSyncStore) *PeerGroupSyncClient {
	return &PeerGroupSyncClient{
		Store:      store,
		HTTPClient: &http.Client{Timeout: 2 * time.Second},
		Discover:   DiscoverWireGuardPeerGroupSyncEndpoints,
		Port:       PeerGroupSyncPort,
	}
}

func (c *PeerGroupSyncClient) SyncPeerGroup(ctx context.Context, router *api.Router, underlayInterface, groupName string) (api.SAMPeerGroupSpec, bool, error) {
	resource, metadata, found, err := c.syncResource(ctx, router, underlayInterface, groupName)
	if err != nil || !found {
		return api.SAMPeerGroupSpec{}, false, err
	}
	spec, err := resource.SAMPeerGroupSpec()
	if err != nil {
		return api.SAMPeerGroupSpec{}, false, err
	}
	if err := c.saveSyncResource(groupName, resource, metadata); err != nil {
		return api.SAMPeerGroupSpec{}, false, err
	}
	return spec, true, nil
}

type syncFetchResult struct {
	resource api.Resource
	meta     syncMetadata
	found    bool
	err      error
}

func (c *PeerGroupSyncClient) syncResource(ctx context.Context, router *api.Router, underlayInterface, name string) (api.Resource, syncMetadata, bool, error) {
	name = strings.TrimSpace(name)
	if c == nil || c.Store == nil || name == "" {
		return api.Resource{}, syncMetadata{}, false, nil
	}
	discover := c.Discover
	if discover == nil {
		discover = DiscoverWireGuardPeerGroupSyncEndpoints
	}
	endpoints, err := discover(ctx, router, underlayInterface)
	if err != nil {
		return api.Resource{}, syncMetadata{}, false, err
	}
	if len(endpoints) == 0 {
		return api.Resource{}, syncMetadata{}, false, nil
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	port := c.Port
	if port == 0 {
		port = PeerGroupSyncPort
	}
	results := make(chan syncFetchResult, len(endpoints))
	var wg sync.WaitGroup
	for _, endpoint := range endpoints {
		wg.Add(1)
		go func(endpoint netip.Addr) {
			defer wg.Done()
			resource, metadata, found, fetchErr := fetchSyncResourceFromEndpoint(ctx, client, endpoint, port, name)
			if metadata.PublisherID == "" {
				metadata.PublisherID = endpoint.String()
			}
			results <- syncFetchResult{resource: resource, meta: metadata, found: found, err: fetchErr}
		}(endpoint)
	}
	wg.Wait()
	close(results)

	var firstErr error
	var candidates []syncCandidate
	for result := range results {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		if result.found {
			candidates = append(candidates, syncCandidate{resource: result.resource, meta: result.meta})
		}
	}
	if len(candidates) == 0 {
		return api.Resource{}, syncMetadata{}, false, firstErr
	}
	selected, err := selectSyncCandidate(candidates, controllerNow(c.Now))
	if err != nil {
		return api.Resource{}, syncMetadata{}, false, err
	}
	return selected.resource, selected.meta, true, nil
}

func fetchSyncResourceFromEndpoint(ctx context.Context, client *http.Client, endpoint netip.Addr, port int, name string) (api.Resource, syncMetadata, bool, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	url := "http://" + net.JoinHostPort(endpoint.String(), strconv.Itoa(port)) + peerGroupSyncPath + "?name=" + neturl.QueryEscape(name)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return api.Resource{}, syncMetadata{}, false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return api.Resource{}, syncMetadata{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<12))
		return api.Resource{}, syncMetadata{}, false, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	var payload PeerGroupSyncResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return api.Resource{}, syncMetadata{}, false, err
	}
	for _, resource := range payload.PeerGroups {
		if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == peerGroupSyncResourceKind && resource.Metadata.Name == name {
			return resource, syncMetadataForResource(resource, endpoint.String()), true, nil
		}
	}
	return api.Resource{}, syncMetadata{PublisherID: endpoint.String()}, false, nil
}

func (c *PeerGroupSyncClient) saveSyncResource(name string, resource api.Resource, meta syncMetadata) error {
	now := controllerNow(c.Now)
	resource.TypeMeta = api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: peerGroupSyncResourceKind}
	resource.Metadata.Name = strings.TrimSpace(name)
	expiresAt := meta.ValidUntil
	if expiresAt.IsZero() || !expiresAt.After(now) {
		expiresAt = now.Add(DefaultLeaseTTL)
	}
	part := dynamicconfig.NewPart(safeName(peerGroupSyncCacheKind+"-"+strings.TrimSpace(name)), PeerGroupSyncDynamicSource(name), nil, dynamicGeneration, now, expiresAt)
	part.Spec.Resources = []api.Resource{resource}
	part.Spec.ActionPlans = []dynamicconfig.ActionPlan{}
	part.Spec.Digest = digestDynamicPart(part)
	record, err := codec.Encode(part)
	if err != nil {
		return err
	}
	return c.Store.UpsertDynamicConfigPart(record)
}

func DiscoverWireGuardPeerGroupSyncEndpoints(ctx context.Context, router *api.Router, underlayInterface string) ([]netip.Addr, error) {
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
		resources, err := codec.DecodeGenericResources(record)
		if err != nil {
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
