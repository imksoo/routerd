// SPDX-License-Identifier: BSD-3-Clause

package nftstate

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"time"
)

const DefaultVerifyInterval = 2 * time.Minute

func RecentlyVerified(path string, now time.Time) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(markerPath(path))
	if err != nil {
		return false
	}
	if !now.Before(info.ModTime().Add(DefaultVerifyInterval)) {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	verified, err := os.ReadFile(markerPath(path))
	digest := sha256.Sum256(data)
	return err == nil && bytes.Equal(verified, digest[:])
}

func MarkVerified(path string, now time.Time) error {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	marker := markerPath(path)
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(marker, digest[:], 0o644); err != nil {
		return err
	}
	return os.Chtimes(marker, now, now)
}

// Invalidate removes the acknowledgement before changing or applying a ruleset.
func Invalidate(path string) error {
	if path == "" {
		return nil
	}
	err := os.Remove(markerPath(path))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func markerPath(path string) string {
	return path + ".verified"
}
