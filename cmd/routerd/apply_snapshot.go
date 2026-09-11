// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"gopkg.in/yaml.v3"
)

// applySnapshotManifest describes the acquired evaluation inputs, never their
// secret-bearing payloads. EvaluatedAt is the reference time for comparing the
// pure dynamic planner; individual controllers still reevaluate TTL/ownership
// at execution and may refresh their own local dry-run state.
type applySnapshotManifest struct {
	CanonicalHash  string              `json:"canonicalHash"`
	EvaluatedAt    time.Time           `json:"evaluatedAt"`
	TargetOS       platform.OS         `json:"targetOS"`
	Inputs         []string            `json:"inputs"`
	ExcludedInputs []string            `json:"excludedInputs"`
	Consistency    string              `json:"consistency"`
	DynamicParts   []applySnapshotPart `json:"dynamicParts"`
}

type applySnapshotPart struct {
	Source     string    `json:"source"`
	Generation int64     `json:"generation"`
	Digest     string    `json:"digest"`
	ObservedAt time.Time `json:"observedAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	Status     string    `json:"status"`
}

func describeApplySnapshot(candidate *api.Router, store *routerstate.SQLiteStore, now time.Time, targetOS platform.OS) (applySnapshotManifest, error) {
	canonical, err := yaml.Marshal(candidate)
	if err != nil {
		return applySnapshotManifest{}, err
	}
	hash, err := config.NormalizedYAMLHash(canonical)
	if err != nil {
		return applySnapshotManifest{}, err
	}
	parts, err := store.ListDynamicConfigParts()
	if err != nil {
		return applySnapshotManifest{}, err
	}
	manifest := applySnapshotManifest{
		CanonicalHash: hash, EvaluatedAt: now.UTC(), TargetOS: targetOS,
		Inputs:         []string{"object-status", "state-variables", "dynamic-config-parts"},
		ExcludedInputs: []string{"event-history", "event-cursors", "federation-history", "action-executions", "jobs", "generations", "host-observations"},
		Consistency:    "SQLite inputs are acquired in one read transaction; controller evaluation and host observations are not frozen",
		DynamicParts:   make([]applySnapshotPart, 0, len(parts)),
	}
	for _, part := range parts {
		manifest.DynamicParts = append(manifest.DynamicParts, applySnapshotPart{Source: part.Source, Generation: part.Generation, Digest: part.Digest, ObservedAt: part.ObservedAt, ExpiresAt: part.ExpiresAt, Status: part.Status})
	}
	return manifest, nil
}
