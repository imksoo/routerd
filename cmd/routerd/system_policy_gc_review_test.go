// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestReviewStatusGCCannotForgetUnparsedSysctlPersistentOwner(t *testing.T) {
	for _, kind := range []string{"Sysctl", "SysctlProfile"} {
		t.Run(kind, func(t *testing.T) {
			statePath := filepath.Join(t.TempDir(), "state.db")
			store, err := routerstate.OpenSQLite(statePath)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			name := "removed-setting"
			router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "gc-review"}}
			if kind == "SysctlProfile" {
				name = "profile-net.inet.ip.forwarding"
				router.Spec.Resources = []api.Resource{{
					TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "SysctlProfile"}, Metadata: api.ObjectMeta{Name: "profile"},
					Spec: api.SysctlProfileSpec{Profile: "router-freebsd", Persistent: true},
				}}
			}
			// Unknown proof is not authority to touch a file. It is also not evidence
			// that an acquired block is absent; only the concrete owner may resolve it.
			const unknown = "unparsed proof remains the Sysctl actor's responsibility"
			if err := store.SaveObjectStatus(api.SystemAPIVersion, kind, name, map[string]any{"phase": "Pending", "managedBy": "routerd", "sysctlPersistentOwner": unknown}); err != nil {
				t.Fatal(err)
			}
			result, err := cleanupUnsupportedLegacyObjectStatusesForOS(router, store, statePath, time.Now().UTC(), nil, platform.OSFreeBSD)
			actual, readErr := store.ReadObjectStatus(api.SystemAPIVersion, kind, name)
			if err != nil || len(result.Removed) != 0 || readErr != nil || actual["sysctlPersistentOwner"] != unknown {
				t.Fatalf("general GC forgot unresolved Sysctl proof before its actor: kind=%s removed=%+v cleanupErr=%v readErr=%v status=%+v", kind, result.Removed, err, readErr, actual)
			}
		})
	}
}
