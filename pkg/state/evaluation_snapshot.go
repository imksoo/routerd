// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// CopyEvaluationStateTo materializes only object status (including state
// variables) and dynamic desired parts from one SQLite read transaction. The
// source is released before writing the isolated destination or running any
// controller. It deliberately excludes jobs, event cursors, action execution
// journals, federation history and generation records: this is a planner input
// snapshot, not a backup or permission to replay external operations.
//
// Raw rows preserve freshness, invalid envelopes and source generation ordering
// exactly. Normal write APIs may initialize a missing ObservedAt or update an
// object's generation, which would change the meaning of copied evidence.
func (s *SQLiteStore) CopyEvaluationStateTo(dst *SQLiteStore) error {
	if dst == nil || s == dst || s.path == dst.path {
		return fmt.Errorf("evaluation snapshot requires a separate destination")
	}
	tables, err := s.evaluationSnapshotRows()
	if err != nil {
		return err
	}
	dst.mu.Lock()
	defer dst.mu.Unlock()
	if dst.closed {
		return sql.ErrConnDone
	}
	tx, err := dst.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range tables {
		// The destination is newly initialized and must not merge with unrelated
		// prior state. Primary-key collisions fail the whole copy transaction.
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(strings.Split(table.columns, ","))), ",")
		query := "INSERT INTO " + table.name + "(" + table.columns + ") VALUES(" + placeholders + ")"
		for _, values := range table.rows {
			if _, err := tx.Exec(query, values...); err != nil {
				return fmt.Errorf("copy evaluation snapshot %s: %w", table.name, err)
			}
		}
	}
	return tx.Commit()
}

type evaluationSnapshotTable struct {
	name, columns string
	rows          [][]any
}

func (s *SQLiteStore) evaluationSnapshotRows() ([]evaluationSnapshotTable, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, sql.ErrConnDone
	}
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tables := []evaluationSnapshotTable{
		{name: "objects", columns: "api_version,kind,name,uid,resource_version,observed_generation,last_applied_path,status,created_at,modified_at"},
		{name: "dynamic_config_parts", columns: "id,source,generation,observed_at,expires_at,digest,resources_json,directives_json,actionplans_json,mobility_dataplane_json,arp_observer_intents_json,fib_verdicts_json,status,error,created_at,updated_at"},
	}
	for i := range tables {
		table := &tables[i]
		var exists bool
		if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)", table.name).Scan(&exists); err != nil {
			return nil, fmt.Errorf("inspect evaluation snapshot schema: %w", err)
		}
		if !exists {
			return nil, fmt.Errorf("%w: required %s table is missing", ErrSchemaNotInitialized, table.name)
		}
		rows, err := tx.Query("SELECT " + table.columns + " FROM " + table.name)
		if err != nil {
			return nil, fmt.Errorf("read evaluation snapshot %s: %w", table.name, err)
		}
		for rows.Next() {
			values := make([]any, len(strings.Split(table.columns, ",")))
			pointers := make([]any, len(values))
			for j := range values {
				pointers[j] = &values[j]
			}
			if err := rows.Scan(pointers...); err != nil {
				rows.Close()
				return nil, err
			}
			table.rows = append(table.rows, values)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	// A read transaction has no writes to commit; Rollback also ensures no WAL
	// reader is retained across later controller work.
	return tables, tx.Rollback()
}
