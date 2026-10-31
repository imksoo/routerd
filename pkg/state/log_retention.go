// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"fmt"
	"path/filepath"
)

// LogRetentionPath identifies the opened state database containing operational
// event, access and plugin logs. Platform defaults do not identify an instance
// started with a different state-file option.
func (s *SQLiteStore) LogRetentionPath() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return "", sql.ErrConnDone
	}
	if s.path == "" {
		return "", fmt.Errorf("state log database has no filesystem identity")
	}
	return filepath.Abs(s.path)
}
