package resource

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Ledger struct {
	Version   int        `json:"version"`
	UpdatedAt time.Time  `json:"updatedAt"`
	Artifacts []Artifact `json:"artifacts"`
}

func LoadLedger(path string) (*Ledger, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Ledger{Version: 1}, nil
		}
		return nil, err
	}
	var ledger Ledger
	if err := json.Unmarshal(data, &ledger); err != nil {
		return nil, err
	}
	if ledger.Version == 0 {
		ledger.Version = 1
	}
	return &ledger, nil
}

func (l *Ledger) Remember(artifacts []Artifact) {
	byID := map[string]Artifact{}
	for _, artifact := range l.Artifacts {
		byID[artifact.Identity()] = artifact
	}
	for _, artifact := range artifacts {
		if artifact.Owner == "" {
			continue
		}
		byID[artifact.Identity()] = artifact
	}
	l.Artifacts = l.Artifacts[:0]
	for _, artifact := range byID {
		l.Artifacts = append(l.Artifacts, artifact)
	}
	sort.Slice(l.Artifacts, func(i, j int) bool {
		return l.Artifacts[i].Identity() < l.Artifacts[j].Identity()
	})
	l.Version = 1
	l.UpdatedAt = time.Now().UTC()
}

func (l *Ledger) Forget(artifacts []Artifact) {
	remove := map[string]bool{}
	for _, artifact := range artifacts {
		remove[artifact.Identity()] = true
	}
	var kept []Artifact
	for _, artifact := range l.Artifacts {
		if !remove[artifact.Identity()] {
			kept = append(kept, artifact)
		}
	}
	l.Artifacts = kept
	l.UpdatedAt = time.Now().UTC()
}

func (l *Ledger) Owns(artifact Artifact) bool {
	for _, known := range l.Artifacts {
		if known.Identity() == artifact.Identity() {
			return true
		}
	}
	return false
}

func (l *Ledger) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}
