// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"gopkg.in/yaml.v3"
)

func TestFaultDryRunEventRuleHistoryUsesIsolatedJournal(t *testing.T) {
	router, opts := faultApplyFixture(t)
	opts.DryRun = true
	opts.attemptHooks.configureControllers = func(o *controllerchain.Options) { o.EnabledControllers = []string{"link"} }
	router.Spec.Resources = []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "loopback"}, Spec: api.InterfaceSpec{IfName: "lo", Managed: false, Owner: "external"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EventRule"}, Metadata: api.ObjectMeta{Name: "status-history"}, Spec: api.EventRuleSpec{Pattern: api.EventRulePatternSpec{Operator: "count", Topic: "routerd.resource.status.changed", Threshold: 1}, Emit: api.EventRuleEmitSpec{Topic: "routerd.test.status"}}},
	}
	data, err := yaml.Marshal(router)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Validate(router); err != nil {
		t.Fatal(err)
	}
	opts.ConfigYAMLOverride = string(data)
	store := faultGenerationStore(t, opts.StatePath, false)
	if _, err := runApplyChainOnce(context.Background(), router, opts, io.Discard, nil); err != nil {
		t.Fatalf("valid dry-run rejected history capability: %v", err)
	}
	m := serveConfigMutator{configPath: opts.ConfigPath, statePath: opts.StatePath, baseOpts: opts, cache: &resultCache{}, getRouter: func() *api.Router { return router }}
	if _, err := m.plan(nil, controlapi.PlanRequest{CandidateYAML: string(data), Replace: true}); err != nil {
		t.Fatalf("valid plan rejected history capability: %v", err)
	}
	if _, err := m.apply(nil, controlapi.ApplyRequest{CandidateYAML: string(data), Replace: true, DryRun: true}); err != nil {
		t.Fatalf("HTTP dry-run rejected history capability: %v", err)
	}
	events, err := store.ListEvents(routerstate.EventQuery{})
	if err != nil || len(events) != 0 {
		t.Errorf("dry-run changed production journal=%v,%v", events, err)
	}
	statuses, err := store.ListObjectStatuses()
	if err != nil || len(statuses) != 0 {
		t.Errorf("dry-run changed production statuses=%v,%v", statuses, err)
	}
	if store.LatestGeneration() != 0 {
		t.Error("dry-run created production generation")
	}
	canonical, err := os.ReadFile(opts.ConfigPath)
	if err != nil || string(canonical) != testRouterYAML("old-router") {
		t.Errorf("dry-run changed canonical=%q,%v", canonical, err)
	}
}
