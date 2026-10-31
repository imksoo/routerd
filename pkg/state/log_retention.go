// SPDX-License-Identifier: BSD-3-Clause

package state

import "io/fs"

// LogRetentionSource identifies the opened database, not a new file that later
// occupies its pathname. Memory/unknown sources do not authorize a file prune.
func (s *SQLiteStore) LogRetentionSource() (string, fs.FileInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.databaseFileLocked()
}
