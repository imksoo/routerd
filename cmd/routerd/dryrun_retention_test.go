// SPDX-License-Identifier: BSD-3-Clause
package main

import (
	"bytes"
	"context"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/logstore"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Exercise the CLI dry-run options through the real controller chain. All
// files are temporary and only the retention controller is enabled.
func TestApplyDryRunPreservesExternalLogDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dns-queries.db")
	log, err := logstore.OpenDNSQueryLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Record(context.Background(), logstore.DNSQuery{Timestamp: time.Now().Add(-48 * time.Hour), QuestionName: "expired.example", QuestionType: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.DNSResolverSpec{QueryLog: api.DNSResolverQueryLogSpec{Enabled: true, Path: path}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogRetention"}, Metadata: api.ObjectMeta{Name: "default"}, Spec: api.LogRetentionSpec{Schedule: "daily", Vacuum: true, Signals: []string{"dnsQueries"}, Retention: "24h"}},
	}}}
	opts := applyChainControllerOptions(applyOptions{DryRun: true})
	if !opts.DryRunNTP {
		t.Fatal("apply dry-run did not disable NTP mutations")
	}
	opts.EnabledControllers = []string{"log-retention"}
	runner := controllerchain.Runner{Router: router, Bus: bus.New(), Store: store, Opts: opts}
	if err := runner.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("dry-run modified the external log database")
	}
	if got := store.ObjectStatus(api.SystemAPIVersion, "LogRetention", "default"); got["phase"] == "Applied" {
		t.Fatalf("dry-run reported live retention: %#v", got)
	}
}
