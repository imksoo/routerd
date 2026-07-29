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
	"os"
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
	PublisherID    string         `json:"publisherID,omitempty"`
	Revision       int64          `json:"revision,omitempty"`
	GeneratedAt    time.Time      `json:"generatedAt,omitempty"`
	ValidUntil     time.Time      `json:"validUntil,omitempty"`
	ResourceDigest string         `json:"resourceDigest,omitempty"`
	PeerGroups     []api.Resource `json:"peerGroups"`
}

type MemberSetSyncResponse struct {
	PublisherID    string         `json:"publisherID,omitempty"`
	Revision       int64          `json:"revision,omitempty"`
	GeneratedAt    time.Time      `json:"generatedAt,omitempty"`
	ValidUntil     time.Time      `json:"validUntil,omitempty"`
	ResourceDigest string         `json:"resourceDigest,omitempty"`
	MemberSets     []api.Resource `json:"memberSets"`
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
		groups = filterSyncResources(groups, r.URL.Query().Get("name"))
		publisher, _ := os.Hostname()
		_ = json.NewEncoder(w).Encode(PeerGroupSyncResponse{PublisherID: publisher, Revision: semanticRevision(groups), GeneratedAt: s.now(), ValidUntil: semanticValidUntil(groups, s.now()), ResourceDigest: resourceSetDigest(groups), PeerGroups: groups})
	case memberSetSyncPath:
		sets, err := s.MemberSets()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sets = filterSyncResources(sets, r.URL.Query().Get("name"))
		publisher, _ := os.Hostname()
		_ = json.NewEncoder(w).Encode(MemberSetSyncResponse{PublisherID: publisher, Revision: semanticRevision(sets), GeneratedAt: s.now(), ValidUntil: semanticValidUntil(sets, s.now()), ResourceDigest: resourceSetDigest(sets), MemberSets: sets})
	}
}

func (s *PeerGroupSyncServer) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func resourceSetDigest(resources []api.Resource) string {
	data, _ := json.Marshal(resources)
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

func semanticRevision(resources []api.Resource) int64 {
	var revision int64
	for _, resource := range resources {
		value, _ := strconv.ParseInt(resource.Metadata.Annotations["routerd.net/source-generation"], 10, 64)
		if value > revision {
			revision = value
		}
	}
	return revision
}

func semanticValidUntil(resources []api.Resource, fallback time.Time) time.Time {
	validUntil := fallback.Add(DefaultLeaseTTL)
	for _, resource := range resources {
		value, err := time.Parse(time.RFC3339Nano, resource.Metadata.Annotations["routerd.net/source-valid-until"])
		if err == nil && value.Before(validUntil) {
			validUntil = value
		}
	}
	return validUntil
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
				stampSourceGeneration(&resource, record)
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

func selectSyncCandidate(candidates []syncCandidate, now time.Time) (syncCandidate, error) {
	active := candidates[:0]
	for _, candidate := range candidates {
		if !candidate.meta.ValidUntil.IsZero() && !candidate.meta.ValidUntil.After(now) {
			continue
		}
		if candidate.meta.Digest == "" {
			candidate.meta.Digest = resourceSetDigest([]api.Resource{candidate.resource})
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
		meta     syncMetadata
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
			resource, meta, found, err := fetchPeerGroupFromEndpoint(ctx, client, endpoint, port, groupName)
			if meta.PublisherID == "" {
				meta.PublisherID = endpoint.String()
			}
			results <- result{resource: resource, meta: meta, found: found, err: err}
		}()
	}
	wg.Wait()
	close(results)
	var firstErr error
	var candidates []syncCandidate
	for res := range results {
		if res.err != nil && firstErr == nil {
			firstErr = res.err
			continue
		}
		if !res.found {
			continue
		}
		candidates = append(candidates, syncCandidate{resource: res.resource, meta: res.meta})
	}
	if len(candidates) > 0 {
		selected, err := selectSyncCandidate(candidates, c.now())
		if err != nil {
			return api.SAMPeerGroupSpec{}, false, err
		}
		stampSyncProvenance(&selected.resource, selected.meta)
		spec, err := selected.resource.SAMPeerGroupSpec()
		if err != nil {
			return api.SAMPeerGroupSpec{}, false, err
		}
		if err := c.savePeerGroup(ctx, groupName, selected.resource, selected.meta); err != nil {
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
		meta     syncMetadata
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
			resource, meta, found, err := fetchMemberSetFromEndpoint(ctx, client, endpoint, port, setName)
			if meta.PublisherID == "" {
				meta.PublisherID = endpoint.String()
			}
			results <- result{resource: resource, meta: meta, found: found, err: err}
		}()
	}
	wg.Wait()
	close(results)
	var firstErr error
	var candidates []syncCandidate
	for res := range results {
		if res.err != nil && firstErr == nil {
			firstErr = res.err
			continue
		}
		if !res.found {
			continue
		}
		candidates = append(candidates, syncCandidate{resource: res.resource, meta: res.meta})
	}
	if len(candidates) > 0 {
		selected, err := selectSyncCandidate(candidates, c.now())
		if err != nil {
			return api.MobilityMemberSetSpec{}, false, err
		}
		stampSyncProvenance(&selected.resource, selected.meta)
		spec, err := selected.resource.MobilityMemberSetSpec()
		if err != nil {
			return api.MobilityMemberSetSpec{}, false, err
		}
		if err := c.saveMemberSet(ctx, setName, selected.resource, selected.meta); err != nil {
			return api.MobilityMemberSetSpec{}, false, err
		}
		return spec, true, nil
	}
	return api.MobilityMemberSetSpec{}, false, firstErr
}

func fetchPeerGroupFromEndpoint(ctx context.Context, client *http.Client, endpoint netip.Addr, port int, groupName string) (api.Resource, syncMetadata, bool, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	url := "http://" + net.JoinHostPort(endpoint.String(), strconv.Itoa(port)) + peerGroupSyncPath + "?name=" + neturl.QueryEscape(groupName)
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
	meta := syncMetadata{PublisherID: payload.PublisherID, Revision: payload.Revision, ValidUntil: payload.ValidUntil, Digest: payload.ResourceDigest}
	if payload.ResourceDigest != "" && payload.ResourceDigest != resourceSetDigest(payload.PeerGroups) {
		return api.Resource{}, meta, false, fmt.Errorf("GET %s: resource digest mismatch", url)
	}
	for _, resource := range payload.PeerGroups {
		if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "SAMPeerGroup" && resource.Metadata.Name == groupName {
			return resource, meta, true, nil
		}
	}
	return api.Resource{}, meta, false, nil
}

func fetchMemberSetFromEndpoint(ctx context.Context, client *http.Client, endpoint netip.Addr, port int, setName string) (api.Resource, syncMetadata, bool, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	url := "http://" + net.JoinHostPort(endpoint.String(), strconv.Itoa(port)) + memberSetSyncPath + "?name=" + neturl.QueryEscape(setName)
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
	var payload MemberSetSyncResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return api.Resource{}, syncMetadata{}, false, err
	}
	meta := syncMetadata{PublisherID: payload.PublisherID, Revision: payload.Revision, ValidUntil: payload.ValidUntil, Digest: payload.ResourceDigest}
	if payload.ResourceDigest != "" && payload.ResourceDigest != resourceSetDigest(payload.MemberSets) {
		return api.Resource{}, meta, false, fmt.Errorf("GET %s: resource digest mismatch", url)
	}
	for _, resource := range payload.MemberSets {
		if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "MobilityMemberSet" && resource.Metadata.Name == setName {
			return resource, meta, true, nil
		}
	}
	return api.Resource{}, meta, false, nil
}

func (c *PeerGroupSyncClient) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func stampSyncProvenance(resource *api.Resource, meta syncMetadata) {
	if resource.Metadata.Annotations == nil {
		resource.Metadata.Annotations = map[string]string{}
	}
	resource.Metadata.Annotations["routerd.net/sync-publisher"] = meta.PublisherID
	resource.Metadata.Annotations["routerd.net/sync-revision"] = strconv.FormatInt(meta.Revision, 10)
	resource.Metadata.Annotations["routerd.net/sync-digest"] = meta.Digest
}

func (c *PeerGroupSyncClient) savePeerGroup(_ context.Context, groupName string, resource api.Resource, meta syncMetadata) error {
	now := time.Now().UTC()
	if c.Now != nil {
		now = c.Now().UTC()
	}
	resource.TypeMeta = api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"}
	resource.Metadata.Name = strings.TrimSpace(groupName)
	expiresAt := meta.ValidUntil
	if expiresAt.IsZero() || !expiresAt.After(now) {
		expiresAt = now.Add(DefaultLeaseTTL)
	}
	part := dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{
			Name: safeName("peer-group-sync-" + groupName),
		},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:      PeerGroupSyncDynamicSource(groupName),
			Generation:  dynamicGeneration,
			ObservedAt:  now,
			ExpiresAt:   expiresAt,
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

func (c *PeerGroupSyncClient) saveMemberSet(_ context.Context, setName string, resource api.Resource, meta syncMetadata) error {
	now := time.Now().UTC()
	if c.Now != nil {
		now = c.Now().UTC()
	}
	resource.TypeMeta = api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityMemberSet"}
	resource.Metadata.Name = strings.TrimSpace(setName)
	expiresAt := meta.ValidUntil
	if expiresAt.IsZero() || !expiresAt.After(now) {
		expiresAt = now.Add(DefaultLeaseTTL)
	}
	part := dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{
			Name: safeName("member-set-sync-" + setName),
		},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:      MemberSetSyncDynamicSource(setName),
			Generation:  dynamicGeneration,
			ObservedAt:  now,
			ExpiresAt:   expiresAt,
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
