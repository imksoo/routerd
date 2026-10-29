// SPDX-License-Identifier: BSD-3-Clause

package bgpdaemon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/stringutil"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

// Version 3 gives the daemon exclusive policy and local-path ownership, with
// complete static/dynamic policy intent and pending path effects. Earlier
// journals do not prove this ownership contract and are rejected at cutover.
const AppliedVersion = 3

const (
	AppliedPathSourceStatic = "routerd-static"

	AppliedPathFamilyIPv4Unicast = "ipv4-unicast"
	AppliedPathFamilyIPv6Unicast = "ipv6-unicast"
)

type AppliedConfig struct {
	Version       int           `json:"version"`
	UpdatedAt     string        `json:"updatedAt,omitempty"`
	Policy        PolicyIntent  `json:"policy"`
	PendingPolicy *PolicyIntent `json:"pendingPolicy,omitempty"`
	// PendingDirectPeerAdditions records direct SAM peers which were journaled
	// before AddPeer but have not yet reached a complete desired-state save.
	// routerd-bgp skips them on restore, while routerd can identify and finish
	// or withdraw the live peer before changing its narrow import policy.
	PendingDirectPeerAdditions []string `json:"pendingDirectPeerAdditions,omitempty"`
	// PendingDirectPeerRemovals records direct SAM peers that must not be
	// restored after routerd-bgp restarts. The peer record remains below so a
	// live daemon keeps its existing policy during the withdrawal transaction.
	// routerd writes this marker before DeletePeer, so a crash between the live
	// deletion and the final desired-state save cannot resurrect the peer.
	PendingDirectPeerRemovals []string `json:"pendingDirectPeerRemovals,omitempty"`
	// PendingPathAdds records native path ownership before AddPath.
	// A missing returned UUID must never make a possibly-live path disappear
	// from reconciliation after a failed applied-state write.
	PendingPathAdds []AppliedPath `json:"pendingPathAdds,omitempty"`
	// PendingPathRemovals contains locally originated paths which have
	// been removed from the restart set but still need a live GoBGP withdrawal.
	// Their exact source/prefix native identity survives a lost UUID, so retry
	// never re-advertises the path or withdraws another local source or peer.
	PendingPathRemovals []AppliedPath          `json:"pendingPathRemovals,omitempty"`
	Global              AppliedGlobal          `json:"global,omitempty"`
	Peers               map[string]AppliedPeer `json:"peers,omitempty"`
	Paths               []AppliedPath          `json:"paths,omitempty"`
}

type AppliedGlobal struct {
	ASN              uint32                  `json:"asn"`
	RouterID         string                  `json:"routerID"`
	ListenPort       int                     `json:"listenPort"`
	ListenAddresses  []string                `json:"listenAddresses,omitempty"`
	Families         []string                `json:"families,omitempty"`
	UseMultiplePaths bool                    `json:"useMultiplePaths"`
	GracefulRestart  *AppliedGracefulRestart `json:"gracefulRestart,omitempty"`
}

type AppliedGracefulRestart struct {
	Enabled         bool   `json:"enabled"`
	RestartTime     uint32 `json:"restartTime"`
	StaleRoutesTime uint32 `json:"staleRoutesTime"`
}

type AppliedPeer struct {
	SendCommunity           uint32 `json:"sendCommunity"`
	Address                 string `json:"address"`
	ASN                     uint32 `json:"asn"`
	PassiveMode             bool   `json:"passiveMode,omitempty"`
	Password                string `json:"password,omitempty"`
	BFD                     string `json:"bfd,omitempty"`
	EbgpMultihop            int    `json:"ebgpMultihop,omitempty"`
	RouteReflectorClient    bool   `json:"routeReflectorClient,omitempty"`
	RouteReflectorClusterID string `json:"routeReflectorClusterID,omitempty"`
	TimersProfile           string `json:"timersProfile,omitempty"`
	ConvergenceProfile      string `json:"convergenceProfile,omitempty"`
	// PreserveImportPrefixes keeps an explicit peer import allowlist from being
	// widened with router-wide dynamic prefixes during daemon restore. It is
	// used by direct SAM leaf peers, whose admitted /32 boundary must survive a
	// routerd-bgp restart unchanged.
	PreserveImportPrefixes bool `json:"preserveImportPrefixes,omitempty"`
	// RejectImportAll retains the route boundary for a direct SAM peer whose
	// admitted claim currently owns no mobility /32. The session is restored, but
	// every inbound route remains rejected until fresh desired state says
	// otherwise.
	RejectImportAll bool                    `json:"rejectImportAll,omitempty"`
	GracefulRestart *AppliedGracefulRestart `json:"gracefulRestart,omitempty"`
}

type AppliedPath struct {
	Source string           `json:"source"`
	Prefix string           `json:"prefix"`
	Family string           `json:"family,omitempty"`
	Attrs  AppliedPathAttrs `json:"attrs,omitempty"`
	UUID   string           `json:"uuid,omitempty"`
}

type AppliedPathAttrs struct {
	NextHop     string   `json:"nextHop,omitempty"`
	LocalPref   uint32   `json:"localPref,omitempty"`
	MED         uint32   `json:"med,omitempty"`
	Communities []string `json:"communities,omitempty"`
}

func Normalize(config AppliedConfig) AppliedConfig {
	if config.Version == 0 {
		config.Version = AppliedVersion
	}
	config.Global.RouterID = strings.TrimSpace(config.Global.RouterID)
	config.Global.ListenAddresses = stringutil.UniqueTrimmedSorted(config.Global.ListenAddresses)
	config.Global.Families = stringutil.UniqueTrimmedSorted(config.Global.Families)
	config.PendingDirectPeerAdditions = stringutil.UniqueTrimmedSorted(config.PendingDirectPeerAdditions)
	config.PendingDirectPeerRemovals = stringutil.UniqueTrimmedSorted(config.PendingDirectPeerRemovals)
	config.PendingPathRemovals = normalizeAppliedPaths(config.PendingPathRemovals)
	config.PendingPathAdds = normalizeAppliedPaths(config.PendingPathAdds)
	config.Paths = normalizeAppliedPaths(config.Paths)
	if config.Peers != nil {
		peers := make(map[string]AppliedPeer, len(config.Peers))
		for key, peer := range config.Peers {
			peer.Address = stringutil.FirstNonBlank(peer.Address, key)
			if peer.SendCommunity == 0 {
				peer.SendCommunity = 3
			}
			peer.BFD = strings.TrimSpace(peer.BFD)
			if peer.EbgpMultihop < 0 || peer.EbgpMultihop > 255 {
				peer.EbgpMultihop = 0
			}
			peer.RouteReflectorClusterID = strings.TrimSpace(peer.RouteReflectorClusterID)
			peer.TimersProfile = strings.TrimSpace(peer.TimersProfile)
			peer.ConvergenceProfile = strings.TrimSpace(peer.ConvergenceProfile)
			if peer.Address != "" {
				peers[peer.Address] = peer
			}
		}
		config.Peers = peers
	}
	return config
}

func NormalizeAppliedPath(path AppliedPath) AppliedPath {
	path.Source = strings.TrimSpace(path.Source)
	path.Prefix = strings.TrimSpace(path.Prefix)
	if parsed, err := netip.ParsePrefix(path.Prefix); err == nil {
		parsed = parsed.Masked()
		path.Prefix = parsed.String()
		if strings.TrimSpace(path.Family) == "" {
			path.Family = familyForPrefix(parsed)
		}
	}
	path.Family = strings.TrimSpace(path.Family)
	path.Attrs.NextHop = strings.TrimSpace(path.Attrs.NextHop)
	path.Attrs.Communities = stringutil.UniqueTrimmedSorted(path.Attrs.Communities)
	path.UUID = strings.TrimSpace(path.UUID)
	return path
}

func StaticAppliedPath(prefix string, uuid []byte) AppliedPath {
	return NormalizeAppliedPath(AppliedPath{
		Source: AppliedPathSourceStatic,
		Prefix: prefix,
		UUID:   EncodeUUID(uuid),
	})
}

func StaticAdvertisements(paths []AppliedPath) []string {
	var out []string
	for _, path := range normalizeAppliedPaths(paths) {
		if path.Source == AppliedPathSourceStatic {
			out = append(out, path.Prefix)
		}
	}
	return stringutil.UniqueTrimmedSorted(out)
}

func NonStaticPaths(paths []AppliedPath) []AppliedPath {
	var out []AppliedPath
	for _, path := range normalizeAppliedPaths(paths) {
		if path.Source != AppliedPathSourceStatic {
			out = append(out, path)
		}
	}
	return out
}

func AppliedPathKey(path AppliedPath) string {
	path = NormalizeAppliedPath(path)
	return path.Source + "|" + path.Family + "|" + path.Prefix
}

func IsMobilityPathSource(source string) bool {
	owner, ok := dynamicconfig.ParseMobilityPoolPlanSource(source)
	return ok && !owner.ARPObserver
}

func EncodeUUID(uuid []byte) string {
	if len(uuid) == 0 {
		return ""
	}
	return hex.EncodeToString(uuid)
}

func DecodeUUID(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	return hex.DecodeString(value)
}

func ReadApplied(path string) (AppliedConfig, bool, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return AppliedConfig{}, false, nil
	}
	if err != nil {
		return AppliedConfig{}, false, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if err != nil {
		return AppliedConfig{}, false, err
	}
	if len(data) > 4<<20 {
		return AppliedConfig{}, false, fmt.Errorf("applied BGP state exceeds 4 MiB")
	}
	if strings.TrimSpace(string(data)) == "" {
		return AppliedConfig{}, false, fmt.Errorf("applied BGP state is empty")
	}
	var config AppliedConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return AppliedConfig{}, false, err
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		return AppliedConfig{}, false, fmt.Errorf("applied BGP state contains trailing data")
	}
	if config.Version != AppliedVersion {
		return AppliedConfig{}, false, fmt.Errorf("unsupported applied BGP config version %d; expected %d", config.Version, AppliedVersion)
	}
	if err := Validate(config); err != nil {
		return AppliedConfig{}, false, err
	}
	return Normalize(config), true, nil
}

func WriteApplied(path string, config AppliedConfig) error {
	if config.Version != 0 && config.Version != AppliedVersion {
		return fmt.Errorf("unsupported applied BGP config version %d; expected %d", config.Version, AppliedVersion)
	}
	if err := validateJournalPathIdentities(config); err != nil {
		return err
	}
	config = Normalize(config)
	config.Version = AppliedVersion
	if err := Validate(config); err != nil {
		return err
	}
	if strings.TrimSpace(config.UpdatedAt) == "" {
		config.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > 4<<20 {
		return fmt.Errorf("applied BGP state exceeds 4 MiB")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".applied-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	// tmp.Sync makes the file contents durable, but the rename itself is not
	// guaranteed to survive a power loss until the containing directory is
	// synced as well. Applied-state transition fences rely on that rename.
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return err
	}
	ok = true
	return nil
}

func Hash(config AppliedConfig) string {
	config = Normalize(config)
	config.UpdatedAt = ""
	data, err := json.Marshal(config)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Pending and applied identities must be validated before normalization can
// discard an incomplete entry. Unknown ownership is not an empty journal.
func validateJournalPathIdentities(config AppliedConfig) error {
	for _, paths := range [][]AppliedPath{config.Paths, config.PendingPathAdds, config.PendingPathRemovals} {
		for _, path := range paths {
			if strings.TrimSpace(path.Source) == "" || strings.TrimSpace(path.Prefix) == "" {
				return fmt.Errorf("BGP journal path source and prefix are required")
			}
		}
	}
	return nil
}

func Validate(config AppliedConfig) error {
	if err := validateJournalPathIdentities(config); err != nil {
		return err
	}
	config = Normalize(config)
	if config.Version != AppliedVersion {
		return fmt.Errorf("unsupported applied BGP config version %d", config.Version)
	}
	if config.Global.ASN == 0 || config.Global.RouterID == "" {
		return fmt.Errorf("applied BGP global config is incomplete")
	}
	if config.Policy.RouterName != "" || len(config.Policy.Scopes) > 0 {
		if err := ValidatePolicyIntent(config.Policy); err != nil {
			return err
		}
	}
	if config.PendingPolicy != nil {
		if err := ValidatePolicyIntent(*config.PendingPolicy); err != nil {
			return err
		}
	}
	for address, peer := range config.Peers {
		if peer.SendCommunity < 1 || peer.SendCommunity > 3 {
			return fmt.Errorf("applied BGP peer %q has invalid community send mask", address)
		}
		if peer.Address == "" || peer.ASN == 0 {
			return fmt.Errorf("applied BGP peer %q is incomplete", address)
		}
		if peer.RouteReflectorClusterID != "" {
			addr, err := netip.ParseAddr(peer.RouteReflectorClusterID)
			if err != nil || !addr.Is4() {
				return fmt.Errorf("applied BGP peer %q routeReflectorClusterID must be an IPv4 router ID", address)
			}
		}
	}
	for _, pending := range []struct {
		name      string
		addresses []string
	}{
		{name: "pending direct peer addition", addresses: config.PendingDirectPeerAdditions},
		{name: "pending direct peer removal", addresses: config.PendingDirectPeerRemovals},
	} {
		for _, address := range pending.addresses {
			peer, found := config.Peers[address]
			if !found || !peer.PreserveImportPrefixes {
				return fmt.Errorf("%s %q is not a persisted direct peer", pending.name, address)
			}
		}
	}
	for _, path := range config.Paths {
		if err := ValidateAppliedPath(path); err != nil {
			return err
		}
	}
	for _, path := range config.PendingPathAdds {
		if err := ValidateAppliedPath(path); err != nil {
			return err
		}
		prefix, err := netip.ParsePrefix(path.Prefix)
		if IsMobilityPathSource(path.Source) && (err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32) {
			return fmt.Errorf("pending dynamic BGP path %q must be an IPv4 /32", path.Prefix)
		}
		if path.UUID != "" {
			return fmt.Errorf("pending dynamic BGP path %q already has an applied UUID", path.Prefix)
		}
	}
	for _, path := range config.PendingPathRemovals {
		if err := ValidateAppliedPath(path); err != nil {
			return err
		}
	}
	return nil
}

func ValidateAppliedPath(path AppliedPath) error {
	path = NormalizeAppliedPath(path)
	if path.Source == "" {
		return fmt.Errorf("applied BGP path source is required")
	}
	if path.Source != AppliedPathSourceStatic && !IsMobilityPathSource(path.Source) {
		return fmt.Errorf("applied BGP path source %q is not a supported owner", path.Source)
	}
	prefix, err := netip.ParsePrefix(path.Prefix)
	if err != nil {
		return fmt.Errorf("applied BGP path prefix %q is invalid: %w", path.Prefix, err)
	}
	prefix = prefix.Masked()
	if path.Prefix != prefix.String() {
		return fmt.Errorf("applied BGP path prefix %q must be masked as %q", path.Prefix, prefix.String())
	}
	if family := familyForPrefix(prefix); path.Family != "" && path.Family != family {
		return fmt.Errorf("applied BGP path %s family %q does not match prefix family %q", path.Prefix, path.Family, family)
	}
	if path.Attrs.NextHop != "" {
		if _, err := netip.ParseAddr(path.Attrs.NextHop); err != nil {
			return fmt.Errorf("applied BGP path %s nextHop %q is invalid: %w", path.Prefix, path.Attrs.NextHop, err)
		}
	}
	if _, err := bgpstate.StandardCommunities(path.Attrs.Communities); err != nil {
		return fmt.Errorf("applied BGP path %s communities: %w", path.Prefix, err)
	}
	if path.UUID != "" {
		if _, err := DecodeUUID(path.UUID); err != nil {
			return fmt.Errorf("applied BGP path %s uuid is invalid hex: %w", path.Prefix, err)
		}
	}
	return nil
}

func normalizeAppliedPaths(paths []AppliedPath) []AppliedPath {
	byKey := map[string]AppliedPath{}
	for _, path := range paths {
		path = NormalizeAppliedPath(path)
		if path.Source == "" || path.Prefix == "" {
			continue
		}
		byKey[AppliedPathKey(path)] = path
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]AppliedPath, 0, len(keys))
	for _, key := range keys {
		out = append(out, byKey[key])
	}
	return out
}

func familyForPrefix(prefix netip.Prefix) string {
	if prefix.Addr().Is6() {
		return AppliedPathFamilyIPv6Unicast
	}
	return AppliedPathFamilyIPv4Unicast
}
