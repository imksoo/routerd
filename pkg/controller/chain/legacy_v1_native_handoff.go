// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/imksoo/routerd/pkg/platform"
)

const legacyV1NativeHandoffVersion = "routerd-v1-native-handoff/v1"
const legacyV1NativeHandoffCommit = "e3b49dbb"

// legacyV1NativeHandoff is written only by the Linux package installer while
// replacing the pinned 1.0 release. Its configuration digest prevents the
// migration record from granting ownership to resources introduced later.
type legacyV1NativeHandoff struct {
	Version      string `json:"version"`
	SourceCommit string `json:"sourceCommit"`
	ConfigSHA256 string `json:"configSHA256"`
}

func legacyV1NativeHandoffDefaultPath() string {
	defaults, _ := platform.Current()
	return filepath.Join(defaults.StateDir, "migrations", "routerd-v1-native-handoff.json")
}

func legacyV1NativeHandoffAllows(handoffPath, configPath string) (bool, error) {
	if strings.TrimSpace(handoffPath) == "" || strings.TrimSpace(configPath) == "" {
		return false, nil
	}
	recordData, err := os.ReadFile(handoffPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read legacy native handoff: %w", err)
	}
	var record legacyV1NativeHandoff
	if err := json.Unmarshal(recordData, &record); err != nil || record.Version != legacyV1NativeHandoffVersion || record.SourceCommit != legacyV1NativeHandoffCommit || len(record.ConfigSHA256) != sha256.Size*2 {
		return false, fmt.Errorf("invalid legacy native handoff record")
	}
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return false, fmt.Errorf("read legacy native handoff configuration: %w", err)
	}
	digest := sha256.Sum256(configData)
	if !strings.EqualFold(record.ConfigSHA256, hex.EncodeToString(digest[:])) {
		return false, nil
	}
	return true, nil
}
