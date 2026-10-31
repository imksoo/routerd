// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// LogRetentionSource identifies the opened database, not a new file that later
// occupies its pathname. Memory/unknown sources do not authorize a file prune.
func (s *SQLiteStore) LogRetentionSource() (string, fs.FileInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return "", nil, sql.ErrConnDone
	}
	if s.retentionFile == nil {
		return "", nil, fmt.Errorf("state log database has no filesystem identity")
	}
	var path string
	if err := s.db.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path); err != nil {
		return "", nil, err
	}
	if !filepath.IsAbs(path) {
		return "", nil, fmt.Errorf("state log database has no absolute filesystem identity")
	}
	current, err := os.Stat(path)
	if err != nil {
		return "", nil, err
	}
	if !os.SameFile(s.retentionFile, current) {
		return "", nil, fmt.Errorf("state log database file identity changed")
	}
	return path, s.retentionFile, nil
}
