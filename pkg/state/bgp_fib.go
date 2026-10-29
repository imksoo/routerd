// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgp"
)

const bgpFIBJournalName = "bgp-fib"

func (s *SQLiteStore) ReadBGPFIB(ctx context.Context) (bgp.FIBJournal, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return bgp.FIBJournal{}, false, sql.ErrConnDone
	}
	var raw string
	err := s.db.QueryRowContext(ctx, "SELECT status FROM objects WHERE api_version=? AND kind='Router' AND name=?", api.NetAPIVersion, bgpFIBJournalName).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return bgp.FIBJournal{}, false, nil
	}
	if err != nil {
		return bgp.FIBJournal{}, false, err
	}
	var j *bgp.FIBJournal
	d := json.NewDecoder(bytes.NewBufferString(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&j); err != nil {
		return bgp.FIBJournal{}, false, fmt.Errorf("decode BGP FIB journal: %w", err)
	}
	if j == nil {
		return bgp.FIBJournal{}, false, fmt.Errorf("BGP FIB journal is null")
	}
	if err := d.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return bgp.FIBJournal{}, false, fmt.Errorf("BGP FIB journal contains trailing data")
	}
	if err := bgp.ValidateFIBJournal(*j); err != nil {
		return bgp.FIBJournal{}, false, err
	}
	return *j, true, nil
}

func (s *SQLiteStore) SaveBGPFIB(ctx context.Context, j bgp.FIBJournal) error {
	if err := bgp.ValidateFIBJournal(j); err != nil {
		return err
	}
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `INSERT INTO objects(api_version,kind,name,uid,resource_version,status,created_at,modified_at)
VALUES(?,'Router',?,?,1,?,?,?) ON CONFLICT(api_version,kind,name) DO UPDATE SET resource_version=resource_version+1,status=excluded.status,modified_at=excluded.modified_at`, api.NetAPIVersion, bgpFIBJournalName, api.NetAPIVersion+"/Router/"+bgpFIBJournalName, string(raw), now, now)
	return err
}
