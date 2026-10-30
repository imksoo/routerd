// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const activationReceiptSuffix = ".routerd-activation.json"

type artifactActivation struct {
	AppliedDigest string `json:"appliedDigest,omitempty"`
	PendingDigest string `json:"pendingDigest,omitempty"`
	Removing      bool   `json:"removing,omitempty"`
}

// File equality describes installed intent, not whether its consumer accepted it.
// Keep the last acknowledged input until activation succeeds, including when a
// command failed after the new file was already installed.
func artifactActivationDigest(data, input []byte) string {
	h := sha256.New()
	for _, value := range [][]byte{data, input} {
		_, _ = h.Write(binary.BigEndian.AppendUint64(nil, uint64(len(value))))
		_, _ = h.Write(value)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func artifactActivationReceipt(path string) (artifactActivation, error) {
	data, err := os.ReadFile(path + activationReceiptSuffix)
	if os.IsNotExist(err) {
		return artifactActivation{}, nil
	}
	if err != nil {
		return artifactActivation{}, err
	}
	var receipt artifactActivation
	if err := json.Unmarshal(data, &receipt); err != nil {
		return receipt, fmt.Errorf("activation receipt for %s: %w", path, err)
	}
	for _, digest := range []string{receipt.AppliedDigest, receipt.PendingDigest} {
		if digest == "" {
			continue
		}
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != sha256.Size {
			return receipt, fmt.Errorf("invalid activation receipt for %s", path)
		}
	}
	if receipt.AppliedDigest == "" && receipt.PendingDigest == "" && !receipt.Removing {
		return receipt, fmt.Errorf("empty activation receipt for %s", path)
	}

	return receipt, nil
}

func writeArtifactActivation(path string, receipt artifactActivation) error {
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	return writeAtomicArtifact(path+activationReceiptSuffix, append(data, '\n'), 0o644)
}

func beginArtifactRemoval(path string) error {
	receipt, err := artifactActivationReceipt(path)
	if err != nil {
		return err
	}
	receipt.Removing = true
	return writeArtifactActivation(path, receipt)
}

func prepareArtifactActivation(path string, data, input []byte, dryRun bool) (changed, pending bool, digest string, err error) {
	digest = artifactActivationDigest(data, input)
	applied, err := artifactActivationReceipt(path)
	if err != nil {
		return false, false, "", err
	}
	changed, err = writeFileIfChanged(path, data, 0o644, true)
	if err != nil {
		return false, false, "", err
	}
	pending = changed || applied.Removing || applied.PendingDigest != "" || applied.AppliedDigest != digest
	if dryRun {
		return changed, pending, digest, nil
	}
	if pending {
		applied.PendingDigest = digest
		applied.Removing = false
		if err := writeArtifactActivation(path, applied); err != nil {
			return changed, pending, digest, err
		}
	}
	if changed {
		_, err = writeFileIfChanged(path, data, 0o644, false)
	}
	return changed, pending, digest, err
}

func acknowledgeArtifactActivation(path, digest string) error {
	return writeArtifactActivation(path, artifactActivation{AppliedDigest: digest})
}

func clearArtifactActivation(path string) error {
	_, err := removeFileIfExists(path+activationReceiptSuffix, false)
	return err
}

// Both the installed input and its receipt must survive a process restart as
// complete files. In particular, a truncated receipt must never authorize work.
func writeAtomicArtifact(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".routerd-artifact-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
