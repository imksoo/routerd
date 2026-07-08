// SPDX-License-Identifier: BSD-3-Clause

package nftstate

import (
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
	return now.Before(info.ModTime().Add(DefaultVerifyInterval))
}

func MarkVerified(path string, now time.Time) error {
	if path == "" {
		return nil
	}
	marker := markerPath(path)
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(marker, []byte(now.UTC().Format(time.RFC3339Nano)+"\n"), 0o644); err != nil {
		return err
	}
	return os.Chtimes(marker, now, now)
}

func markerPath(path string) string {
	return path + ".verified"
}
