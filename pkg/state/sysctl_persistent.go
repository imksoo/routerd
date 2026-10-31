// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

const SysctlPersistentOwnerKey = "sysctlPersistentOwner"

var ErrSysctlPersistentOwnerChanged = errors.New("persistent sysctl receipt changed")

func SysctlPersistentOwnerName(resource, key string) string {
	return "@persistent/" + resource + "/" + key
}

type SysctlPersistentOwner struct {
	ResourceName  string `json:"resourceName"`
	Version       int    `json:"version"`
	Token         string `json:"token"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	Key           string `json:"key"`
	Block         string `json:"block"`
	PreviousBlock string `json:"previousBlock,omitempty"`
	Pending       bool   `json:"pending"`
	Withdrawing   bool   `json:"withdrawing"`
}

// SaveSysctlPersistentOwner compares the concrete receipt inside the SQLite
// writer transaction. A final release may retire the declaration display only
// when no other persistent key still owns it.
func (s *SQLiteStore) SaveSysctlPersistentOwner(ctx context.Context, kind, name string, previous, next *SysctlPersistentOwner, retireResource bool) error {
	if kind != "Sysctl" && kind != "SysctlProfile" {
		return errors.New("unsupported persistent sysctl owner kind")
	}
	if previous == nil && next == nil {
		return errors.New("persistent sysctl transition has no owner")
	}
	if retireResource && (previous == nil || next != nil) {
		return errors.New("resource release requires a proven persistent owner retirement")
	}
	for _, owner := range []*SysctlPersistentOwner{previous, next} {
		if owner != nil && (owner.Kind != kind || owner.Name != name || owner.Version != 1 || owner.ResourceName == "" || owner.Key == "" || owner.Name != SysctlPersistentOwnerName(owner.ResourceName, owner.Key) || strings.ContainsAny(owner.ResourceName+owner.Key, "/\r\n")) {
			return errors.New("persistent sysctl owner does not match its status identity")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return sql.ErrConnDone
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	err = tx.QueryRowContext(ctx, "SELECT coalesce(status,'{}') FROM objects WHERE api_version=? AND kind=? AND name=?", api.SystemAPIVersion, kind, name).Scan(&raw)
	exists := !errors.Is(err, sql.ErrNoRows)
	if err != nil && exists {
		return err
	}
	status := map[string]json.RawMessage{}
	if exists {
		if err := json.Unmarshal([]byte(raw), &status); err != nil {
			return err
		}
		if status == nil {
			return errors.New("persistent sysctl status is not an object")
		}
	}
	var current *SysctlPersistentOwner
	if encoded, found := status[SysctlPersistentOwnerKey]; found {
		var decoded SysctlPersistentOwner
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&decoded); err != nil {
			return err
		}
		if decoded.Version != 1 || decoded.Kind != kind || decoded.Name != name {
			return errors.New("stored persistent sysctl owner identity is unknown")
		}
		current = &decoded
	}
	if (current == nil) != (previous == nil) || (previous != nil && *current != *previous) {
		return ErrSysctlPersistentOwnerChanged
	}
	if next == nil {
		delete(status, SysctlPersistentOwnerKey)
		delete(status, "previousValue")
		status["changed"] = json.RawMessage("false")
	} else {
		encoded, err := json.Marshal(next)
		if err != nil {
			return err
		}
		status[SysctlPersistentOwnerKey] = encoded
	}
	delete(status, "phase")
	now := s.now().UTC().Format(time.RFC3339Nano)
	status["updatedAt"], _ = json.Marshal(now)
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if next == nil {
		_, err = tx.ExecContext(ctx, "DELETE FROM objects WHERE api_version=? AND kind=? AND name=?", api.SystemAPIVersion, kind, name)
	} else if exists {
		_, err = tx.ExecContext(ctx, "UPDATE objects SET resource_version=resource_version+1,observed_generation=?,status=?,modified_at=? WHERE api_version=? AND kind=? AND name=?", nullGeneration(s.generation), string(data), now, api.SystemAPIVersion, kind, name)
	} else {
		_, err = tx.ExecContext(ctx, "INSERT INTO objects(api_version,kind,name,uid,resource_version,observed_generation,status,created_at,modified_at) VALUES(?,?,?,?,1,?,?,?,?)",
			api.SystemAPIVersion, kind, name, api.SystemAPIVersion+"/"+kind+"/"+name, nullGeneration(s.generation), string(data), now, now)
	}
	if err != nil {
		return s.noteStorageWriteErrorLocked(err)
	}
	if retireResource {
		var otherOwners int
		prefix := SysctlPersistentOwnerName(previous.ResourceName, "")
		err := tx.QueryRowContext(ctx, "SELECT count(*) FROM objects WHERE api_version=? AND kind=? AND name!=? AND instr(name,?)=1 AND json_type(status,?) IS NOT NULL",
			api.SystemAPIVersion, kind, name, prefix, "$."+SysctlPersistentOwnerKey).Scan(&otherOwners)
		if err != nil {
			return err
		}
		if otherOwners == 0 {
			_, err = tx.ExecContext(ctx, "UPDATE objects SET resource_version=resource_version+1,observed_generation=?,status=json_set(coalesce(status,'{}'),'$.phase','Removed','$.reason','RemovedFromConfig','$.persistent',json('false'),'$.updatedAt',?),modified_at=? WHERE api_version=? AND kind=? AND name=?",
				nullGeneration(s.generation), now, now, api.SystemAPIVersion, kind, previous.ResourceName)
			if err != nil {
				return s.noteStorageWriteErrorLocked(err)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return s.noteStorageWriteErrorLocked(err)
	}
	s.statusWriteCount++
	s.incrementStatusKindWriteLocked(kind)
	return nil
}
