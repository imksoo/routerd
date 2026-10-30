// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"database/sql"
	"fmt"
	"net/netip"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

// projectDHCPLeaseObservation is called only by the existing journal insertion
// helper, inside that caller's transaction. Acknowledging the journal therefore
// also acknowledges its current named lease/tombstone projection.
func projectDHCPLeaseObservation(executor interface {
	Exec(string, ...any) (sql.Result, error)
	QueryRow(string, ...any) *sql.Row
}, now time.Time, event daemonapi.DaemonEvent) error {
	if _, ok := daemonapi.DHCPLeaseActionFromEventType(event.Type); !ok || event.Attributes["replayDisposition"] != "" {
		return nil
	}
	// Historical unqualified events do not gain named source authority merely
	// because their topic resembles a lease callback.
	if event.Resource == nil {
		return nil
	}
	lease, err := daemonapi.DHCPLeaseFromEvent(event)
	if err != nil {
		return fmt.Errorf("project named DHCP observation: %w", err)
	}
	if err := lease.Validate(now); err != nil {
		return fmt.Errorf("project named DHCP observation: %w", err)
	}
	if !time.Unix(0, lease.ObservedAt.UnixNano()).UTC().Equal(lease.ObservedAt) {
		return fmt.Errorf("DHCP observation timestamp outside SQLite nanosecond range")
	}
	lease.IP = netip.MustParseAddr(lease.IP).String()
	expiry := ""
	if !lease.ExpiresAt.IsZero() {
		expiry = lease.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	// Equal timestamps are replayable only for the same observation. Without
	// a causal order, acknowledging conflicting contents would publish an event
	// whose current projection still describes something else.
	var previous daemonapi.DHCPLeaseUpdate
	var priorObserved int64
	var priorExpiry string
	err = executor.QueryRow(`SELECT action,client,hostname,observed_at,expires_at,infinite FROM dhcp_lease_observations WHERE source=? AND ip=?`, lease.Source, lease.IP).Scan(&previous.Action, &previous.MAC, &previous.Hostname, &priorObserved, &priorExpiry, &previous.Infinite)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && priorObserved == lease.ObservedAt.UnixNano() && (previous.Action != lease.Action || previous.MAC != lease.MAC || previous.Hostname != lease.Hostname || priorExpiry != expiry || previous.Infinite != lease.Infinite) {
		return fmt.Errorf("conflicting named DHCP observations share source/IP/time")
	}
	_, err = executor.Exec(`INSERT INTO dhcp_lease_observations(source,ip,action,client,hostname,observed_at,expires_at,infinite)
 VALUES(?,?,?,?,?,?,?,?)
 ON CONFLICT(source,ip) DO UPDATE SET action=excluded.action,client=excluded.client,hostname=excluded.hostname,
 observed_at=excluded.observed_at,expires_at=excluded.expires_at,infinite=excluded.infinite
 WHERE excluded.observed_at > dhcp_lease_observations.observed_at`, lease.Source, lease.IP, lease.Action, lease.MAC, lease.Hostname, lease.ObservedAt.UnixNano(), expiry, lease.Infinite)
	return err
}

// ReadDHCPLeaseObservations returns the current named lease observations,
// including revoked/expired entries required to fence stale lease-file recovery.
// History retention never prunes these functional recovery records.
func (s *SQLiteStore) ReadDHCPLeaseObservations(ctx context.Context) ([]daemonapi.DHCPLeaseUpdate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, sql.ErrConnDone
	}
	rows, err := s.db.QueryContext(ctx, `SELECT source,ip,action,client,hostname,observed_at,expires_at,infinite FROM dhcp_lease_observations ORDER BY source,ip`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []daemonapi.DHCPLeaseUpdate
	for rows.Next() {
		var lease daemonapi.DHCPLeaseUpdate
		var observed int64
		var expiry string
		var infinite int
		if err := rows.Scan(&lease.Source, &lease.IP, &lease.Action, &lease.MAC, &lease.Hostname, &observed, &expiry, &infinite); err != nil {
			return nil, err
		}
		if infinite != 0 && infinite != 1 {
			return nil, fmt.Errorf("invalid persisted DHCP infinite flag")
		}
		lease.ObservedAt = time.Unix(0, observed).UTC()
		lease.Infinite = infinite == 1
		if expiry != "" {
			lease.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiry)
			if err != nil {
				return nil, fmt.Errorf("read named DHCP expiry: %w", err)
			}
		}
		if err := lease.Validate(s.now().UTC()); err != nil {
			return nil, fmt.Errorf("read named DHCP observation: %w", err)
		}
		out = append(out, lease)
	}
	return out, rows.Err()
}
