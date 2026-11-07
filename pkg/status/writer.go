// SPDX-License-Identifier: BSD-3-Clause

package status

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func Write(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create status dir: %w", err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode status: %w", err)
	}
	data = append(data, '\n')
	file, err := os.CreateTemp(filepath.Dir(path), ".routerd-status-*")
	if err != nil {
		return fmt.Errorf("create status %s: %w", path, err)
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0o644); err != nil {
		file.Close()
		return fmt.Errorf("set status mode %s: %w", path, err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("write status %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close status %s: %w", path, err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("publish status %s: %w", path, err)
	}
	return nil
}
