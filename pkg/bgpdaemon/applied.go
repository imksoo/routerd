// SPDX-License-Identifier: BSD-3-Clause

package bgpdaemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const AppliedVersion = 1

const (
	AppliedPathSourceStatic = "routerd-static"

	AppliedPathFamilyIPv4Unicast = "ipv4-unicast"
	AppliedPathFamilyIPv6Unicast = "ipv6-unicast"
)

type AppliedConfig struct {
	Version   int                    `json:"version"`
	UpdatedAt string                 `json:"updatedAt,omitempty"`
	Global    AppliedGlobal          `json:"global,omitempty"`
	Peers     map[string]AppliedPeer `json:"peers,omitempty"`
	Paths     []AppliedPath          `json:"paths,omitempty"`
	// Advertisements is the legacy static-prefix view. New code should use Paths
	// with source=routerd-static, but keeping this field preserves existing state
	// files and control API clients.
	Advertisements []string `json:"advertisements,omitempty"`
}

type AppliedGlobal struct {
	ASN              uint32                  `json:"asn"`
	RouterID         string                  `json:"routerID"`
	ListenPort       int                     `json:"listenPort"`
	ListenAddresses  []string                `json:"listenAddresses,omitempty"`
	Families         []string                `json:"families,omitempty"`
	UseMultiplePaths bool                    `json:"useMultiplePaths"`
	ImportPolicy     AppliedImportPolicy     `json:"importPolicy,omitempty"`
	GracefulRestart  *AppliedGracefulRestart `json:"gracefulRestart,omitempty"`
}

type AppliedImportPolicy struct {
	AllowedPrefixes []string `json:"allowedPrefixes,omitempty"`
	NextHopRewrite  string   `json:"nextHopRewrite,omitempty"`
}

type AppliedGracefulRestart struct {
	Enabled         bool   `json:"enabled"`
	RestartTime     uint32 `json:"restartTime"`
	StaleRoutesTime uint32 `json:"staleRoutesTime"`
}

type AppliedPeer struct {
	Address            string                  `json:"address"`
	ASN                uint32                  `json:"asn"`
	Password           string                  `json:"password,omitempty"`
	EbgpMultihop       int                     `json:"ebgpMultihop,omitempty"`
	TimersProfile      string                  `json:"timersProfile,omitempty"`
	ConvergenceProfile string                  `json:"convergenceProfile,omitempty"`
	ImportPolicyName   string                  `json:"importPolicyName,omitempty"`
	ImportPolicy       AppliedImportPolicy     `json:"importPolicy,omitempty"`
	GracefulRestart    *AppliedGracefulRestart `json:"gracefulRestart,omitempty"`
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
	config.Global.ListenAddresses = cleanStrings(config.Global.ListenAddresses)
	config.Global.Families = cleanStrings(config.Global.Families)
	config.Global.ImportPolicy.AllowedPrefixes = cleanStrings(config.Global.ImportPolicy.AllowedPrefixes)
	config.Global.ImportPolicy.NextHopRewrite = strings.TrimSpace(config.Global.ImportPolicy.NextHopRewrite)
	config.Advertisements = cleanStrings(config.Advertisements)
	config.Paths = normalizeAppliedPaths(config.Paths, config.Advertisements)
	config.Advertisements = StaticAdvertisements(config.Paths)
	if config.Peers != nil {
		peers := make(map[string]AppliedPeer, len(config.Peers))
		for key, peer := range config.Peers {
			peer.Address = firstNonEmpty(strings.TrimSpace(peer.Address), strings.TrimSpace(key))
			if peer.EbgpMultihop < 0 || peer.EbgpMultihop > 255 {
				peer.EbgpMultihop = 0
			}
			peer.TimersProfile = strings.TrimSpace(peer.TimersProfile)
			peer.ConvergenceProfile = strings.TrimSpace(peer.ConvergenceProfile)
			peer.ImportPolicyName = strings.TrimSpace(peer.ImportPolicyName)
			peer.ImportPolicy.AllowedPrefixes = cleanStrings(peer.ImportPolicy.AllowedPrefixes)
			peer.ImportPolicy.NextHopRewrite = strings.TrimSpace(peer.ImportPolicy.NextHopRewrite)
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
	path.Attrs.Communities = cleanStrings(path.Attrs.Communities)
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
	for _, path := range normalizeAppliedPaths(paths, nil) {
		if path.Source == AppliedPathSourceStatic {
			out = append(out, path.Prefix)
		}
	}
	return cleanStrings(out)
}

func NonStaticPaths(paths []AppliedPath) []AppliedPath {
	var out []AppliedPath
	for _, path := range normalizeAppliedPaths(paths, nil) {
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
	source = strings.TrimSpace(source)
	return strings.HasPrefix(source, "MobilityPool/") || strings.HasPrefix(source, "mobility/")
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
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return AppliedConfig{}, false, nil
	}
	if err != nil {
		return AppliedConfig{}, false, err
	}
	var config AppliedConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return AppliedConfig{}, false, err
	}
	return Normalize(config), true, nil
}

func WriteApplied(path string, config AppliedConfig) error {
	config = Normalize(config)
	config.Version = AppliedVersion
	if strings.TrimSpace(config.UpdatedAt) == "" {
		config.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
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

func Validate(config AppliedConfig) error {
	config = Normalize(config)
	if config.Version != AppliedVersion {
		return fmt.Errorf("unsupported applied BGP config version %d", config.Version)
	}
	if config.Global.ASN == 0 || config.Global.RouterID == "" {
		return fmt.Errorf("applied BGP global config is incomplete")
	}
	for address, peer := range config.Peers {
		if peer.Address == "" || peer.ASN == 0 {
			return fmt.Errorf("applied BGP peer %q is incomplete", address)
		}
	}
	for _, path := range config.Paths {
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
	if path.UUID != "" {
		if _, err := DecodeUUID(path.UUID); err != nil {
			return fmt.Errorf("applied BGP path %s uuid is invalid hex: %w", path.Prefix, err)
		}
	}
	return nil
}

func normalizeAppliedPaths(paths []AppliedPath, legacyAdvertisements []string) []AppliedPath {
	byKey := map[string]AppliedPath{}
	for _, prefix := range cleanStrings(legacyAdvertisements) {
		path := StaticAppliedPath(prefix, nil)
		byKey[AppliedPathKey(path)] = path
	}
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

func cleanStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
