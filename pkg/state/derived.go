// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

var ErrDerivedConflict = errors.New("DerivedEvent checkpoint changed")

// DerivedCheckpoint is the committed latch and pending deadline. It lives in
// the existing object's status, in the same transaction as its output events.
// Revision is stored as a decimal string so status JSON cannot round it.
type DerivedCheckpoint struct {
	Definition      string
	Revision        uint64
	Initialized     bool
	Asserted        bool
	PendingTarget   bool
	PendingDue      time.Time
	LastAssertedAt  time.Time
	LastRetractedAt time.Time
}

func DecodeDerivedCheckpoint(status map[string]any) (DerivedCheckpoint, error) {
	var cp DerivedCheckpoint
	version, found := status["derivedVersion"]
	if !found {
		for _, key := range []string{"definition", "revision", "initialized", "asserted", "pendingTarget", "pendingDueAt", "lastAssertedAt", "lastRetractedAt"} {
			if _, exists := status[key]; exists {
				return cp, fmt.Errorf("DerivedEvent status has no supported checkpoint")
			}
		}
		return cp, nil
	}
	if version != "1" {
		return cp, fmt.Errorf("unsupported DerivedEvent checkpoint")
	}
	var ok bool
	if cp.Definition, ok = status["definition"].(string); !ok || cp.Definition == "" {
		return cp, fmt.Errorf("invalid DerivedEvent definition")
	}
	digest, err := hex.DecodeString(cp.Definition)
	if err != nil || len(digest) != 32 {
		return cp, fmt.Errorf("invalid DerivedEvent definition")
	}
	revision, ok := status["revision"].(string)
	if !ok {
		return cp, fmt.Errorf("invalid DerivedEvent revision")
	}
	if cp.Revision, err = strconv.ParseUint(revision, 10, 64); err != nil || cp.Revision == 0 {
		return cp, fmt.Errorf("invalid DerivedEvent revision")
	}
	if cp.Initialized, ok = status["initialized"].(bool); !ok || !cp.Initialized {
		return cp, fmt.Errorf("invalid DerivedEvent initialization")
	}
	if cp.Asserted, ok = status["asserted"].(bool); !ok {
		return cp, fmt.Errorf("invalid DerivedEvent latch")
	}
	if cp.PendingTarget, ok = status["pendingTarget"].(bool); !ok {
		return cp, fmt.Errorf("invalid DerivedEvent pending target")
	}
	for key, target := range map[string]*time.Time{"pendingDueAt": &cp.PendingDue, "lastAssertedAt": &cp.LastAssertedAt, "lastRetractedAt": &cp.LastRetractedAt} {
		value, exists := status[key]
		if !exists {
			continue
		}
		text, valid := value.(string)
		if !valid {
			return cp, fmt.Errorf("invalid DerivedEvent %s", key)
		}
		at, err := time.Parse(time.RFC3339Nano, text)
		if err != nil || at.IsZero() {
			return cp, fmt.Errorf("invalid DerivedEvent %s", key)
		}
		*target = at.UTC()
	}
	if cp.Asserted && cp.LastAssertedAt.IsZero() || !cp.Asserted && cp.LastRetractedAt.IsZero() {
		return cp, fmt.Errorf("DerivedEvent latch lacks a committed transition time")
	}
	if !cp.PendingDue.IsZero() && cp.PendingTarget == cp.Asserted {
		return cp, fmt.Errorf("invalid DerivedEvent pending latch")
	}
	return cp, nil
}

// CommitDerived is also used directly by the SQLite-backed controller tests.
func (s *SQLiteStore) CommitDerived(ctx context.Context, name string, expected, next DerivedCheckpoint, output *daemonapi.DaemonEvent, inputs DerivedInputs) ([]daemonapi.DaemonEvent, error) {
	return s.CommitDerivedStatus(ctx, name, expected, next, output, inputs, nil, nil)
}

// CommitDerivedStatus adds the production status decorator's declaration
// metadata and pure notification builder without creating another status owner.
func (s *SQLiteStore) CommitDerivedStatus(ctx context.Context, name string, expected, next DerivedCheckpoint, output *daemonapi.DaemonEvent, inputs DerivedInputs, metadata map[string]any, build StatusEventBuilder) ([]daemonapi.DaemonEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if name == "" || next.Definition == "" || !next.Initialized || next != expected && expected.Revision == math.MaxUint64 {
		return nil, fmt.Errorf("invalid DerivedEvent checkpoint update")
	}
	if next != expected {
		next.Revision = expected.Revision + 1
	}
	status := make(map[string]any, len(metadata)+12)
	for key, value := range metadata {
		status[key] = value
	}
	status["derivedVersion"], status["definition"], status["revision"] = "1", next.Definition, strconv.FormatUint(next.Revision, 10)
	status["initialized"], status["asserted"], status["pendingTarget"] = next.Initialized, next.Asserted, next.PendingTarget
	status["phase"], status["pendingTransition"] = "Retracted", "None"
	if next.Asserted {
		status["phase"] = "Asserted"
	}
	if !next.PendingDue.IsZero() {
		status["phase"], status["pendingTransition"] = "Pending", "Retracting"
		if next.PendingTarget {
			status["pendingTransition"] = "Asserting"
		}
	}
	for key, at := range map[string]time.Time{"pendingDueAt": next.PendingDue, "lastAssertedAt": next.LastAssertedAt, "lastRetractedAt": next.LastRetractedAt} {
		if !at.IsZero() {
			status[key] = at.UTC().Format(time.RFC3339Nano)
		}
	}
	if _, err := DecodeDerivedCheckpoint(status); err != nil {
		return nil, err
	}
	var rejected error
	events, err := s.writeObjectStatusAndEvents(api.NetAPIVersion, "DerivedEvent", name, status, func(current, status map[string]any) (bool, []daemonapi.DaemonEvent) {
		if rejected = ctx.Err(); rejected != nil {
			return false, nil
		}
		cp, err := DecodeDerivedCheckpoint(current)
		if err != nil {
			rejected = err
			return false, nil
		}
		if cp != expected {
			rejected = ErrDerivedConflict
			return false, nil
		}
		var events []daemonapi.DaemonEvent
		if build != nil {
			write, event := build(current, status)
			if !write {
				rejected = ErrDerivedConflict
				return false, nil
			}
			if event != nil {
				events = append(events, *event)
			}
		}
		if output != nil {
			events = append(events, *output)
		}
		return true, events
	}, false, nil, 2, &inputs)
	if err != nil {
		return nil, err
	}
	if rejected != nil {
		return nil, rejected
	}
	return events, nil
}

// DerivedInputRef is a configured scalar status field, decoded once by the
// DerivedEvent owner. Values are converted at this persistence boundary.
type DerivedInputRef struct{ Kind, Name, Field string }
type DerivedInputVersion struct {
	Kind, Name string
	Version    int64
	Found      bool
}
type DerivedInputs struct {
	References []DerivedInputRef
	Values     []string
	Versions   []DerivedInputVersion
}

// ReadDerivedInputs collects all requested objects from one SQLite read
// transaction. Time-dependent Health and PD projections use one captured time.
func (s *SQLiteStore) ReadDerivedInputs(ctx context.Context, refs []DerivedInputRef) (DerivedInputs, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return DerivedInputs{}, sql.ErrConnDone
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return DerivedInputs{}, err
	}
	defer tx.Rollback()
	inputs, err := s.readDerivedInputs(ctx, tx, refs, s.now().UTC())
	if err != nil {
		return inputs, err
	}
	return inputs, tx.Commit()
}

func (s *SQLiteStore) readDerivedInputs(ctx context.Context, tx *sql.Tx, refs []DerivedInputRef, now time.Time) (DerivedInputs, error) {
	inputs := DerivedInputs{References: append([]DerivedInputRef(nil), refs...), Values: make([]string, len(refs))}
	statuses := map[string]map[string]any{}
	for i, ref := range refs {
		if ref.Kind == "" || ref.Name == "" || ref.Field == "" {
			return inputs, fmt.Errorf("invalid DerivedEvent status reference")
		}
		key := ref.Kind + "/" + ref.Name
		status, found := statuses[key]
		if !found {
			version := DerivedInputVersion{Kind: ref.Kind, Name: ref.Name}
			var raw string
			err := tx.QueryRowContext(ctx, `SELECT resource_version,coalesce(status,'{}') FROM objects WHERE api_version=? AND kind=? AND name=?`, api.NetAPIVersion, ref.Kind, ref.Name).Scan(&version.Version, &raw)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return inputs, err
			}
			version.Found = err == nil
			status = map[string]any{}
			if version.Found {
				if err := json.Unmarshal([]byte(raw), &status); err != nil {
					return inputs, fmt.Errorf("decode %s: %w", key, err)
				}
				if status == nil {
					return inputs, fmt.Errorf("invalid %s status", key)
				}
				if ref.Kind == "HealthCheck" {
					if err := projectHealthStatus(status, raw, now); err != nil {
						return inputs, err
					}
				}
				if ref.Kind == "DHCPv6PrefixDelegation" {
					if err := projectPDStatus(status, raw, now); err != nil {
						return inputs, err
					}
				}
			}
			statuses[key] = status
			inputs.Versions = append(inputs.Versions, version)
		}
		inputs.Values[i] = derivedInputValue(status[ref.Field])
	}
	return inputs, nil
}

func (s *SQLiteStore) validateDerivedInputs(ctx context.Context, tx *sql.Tx, expected DerivedInputs, now time.Time) error {
	current, err := s.readDerivedInputs(ctx, tx, expected.References, now)
	if err != nil {
		return err
	}
	if len(current.Values) != len(expected.Values) || len(current.Versions) != len(expected.Versions) {
		return ErrDerivedConflict
	}
	for i, value := range current.Values {
		if value != expected.Values[i] {
			return ErrDerivedConflict
		}
	}
	for i, version := range current.Versions {
		if version != expected.Versions[i] {
			return ErrDerivedConflict
		}
	}
	return nil
}

func derivedInputValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []string:
		data, _ := json.Marshal(typed)
		return string(data)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, fmt.Sprint(item))
		}
		data, _ := json.Marshal(out)
		return string(data)
	default:
		return fmt.Sprint(value)
	}
}

// RetireDerivedEvents is called only after a generation is selected. Removing
// an intent ends that latch/deadline's lifetime; a later re-add starts fresh.
// Diagnostic event history remains in the journal.
func (s *SQLiteStore) RetireDerivedEvents(ctx context.Context, names []string) error {
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
	present := make(map[string]bool, len(names))
	for _, name := range names {
		present[name] = true
	}
	rows, err := tx.QueryContext(ctx, `SELECT name FROM objects WHERE api_version=? AND kind='DerivedEvent'`, api.NetAPIVersion)
	if err != nil {
		return err
	}
	var removed []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		if !present[name] {
			removed = append(removed, name)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, name := range removed {
		if _, err := tx.ExecContext(ctx, `DELETE FROM objects WHERE api_version=? AND kind='DerivedEvent' AND name=?`, api.NetAPIVersion, name); err != nil {
			return err
		}
	}
	return tx.Commit()
}
