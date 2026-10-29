// SPDX-License-Identifier: BSD-3-Clause

package eventfile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const DefaultMaxBytes int64 = 4 << 20

func AppendJSONLine(path string, value any) error {
	return AppendJSONLineWithLimit(path, value, DefaultMaxBytes)
}

func AppendJSONLineWithLimit(path string, value any, maxBytes int64) (err error) {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if maxBytes > 0 {
		info, statErr := os.Stat(path)
		if statErr != nil && !os.IsNotExist(statErr) {
			return statErr
		}
		if statErr == nil && info.Size() >= maxBytes {
			if err := os.Remove(path + ".1"); err != nil && !os.IsNotExist(err) {
				return err
			}
			if err := os.Rename(path, path+".1"); err != nil {
				return err
			}
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	return json.NewEncoder(file).Encode(value)
}
