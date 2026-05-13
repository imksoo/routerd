// SPDX-License-Identifier: BSD-3-Clause

package eventfile

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const DefaultMaxBytes int64 = 4 << 20

func AppendJSONLine(path string, value any) error {
	return AppendJSONLineWithLimit(path, value, DefaultMaxBytes)
}

func AppendJSONLineWithLimit(path string, value any, maxBytes int64) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if maxBytes > 0 {
		if info, err := os.Stat(path); err == nil && info.Size() >= maxBytes {
			_ = os.Remove(path + ".1")
			_ = os.Rename(path, path+".1")
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(value)
}
