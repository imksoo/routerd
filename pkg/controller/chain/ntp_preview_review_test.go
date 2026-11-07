// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestNTPPreviewDoesNotPublishAnUnexecutedApplication(t *testing.T) {
	for _, kind := range []string{"NTPClient", "NTPServer"} {
		t.Run(kind, func(t *testing.T) {
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			path := filepath.Join(t.TempDir(), "ntp.conf")
			effects := 0
			command := func(context.Context, string, ...string) ([]byte, error) {
				effects++
				return nil, errors.New("preview reached a provider command")
			}
			var router *api.Router
			if kind == "NTPClient" {
				router = ntpRouter(api.NTPClientSpec{Provider: "chrony", Managed: true, Servers: []string{"192.0.2.123"}})
				err = (NTPClientController{Router: router, DeclaredRouter: router, Store: store, ConfigPath: path, Command: command, DryRun: true}).Reconcile(t.Context())
			} else {
				router = ntpServerRouter(api.NTPServerSpec{Provider: "chrony", Managed: true, Servers: []string{"192.0.2.123"}, AllowCIDRs: []string{"192.0.2.0/24"}})
				err = (NTPServerController{Router: router, DeclaredRouter: router, Store: store, ConfigPath: path, Command: command, DryRun: true}).Reconcile(t.Context())
			}
			if err != nil {
				t.Fatal(err)
			}
			if effects != 0 {
				t.Fatalf("preview executed %d commands", effects)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("preview config existence: %v", err)
			}
			records, err := (ntpConfigOwner{store: store, kind: kind}).read()
			if err != nil || len(records) != 0 {
				t.Fatalf("preview acquired private ownership: %+v %v", records, err)
			}
			status, err := store.ReadObjectStatus(api.SystemAPIVersion, kind, router.Spec.Resources[0].Metadata.Name)
			if err != nil {
				t.Fatal(err)
			}
			if status["phase"] != "Rendered" || status["dryRun"] != true {
				t.Fatalf("unexecuted preview status=%+v", status)
			}
		})
	}
}
