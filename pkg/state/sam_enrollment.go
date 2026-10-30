// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrSAMEnrollmentIdentityRevoked = errors.New("SAM enrollment client identity is revoked")
	ErrSAMEnrollmentClaimChanged    = errors.New("SAM enrollment claim changed during admission")
)

// SAMEnrollmentSnapshot binds the admission rows and permanent revocations to
// one read-only SQLite snapshot. Reading topology never repairs or writes state.
type SAMEnrollmentSnapshot struct {
	Parts             []DynamicConfigPartRecord
	RevokedIdentities []string
}

func (s *SQLiteStore) ReadSAMEnrollmentSnapshot(ctx context.Context) (SAMEnrollmentSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var snapshot SAMEnrollmentSnapshot
	if s.closed {
		return snapshot, sql.ErrConnDone
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return snapshot, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,source,generation,observed_at,expires_at,digest,coalesce(resources_json,''),coalesce(directives_json,''),coalesce(actionplans_json,''),coalesce(mobility_dataplane_json,''),coalesce(arp_observer_intents_json,''),coalesce(fib_verdicts_json,''),status,coalesce(error,''),created_at,updated_at FROM dynamic_config_parts ORDER BY observed_at DESC,generation DESC,id DESC`)
	if err != nil {
		return snapshot, err
	}
	snapshot.Parts, err = scanDynamicConfigPartRecords(rows)
	closeErr := rows.Close()
	if err != nil {
		return snapshot, err
	}
	if closeErr != nil {
		return snapshot, closeErr
	}
	rows, err = tx.QueryContext(ctx, `SELECT identity_digest FROM sam_enrollment_revoked_identities ORDER BY identity_digest`)
	if err != nil {
		return snapshot, err
	}
	defer rows.Close()
	for rows.Next() {
		var identity string
		if err := rows.Scan(&identity); err != nil {
			return snapshot, err
		}
		snapshot.RevokedIdentities = append(snapshot.RevokedIdentities, identity)
	}
	if err := rows.Err(); err != nil {
		return snapshot, err
	}
	if err := rows.Close(); err != nil {
		return snapshot, err
	}
	if err := tx.Commit(); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

// AcceptSAMEnrollmentClaim rechecks replay protection and the observed current
// row in the transaction which publishes the accepted claim.
func (s *SQLiteStore) AcceptSAMEnrollmentClaim(ctx context.Context, part DynamicConfigPartRecord, identityDigest, previousDigest string) error {
	return s.commitSAMEnrollmentClaim(ctx, part, identityDigest, previousDigest, false)
}

// RevokeSAMEnrollmentClaim makes expiry and the permanent identity tombstone
// visible together. Neither half survives a failed transaction.
func (s *SQLiteStore) RevokeSAMEnrollmentClaim(ctx context.Context, part DynamicConfigPartRecord, identityDigest, previousDigest string) error {
	return s.commitSAMEnrollmentClaim(ctx, part, identityDigest, previousDigest, true)
}

func (s *SQLiteStore) commitSAMEnrollmentClaim(ctx context.Context, part DynamicConfigPartRecord, identityDigest, previousDigest string, revoke bool) error {
	identityDigest = strings.TrimSpace(identityDigest)
	if !strings.HasPrefix(part.Source, "SAMEnrollmentClaim/") || strings.TrimPrefix(part.Source, "SAMEnrollmentClaim/") == "" || part.Generation != 1 || identityDigest == "" || (revoke && previousDigest == "") {
		return fmt.Errorf("SAM enrollment publication requires its claim source, identity, and current generation")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current string
	err = tx.QueryRowContext(ctx, `SELECT digest FROM dynamic_config_parts WHERE source=? AND generation=1`, part.Source).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if current != previousDigest {
		return ErrSAMEnrollmentClaimChanged
	}
	if revoke {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sam_enrollment_revoked_identities(identity_digest,claim_source,revoked_at) VALUES(?,?,?) ON CONFLICT(identity_digest) DO NOTHING`, identityDigest, part.Source, formatStateTime(part.ObservedAt)); err != nil {
			return err
		}
	} else {
		var revoked bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sam_enrollment_revoked_identities WHERE identity_digest=?)`, identityDigest).Scan(&revoked); err != nil {
			return err
		}
		if revoked {
			return ErrSAMEnrollmentIdentityRevoked
		}
	}
	if _, err := tx.ExecContext(ctx, upsertDynamicConfigPartSQL, dynamicConfigPartArguments(part, s.now().UTC())...); err != nil {
		return err
	}
	return tx.Commit()
}
