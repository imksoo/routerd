// SPDX-License-Identifier: BSD-3-Clause

package eventfile

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/imksoo/routerd/pkg/daemonapi"
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

const HistoryLimit = 1000

// Restore returns the most recent events in file order and the largest cursor read.
func Restore(path string) ([]daemonapi.DaemonEvent, uint64, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	var events []daemonapi.DaemonEvent
	var cursor uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event daemonapi.DaemonEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		events = append(events, event)
		if len(events) > HistoryLimit {
			events = events[1:]
		}
		if id, err := strconv.ParseUint(event.Cursor, 10, 64); err == nil && id > cursor {
			cursor = id
		}
	}
	return events, cursor, scanner.Err()
}
