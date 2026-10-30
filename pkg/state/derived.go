// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
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
		if _, old := status["asserted"]; old {
			return cp, fmt.Errorf("DerivedEvent status has no supported checkpoint")
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
	revision, ok := status["revision"].(string)
	if !ok {
		return cp, fmt.Errorf("invalid DerivedEvent revision")
	}
	var err error
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
	if !cp.PendingDue.IsZero() && cp.PendingTarget == cp.Asserted {
		return cp, fmt.Errorf("invalid DerivedEvent pending latch")
	}
	return cp, nil
}

// CommitDerived is also used directly by the SQLite-backed controller tests.
func (s *SQLiteStore) CommitDerived(ctx context.Context, name string, expected, next DerivedCheckpoint, output *daemonapi.DaemonEvent) ([]daemonapi.DaemonEvent, error) {
	return s.CommitDerivedStatus(ctx, name, expected, next, output, nil, nil)
}

// CommitDerivedStatus adds the production status decorator's declaration
// metadata and pure notification builder without creating another status owner.
func (s *SQLiteStore) CommitDerivedStatus(ctx context.Context, name string, expected, next DerivedCheckpoint, output *daemonapi.DaemonEvent, metadata map[string]any, build StatusEventBuilder) ([]daemonapi.DaemonEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if name == "" || next.Definition == "" || !next.Initialized || expected.Revision == math.MaxUint64 {
		return nil, fmt.Errorf("invalid DerivedEvent checkpoint update")
	}
	next.Revision = expected.Revision + 1
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
	events, err := s.SaveObjectStatusAndEvents(api.NetAPIVersion, "DerivedEvent", name, status, func(current, status map[string]any) (bool, []daemonapi.DaemonEvent) {
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
	})
	if err != nil {
		return nil, err
	}
	if rejected != nil {
		return nil, rejected
	}
	return events, nil
}
