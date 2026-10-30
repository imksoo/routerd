// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

var ErrEventRuleConflict = errors.New("EventRule checkpoint changed")
var ErrEventRuleHistoryGap = errors.New("EventRule input history is no longer available")

const maxEventRuleCheckpointBytes = 8 << 20

type EventRuleDefinition struct{ Name, Digest string }
type EventRuleTimer struct {
	Due   time.Time             `json:"due"`
	Input daemonapi.DaemonEvent `json:"input"`
}
type EventRuleCorrelation struct {
	Latest   map[string]bool `json:"latest,omitempty"`
	Sequence int             `json:"sequence,omitempty"`
	Window   []time.Time     `json:"window,omitempty"`
	LastEmit time.Time       `json:"lastEmit,omitempty"`
	Count    int             `json:"count,omitempty"`
	Timer    *EventRuleTimer `json:"timer,omitempty"`
	LastSeen time.Time       `json:"lastSeen"`
}

// Cursor has exactly one durable representation: event_consumer_cursors. It is
// joined into this typed observation, never encoded into the private object.
type EventRuleCheckpoint struct {
	Version      int                             `json:"version"`
	Name         string                          `json:"name"`
	Definition   string                          `json:"definition"`
	Revision     int64                           `json:"revision"`
	Cursor       int64                           `json:"-"`
	Correlations map[string]EventRuleCorrelation `json:"correlations"`
	FireCount    int                             `json:"fireCount"`
	WarningCount int                             `json:"warningCount"`
	LastFiredAt  time.Time                       `json:"lastFiredAt"`
}

func EventRuleConsumer(name string) string { return "eventrule/rule/" + name }
func eventRuleObject(name string) string {
	return fmt.Sprintf("eventrule-%x", sha256.Sum256([]byte(name)))
}

func decodeEventRuleCheckpoint(raw string) (EventRuleCheckpoint, error) {
	var cp EventRuleCheckpoint
	if len(raw) > maxEventRuleCheckpointBytes {
		return cp, fmt.Errorf("EventRule checkpoint exceeds its storage bound")
	}
	d := json.NewDecoder(bytes.NewBufferString(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&cp); err != nil {
		return cp, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return cp, fmt.Errorf("EventRule checkpoint requires one object")
	}
	if cp.Version != 1 || cp.Name == "" || cp.Definition == "" || cp.Revision < 0 || cp.FireCount < 0 || cp.WarningCount < 0 || cp.Correlations == nil || len(cp.Correlations) > 4096 {
		return cp, fmt.Errorf("invalid EventRule checkpoint")
	}
	for _, c := range cp.Correlations {
		if c.Sequence < 0 || c.Count < 0 || c.LastSeen.IsZero() || c.Timer != nil && c.Timer.Due.IsZero() {
			return cp, fmt.Errorf("invalid EventRule correlation checkpoint")
		}
	}
	return cp, nil
}

// InitializeEventRules runs only for the selected generation. Removed rule
// checkpoints and their cursors retire together, including a zero-rule config.
func (s *SQLiteStore) InitializeEventRules(ctx context.Context, definitions []EventRuleDefinition, initial int64) error {
	if initial < 0 {
		return fmt.Errorf("invalid EventRule input position")
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
	present := map[string]EventRuleDefinition{}
	for _, d := range definitions {
		if d.Name == "" || d.Digest == "" {
			return fmt.Errorf("invalid EventRule declaration")
		}
		if _, ok := present[d.Name]; ok {
			return fmt.Errorf("duplicate EventRule %q", d.Name)
		}
		present[d.Name] = d
	}
	rows, err := tx.QueryContext(ctx, `SELECT name,status FROM objects WHERE api_version=? AND kind='Router' AND name LIKE 'eventrule-%'`, api.RouterAPIVersion)
	if err != nil {
		return err
	}
	existing := map[string]EventRuleCheckpoint{}
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			rows.Close()
			return err
		}
		cp, err := decodeEventRuleCheckpoint(raw)
		if err != nil {
			rows.Close()
			return err
		}
		if key != eventRuleObject(cp.Name) {
			rows.Close()
			return fmt.Errorf("EventRule checkpoint key does not match its owner")
		}
		existing[cp.Name] = cp
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for name, cp := range existing {
		d, keep := present[name]
		if keep && cp.Definition == d.Digest {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM objects WHERE api_version=? AND kind='Router' AND name=?`, api.RouterAPIVersion, eventRuleObject(name)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM event_consumer_cursors WHERE consumer=?`, EventRuleConsumer(name)); err != nil {
			return err
		}
		delete(existing, name)
	}
	for _, d := range definitions {
		if _, ok := existing[d.Name]; ok {
			var cursor int64
			if err := tx.QueryRowContext(ctx, `SELECT cursor FROM event_consumer_cursors WHERE consumer=?`, EventRuleConsumer(d.Name)).Scan(&cursor); err != nil {
				return fmt.Errorf("EventRule checkpoint cursor: %w", err)
			}
			continue
		}
		cp := EventRuleCheckpoint{Version: 1, Name: d.Name, Definition: d.Digest, Correlations: map[string]EventRuleCorrelation{}}
		data, err := json.Marshal(cp)
		if err != nil {
			return err
		}
		if err := writeEventRuleObject(ctx, tx, s.generation, s.now().UTC(), api.RouterAPIVersion, "Router", eventRuleObject(d.Name), data); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO event_consumer_cursors(consumer,cursor,updated_at) VALUES(?,?,?)`, EventRuleConsumer(d.Name), initial, formatStateTime(s.now().UTC())); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func readEventRule(ctx context.Context, tx *sql.Tx, name string) (EventRuleCheckpoint, error) {
	var raw string
	var cursor int64
	err := tx.QueryRowContext(ctx, `SELECT o.status,c.cursor FROM objects o JOIN event_consumer_cursors c ON c.consumer=? WHERE o.api_version=? AND o.kind='Router' AND o.name=?`, EventRuleConsumer(name), api.RouterAPIVersion, eventRuleObject(name)).Scan(&raw, &cursor)
	if err != nil {
		return EventRuleCheckpoint{}, err
	}
	cp, err := decodeEventRuleCheckpoint(raw)
	if err != nil {
		return cp, err
	}
	if cp.Name != name || cursor < 0 {
		return cp, fmt.Errorf("EventRule checkpoint identity/cursor mismatch")
	}
	cp.Cursor = cursor
	return cp, nil
}

func (s *SQLiteStore) ReadEventRule(ctx context.Context, name string) (EventRuleCheckpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return EventRuleCheckpoint{}, sql.ErrConnDone
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return EventRuleCheckpoint{}, err
	}
	defer tx.Rollback()
	return readEventRule(ctx, tx, name)
}

// EventRuleInputs checks the complete ordered journal rather than interpreting
// retention as the absence of an expected event. The finite journal stays finite.
func (s *SQLiteStore) EventRuleInputs(ctx context.Context, cursor int64) ([]StoredEvent, int64, error) {
	if cursor < 0 {
		return nil, 0, fmt.Errorf("invalid EventRule cursor")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, 0, sql.ErrConnDone
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var tip int64
	if err := tx.QueryRowContext(ctx, `SELECT coalesce((SELECT seq FROM sqlite_sequence WHERE name='events'),0)`).Scan(&tip); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,created_at,coalesce(nullif(topic,''),type),coalesce(source_kind,''),coalesce(source_instance,''),coalesce(resource_api_version,''),coalesce(resource_kind,''),coalesce(resource_name,''),coalesce(severity,''),reason,message,coalesce(attributes,'{}') FROM events WHERE id>? ORDER BY id LIMIT 256`, cursor)
	if err != nil {
		return nil, tip, err
	}
	defer rows.Close()
	var out []StoredEvent
	next := cursor + 1
	for rows.Next() {
		var e StoredEvent
		var created, attrs string
		if err := rows.Scan(&e.ID, &created, &e.Topic, &e.SourceKind, &e.SourceInstance, &e.ResourceAPIVersion, &e.ResourceKind, &e.ResourceName, &e.Severity, &e.Reason, &e.Message, &attrs); err != nil {
			return nil, tip, err
		}
		if e.ID != next {
			return nil, tip, ErrEventRuleHistoryGap
		}
		next++
		e.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, tip, err
		}
		if err := json.Unmarshal([]byte(attrs), &e.Attributes); err != nil {
			return nil, tip, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, tip, err
	}
	if cursor > tip || len(out) == 0 && cursor < tip {
		return nil, tip, ErrEventRuleHistoryGap
	}
	return out, tip, nil
}

func writeEventRuleObject(ctx context.Context, tx *sql.Tx, generation int64, now time.Time, version, kind, name string, data []byte) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO objects(api_version,kind,name,uid,resource_version,observed_generation,status,created_at,modified_at) VALUES(?,?,?,?,1,?,?,?,?) ON CONFLICT(api_version,kind,name) DO UPDATE SET resource_version=resource_version+1,observed_generation=excluded.observed_generation,status=excluded.status,modified_at=excluded.modified_at`, version, kind, name, version+"/"+kind+"/"+name, nullGeneration(generation), string(data), formatStateTime(now), formatStateTime(now))
	return err
}

// CommitEventRule owns the checkpoint, sole cursor, public result and both
// required output/status journal entries in one SQLite transaction. The builder
// is the existing pure status-notification collaborator; it cannot call Store.
func (s *SQLiteStore) CommitEventRule(ctx context.Context, expected EventRuleCheckpoint, next EventRuleCheckpoint, output *daemonapi.DaemonEvent, atTip bool, build StatusEventBuilder) (EventRuleCheckpoint, []daemonapi.DaemonEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return next, nil, sql.ErrConnDone
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return next, nil, err
	}
	defer tx.Rollback()
	current, err := readEventRule(ctx, tx, expected.Name)
	if err != nil {
		return next, nil, err
	}
	if current.Revision != expected.Revision || current.Cursor != expected.Cursor || current.Definition != expected.Definition || next.Name != current.Name || next.Definition != current.Definition || next.Cursor < current.Cursor {
		return next, nil, ErrEventRuleConflict
	}
	if next.Cursor != current.Cursor && next.Cursor != current.Cursor+1 || atTip && next.Cursor != current.Cursor {
		return next, nil, ErrEventRuleConflict
	}
	if atTip {
		var tip int64
		if err := tx.QueryRowContext(ctx, `SELECT coalesce((SELECT seq FROM sqlite_sequence WHERE name='events'),0)`).Scan(&tip); err != nil {
			return next, nil, err
		}
		if tip != current.Cursor {
			return next, nil, ErrEventRuleConflict
		}
	}
	next.Version = 1
	next.Revision = current.Revision + 1
	data, err := json.Marshal(next)
	if err != nil {
		return next, nil, err
	}
	if _, err := decodeEventRuleCheckpoint(string(data)); err != nil {
		return next, nil, err
	}
	status := map[string]any{"phase": "Active", "fireCount": next.FireCount, "warningCount": next.WarningCount}
	if !next.LastFiredAt.IsZero() {
		status["lastFiredAt"] = next.LastFiredAt.UTC().Format(time.RFC3339Nano)
	}
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT status FROM objects WHERE api_version=? AND kind='EventRule' AND name=?`, api.NetAPIVersion, next.Name).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return next, nil, err
	}
	previous := map[string]any{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &previous); err != nil {
			return next, nil, err
		}
		if previous == nil {
			return next, nil, fmt.Errorf("invalid EventRule public status")
		}
	}
	var events []daemonapi.DaemonEvent
	if build != nil {
		write, event := build(previous, status)
		if !write {
			return next, nil, ErrEventRuleConflict
		}
		if event != nil {
			events = append(events, *event)
		}
	}
	if output != nil {
		events = append(events, *output)
	}
	var incoming int64
	for i, event := range events {
		if event.Resource == nil || event.Resource.APIVersion != api.NetAPIVersion || event.Resource.Kind != "EventRule" || event.Resource.Name != next.Name {
			return next, nil, fmt.Errorf("EventRule event owner mismatch")
		}
		bounded, attrs := boundedJournalEvent(event)
		events[i] = bounded
		incoming += busEventJournalPayloadBytes(bounded, attrs)
	}
	usage, err := s.prepareEventJournalBatchLocked(tx, int64(len(events)), incoming)
	if err != nil {
		return next, nil, err
	}
	now := s.now().UTC()
	if err := writeEventRuleObject(ctx, tx, s.generation, now, api.RouterAPIVersion, "Router", eventRuleObject(next.Name), data); err != nil {
		return next, nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE event_consumer_cursors SET cursor=?,updated_at=? WHERE consumer=?`, next.Cursor, formatStateTime(now), EventRuleConsumer(next.Name)); err != nil {
		return next, nil, err
	}
	public, err := json.Marshal(status)
	if err != nil {
		return next, nil, err
	}
	if err := writeEventRuleObject(ctx, tx, s.generation, now, api.NetAPIVersion, "EventRule", next.Name, public); err != nil {
		return next, nil, err
	}
	var bytes int64
	for i, event := range events {
		if event.Time.IsZero() {
			event.Time = now
		}
		cursor, n, err := recordBusEvent(tx, s.generation, now, event)
		if err != nil {
			return next, nil, err
		}
		event.Cursor = cursor
		events[i] = event
		bytes += n
	}
	if err := ctx.Err(); err != nil {
		return next, nil, err
	}
	if err := tx.Commit(); err != nil {
		return next, nil, err
	}
	s.applyEventJournalUsageLocked(usage)
	s.eventJournalRows += int64(len(events))
	s.eventJournalPayloadBytes += bytes
	s.statusWriteCount++
	s.incrementStatusKindWriteLocked("EventRule")
	return next, events, nil
}
