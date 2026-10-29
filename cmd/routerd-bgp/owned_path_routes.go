// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/imksoo/routerd/pkg/bgpdaemon"
)

func decodeControlRequest(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func registerOwnedRoutes(mux *http.ServeMux, statePath string, paths policyOwnerServer, lock sync.Locker) {
	mux.HandleFunc("/v1/policy", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var intent bgpdaemon.PolicyIntent
		if err := decodeControlRequest(w, r, &intent); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := bgpdaemon.ValidatePolicyIntent(intent); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		lock.Lock()
		defer lock.Unlock()
		applied, found, err := bgpdaemon.ReadApplied(statePath)
		if err == nil && !found {
			err = fmt.Errorf("applied BGP config is not initialized")
		}
		if err == nil {
			applied, err = reconcilePolicyConfig(r.Context(), paths, statePath, applied, intent)
		}
		if err == nil {
			sources := map[string]bool{}
			for _, group := range [][]bgpdaemon.AppliedPath{applied.PendingPathAdds, applied.PendingPathRemovals} {
				for _, path := range group {
					if bgpdaemon.IsMobilityPathSource(path.Source) {
						sources[path.Source] = true
					}
				}
			}
			for source := range sources {
				applied, err = recoverPendingPaths(r.Context(), paths, statePath, applied, source)
				if err != nil {
					break
				}
			}
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, applied)
	})
	mux.HandleFunc("/v1/static-paths", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var desired []bgpdaemon.AppliedPath
		if err := decodeControlRequest(w, r, &desired); err != nil || desired == nil {
			http.Error(w, "static paths must be one JSON array", http.StatusBadRequest)
			return
		}
		for _, path := range desired {
			if path.Source != bgpdaemon.AppliedPathSourceStatic {
				http.Error(w, "static request contains another path owner", http.StatusBadRequest)
				return
			}
			if err := bgpdaemon.ValidateAppliedPath(path); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		lock.Lock()
		defer lock.Unlock()
		applied, err := reconcileStaticPaths(r.Context(), paths, statePath, desired)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, applied)
	})
}

func reconcileStaticPaths(ctx context.Context, server policyOwnerServer, statePath string, desired []bgpdaemon.AppliedPath) (bgpdaemon.AppliedConfig, error) {
	applied, found, err := bgpdaemon.ReadApplied(statePath)
	if err != nil {
		return applied, err
	}
	if !found {
		return applied, fmt.Errorf("applied BGP config is not initialized")
	}
	wanted := map[string]bool{}
	for _, path := range desired {
		wanted[bgpdaemon.AppliedPathKey(path)] = true
	}
	for _, group := range [][]bgpdaemon.AppliedPath{applied.Paths, applied.PendingPathAdds, applied.PendingPathRemovals} {
		for _, path := range group {
			if path.Source == bgpdaemon.AppliedPathSourceStatic && !wanted[bgpdaemon.AppliedPathKey(path)] {
				applied, err = deleteOwnedPath(ctx, server, statePath, path)
				if err != nil {
					return applied, err
				}
			}
		}
	}
	for _, path := range desired {
		applied, _, err = upsertOwnedPath(ctx, server, statePath, path)
		if err != nil {
			return applied, err
		}
	}
	return applied, nil
}
