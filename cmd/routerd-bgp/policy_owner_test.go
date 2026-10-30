// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	gobgpapi "github.com/osrg/gobgp/v4/api"
)

func TestDynamicPathReplayPreservesCompleteStaticAndDynamicPolicy(t *testing.T) {
	server := realDynamicPathServer(t)
	statePath, applied := dynamicJournal(t)
	intent := bgpdaemon.PolicyIntent{RouterName: "edge", Scopes: []bgpdaemon.PolicyScope{
		{Name: "routerd-edge-static", Neighbors: []string{"192.0.2.2/32"}, PreserveImportPrefixes: true, Spec: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.0.0.0/24"}}, ExportPolicyName: "routerd-edge-static-export", ExportPolicy: api.BGPExportPolicySpec{AllowedPrefixes: []string{"10.0.0.0/24"}}},
		{Name: "routerd-edge-dynamic", Neighbors: []string{"192.0.2.128/25"}, Spec: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.1.0.0/24"}, ForbiddenCommunities: []string{"64512:666"}}, ExportPolicyName: "routerd-edge-dynamic-export", ExportPolicy: api.BGPExportPolicySpec{AllowedPrefixes: []string{"10.1.0.0/24"}}},
	}}
	ctx := context.Background()
	if _, err := reconcilePolicyConfig(ctx, server, statePath, applied, intent); err != nil {
		t.Fatal(err)
	}
	path := bgpdaemon.AppliedPath{Source: "MobilityPool/replay/node/local", Prefix: "10.77.60.11/32"}
	if _, _, err := upsertDynamicPath(ctx, server, statePath, path); err != nil {
		t.Fatal(err)
	}
	stored, _, err := bgpdaemon.ReadApplied(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if matches, err := policyPlanMatches(ctx, server, bgpdaemon.BuildPolicyPlan(intent, stored.Paths)); err != nil || !matches {
		reportNativePolicyDifferences(t, server, bgpdaemon.BuildPolicyPlan(intent, stored.Paths))
		t.Fatalf("path replay replaced complete policy: matches=%t err=%v", matches, err)
	}
	if _, err := deleteDynamicPath(ctx, server, statePath, path); err != nil {
		t.Fatal(err)
	}
	if matches, err := policyPlanMatches(ctx, server, bgpdaemon.BuildPolicyPlan(intent, nil)); err != nil || !matches {
		reportNativePolicyDifferences(t, server, bgpdaemon.BuildPolicyPlan(intent, nil))
		t.Fatalf("path withdrawal replaced complete policy: matches=%t err=%v", matches, err)
	}
}

func TestPathRequestRejectsTrailingMutationBeforeNativeEffect(t *testing.T) {
	server := realDynamicPathServer(t)
	statePath, _ := dynamicJournal(t)
	body := `{"source":"MobilityPool/edge/node/a","prefix":"10.77.60.11/32"} {}`
	reply := httptest.NewRecorder()
	controlHandler(statePath, server, &sync.Mutex{}).ServeHTTP(reply, httptest.NewRequest(http.MethodPost, "/v1/paths", strings.NewReader(body)))
	if reply.Code != http.StatusBadRequest {
		t.Fatalf("unframed mutation status=%d body=%s", reply.Code, reply.Body.String())
	}
	stored, _, err := bgpdaemon.ReadApplied(statePath)
	if err != nil || len(stored.Paths) > 0 || len(stored.PendingPathAdds) > 0 || len(nativePathIdentities(t, server)) > 0 {
		t.Fatalf("unframed request changed native ownership: %+v, %v", stored, err)
	}
}

func TestPolicyRequestRejectsUnimplementedSelectorsBeforeNativeEffect(t *testing.T) {
	for _, selector := range []string{`"allowedPrefixLengthMin":32`, `"allowedPrefixLengthMax":32`, `"communities":{"accept":["64512:1"]}`} {
		t.Run(selector, func(t *testing.T) {
			server := realDynamicPathServer(t)
			statePath, _ := dynamicJournal(t)
			body := `{"routerName":"edge","scopes":[{"name":"peer","neighbors":["192.0.2.2/32"],"exportPolicyName":"peer-export","importPolicy":{` + selector + `}}]}`
			reply := httptest.NewRecorder()
			controlHandler(statePath, server, &sync.Mutex{}).ServeHTTP(reply, httptest.NewRequest(http.MethodPost, "/v1/policy", strings.NewReader(body)))
			if reply.Code != http.StatusBadRequest {
				t.Fatalf("unsupported selector status=%d body=%s", reply.Code, reply.Body.String())
			}
			stored, _, err := bgpdaemon.ReadApplied(statePath)
			if err != nil || stored.PendingPolicy != nil || stored.Policy.RouterName != "test" {
				t.Fatalf("rejected selector changed journal: %+v, %v", stored, err)
			}
			policies := 0
			if err := server.ListPolicy(context.Background(), &gobgpapi.ListPolicyRequest{}, func(*gobgpapi.Policy) { policies++ }); err != nil {
				t.Fatal(err)
			}
			if policies != 0 {
				t.Fatalf("rejected selector installed %d native policies", policies)
			}
		})
	}
}

func TestPolicyRequestNormalizesUsableFilterValuesBeforePublication(t *testing.T) {
	server := realDynamicPathServer(t)
	statePath, _ := dynamicJournal(t)
	body := `{"routerName":"edge","scopes":[{"name":"peer","neighbors":["192.0.2.2/32"],"exportPolicyName":"peer-export","importPolicy":{"allowedPrefixes":[" 10.77.60.0/24 "],"allowedPrefixLengthMin":32,"allowedPrefixLengthMax":32,"nextHopRewrite":" unchanged "}}]}`
	reply := httptest.NewRecorder()
	controlHandler(statePath, server, &sync.Mutex{}).ServeHTTP(reply, httptest.NewRequest(http.MethodPost, "/v1/policy", strings.NewReader(body)))
	if reply.Code != http.StatusOK {
		t.Fatalf("usable filter status=%d body=%s", reply.Code, reply.Body.String())
	}
	stored, _, err := bgpdaemon.ReadApplied(statePath)
	if err != nil || len(stored.Policy.Scopes) != 1 || stored.Policy.Scopes[0].Spec.NextHopRewrite != "unchanged" || len(stored.Policy.Scopes[0].Spec.AllowedPrefixes) != 1 || stored.Policy.Scopes[0].Spec.AllowedPrefixes[0] != "10.77.60.0/24" {
		t.Fatalf("usable filter was lost: %+v, %v", stored, err)
	}
}

func reportNativePolicyDifferences(t *testing.T, server policyOwnerServer, plan bgpdaemon.PolicyPlan) {
	t.Helper()
	for _, want := range plan.SetPolicies.GetDefinedSets() {
		server.ListDefinedSet(context.Background(), &gobgpapi.ListDefinedSetRequest{DefinedType: want.DefinedType, Name: want.Name}, func(got *gobgpapi.DefinedSet) {
			if !bgpdaemon.EqualDefinedSet(want, got) {
				t.Logf("set want=%s got=%s", want, got)
			}
		})
	}
	for _, want := range plan.SetPolicies.GetPolicies() {
		server.ListPolicy(context.Background(), &gobgpapi.ListPolicyRequest{Name: want.Name}, func(got *gobgpapi.Policy) {
			if !bgpdaemon.EqualPolicy(want, got) {
				t.Logf("policy want=%s got=%s", want, got)
			}
		})
	}
	server.ListPolicyAssignment(context.Background(), &gobgpapi.ListPolicyAssignmentRequest{Name: "global", Direction: gobgpapi.PolicyDirection_POLICY_DIRECTION_IMPORT}, func(got *gobgpapi.PolicyAssignment) {
		if !bgpdaemon.EqualPolicyAssignment(plan.GlobalImportAssignment, got) {
			t.Logf("assignment want=%s got=%s", plan.GlobalImportAssignment, got)
		}
	})
}

func TestPolicyOwnerRepairsReorderedNativeStatements(t *testing.T) {
	server := realDynamicPathServer(t)
	statePath, applied := dynamicJournal(t)
	intent := bgpdaemon.PolicyIntent{RouterName: "ordered", Scopes: []bgpdaemon.PolicyScope{{Name: "routerd-ordered-peer", Neighbors: []string{"192.0.2.2/32"}, Spec: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}, ForbiddenCommunities: []string{"64512:666"}}, ExportPolicyName: "routerd-ordered-export"}}}
	ctx := context.Background()
	applied, err := reconcilePolicyConfig(ctx, server, statePath, applied, intent)
	if err != nil {
		t.Fatal(err)
	}
	plan := bgpdaemon.BuildPolicyPlan(intent, nil)
	for _, policy := range plan.SetPolicies.Policies {
		if policy.Name == plan.GlobalImportAssignment.Policies[0].Name {
			if len(policy.Statements) < 3 {
				t.Fatalf("missing ordered import statements: %+v", policy)
			}
			policy.Statements[0], policy.Statements[1] = policy.Statements[1], policy.Statements[0]
		}
	}
	if err := server.SetPolicies(ctx, plan.SetPolicies); err != nil {
		t.Fatal(err)
	}
	if matches, err := policyPlanMatches(ctx, server, bgpdaemon.BuildPolicyPlan(intent, nil)); err != nil || matches {
		t.Fatalf("reordered native policy accepted: matches=%t err=%v", matches, err)
	}
	if _, err := reconcilePolicyConfig(ctx, server, statePath, applied, intent); err != nil {
		t.Fatal(err)
	}
	if matches, err := policyPlanMatches(ctx, server, bgpdaemon.BuildPolicyPlan(intent, nil)); err != nil || !matches {
		t.Fatalf("native order was not repaired: matches=%t err=%v", matches, err)
	}
}

func TestPendingPolicyFailureKeepsNativeRejectGuard(t *testing.T) {
	server := realDynamicPathServer(t)
	statePath, applied := dynamicJournal(t)
	fault := &interruptedDynamicPathServer{localPathServer: server, policyErr: context.DeadlineExceeded}
	intent := bgpdaemon.PolicyIntent{RouterName: "guarded"}
	if _, err := reconcilePolicyConfig(context.Background(), fault, statePath, applied, intent); err == nil {
		t.Fatal("native policy failure was hidden")
	}
	stored, _, err := bgpdaemon.ReadApplied(statePath)
	if err != nil || stored.PendingPolicy == nil {
		t.Fatalf("failed policy lost recovery: %+v, %v", stored, err)
	}
	var assignment *gobgpapi.PolicyAssignment
	if err := server.ListPolicyAssignment(context.Background(), &gobgpapi.ListPolicyAssignmentRequest{Name: "global", Direction: gobgpapi.PolicyDirection_POLICY_DIRECTION_IMPORT}, func(value *gobgpapi.PolicyAssignment) { assignment = value }); err != nil {
		t.Fatal(err)
	}
	if assignment.GetDefaultAction() != gobgpapi.RouteAction_ROUTE_ACTION_REJECT || len(assignment.GetPolicies()) != 1 || assignment.Policies[0].Name != bgpdaemon.PolicyGuardName {
		t.Fatalf("failed replacement left a permissive assignment: %+v", assignment)
	}
	fault.policyErr = nil
	if _, err := reconcilePolicyConfig(context.Background(), fault, statePath, stored, *stored.PendingPolicy); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyOwnerRepairsUnexpectedNativeNextHopAction(t *testing.T) {
	server := realDynamicPathServer(t)
	statePath, applied := dynamicJournal(t)
	intent := bgpdaemon.PolicyIntent{RouterName: "drift", Scopes: []bgpdaemon.PolicyScope{{
		Name: "routerd-drift-peer", Neighbors: []string{"192.0.2.2/32"},
		Spec: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}}, ExportPolicyName: "routerd-drift-export",
	}}}
	ctx := context.Background()
	applied, err := reconcilePolicyConfig(ctx, server, statePath, applied, intent)
	if err != nil {
		t.Fatal(err)
	}
	plan := bgpdaemon.BuildPolicyPlan(intent, nil)
	var changedPolicy, changedStatement string
	if matches, err := policyPlanMatches(ctx, server, plan); err != nil || !matches {
		reportNativePolicyDifferences(t, server, plan)
		t.Fatalf("initial native policy does not match: matches=%v err=%v", matches, err)
	}
	for _, policy := range plan.SetPolicies.Policies {
		if policy.Name != plan.GlobalImportAssignment.Policies[0].Name {
			continue
		}
		for _, statement := range policy.Statements {
			if statement.GetActions().GetRouteAction() == gobgpapi.RouteAction_ROUTE_ACTION_ACCEPT && statement.GetActions().GetNexthop() == nil {
				statement.Actions.Nexthop = &gobgpapi.NexthopAction{Address: "192.0.2.99"}
				changedPolicy, changedStatement = policy.Name, statement.Name
				break
			}
		}
	}
	if changedStatement == "" {
		t.Fatal("fixture has no native import allow action")
	}
	if err := server.SetPolicies(ctx, plan.SetPolicies); err != nil {
		t.Fatal(err)
	}
	for _, assignment := range []*gobgpapi.PolicyAssignment{plan.GlobalImportAssignment, plan.GlobalExportAssignment} {
		if err := server.SetPolicyAssignment(ctx, &gobgpapi.SetPolicyAssignmentRequest{Assignment: assignment}); err != nil {
			t.Fatal(err)
		}
	}
	assertAddress := func(want string) {
		t.Helper()
		found := false
		if err := server.ListPolicy(ctx, &gobgpapi.ListPolicyRequest{Name: changedPolicy}, func(policy *gobgpapi.Policy) {
			for _, statement := range policy.Statements {
				if statement.Name == changedStatement {
					found = true
					if got := statement.GetActions().GetNexthop().GetAddress(); got != want {
						t.Errorf("actual native next hop=%q want=%q", got, want)
					}
				}
			}
		}); err != nil {
			t.Fatal(err)
		}
		if !found {
			t.Fatal("actual native changed statement is missing")
		}
	}
	assertAddress("192.0.2.99")
	if matches, err := policyPlanMatches(ctx, server, bgpdaemon.BuildPolicyPlan(intent, nil)); err != nil || matches {
		t.Fatalf("unexpected native action accepted as applied: matches=%v err=%v", matches, err)
	}
	if _, err := reconcilePolicyConfig(ctx, server, statePath, applied, intent); err != nil {
		t.Fatal(err)
	}
	assertAddress("")
	if matches, err := policyPlanMatches(ctx, server, bgpdaemon.BuildPolicyPlan(intent, nil)); err != nil || !matches {
		t.Fatalf("native action drift was not repaired: matches=%v err=%v", matches, err)
	}
}
