// SPDX-License-Identifier: BSD-3-Clause

package bgpdaemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const AppliedVersion = 1

type AppliedConfig struct {
	Version        int                    `json:"version"`
	UpdatedAt      string                 `json:"updatedAt,omitempty"`
	Global         AppliedGlobal          `json:"global,omitempty"`
	Peers          map[string]AppliedPeer `json:"peers,omitempty"`
	Advertisements []string               `json:"advertisements,omitempty"`
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
	return nil
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
