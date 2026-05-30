// SPDX-License-Identifier: BSD-3-Clause

package plugin

import (
	"strings"
	"testing"
)

func validAssignSecondaryIP() ActionPlan {
	return ActionPlan{
		Name:     "claim-secondary",
		Provider: "oci",
		Action:   ActionAssignSecondaryIP,
		Target:   map[string]string{"address": "10.0.0.5", "nicRef": "vnic-abc"},
	}
}

func TestValidateActionPlanAccepts(t *testing.T) {
	tests := []struct {
		name string
		plan ActionPlan
	}{
		{
			name: "assign-secondary-ip",
			plan: validAssignSecondaryIP(),
		},
		{
			name: "ensure-forwarding-enabled no target required",
			plan: ActionPlan{
				Name:     "enable-fwd",
				Provider: "aws",
				Action:   ActionEnsureForwardingEnabled,
			},
		},
		{
			name: "dry-run mode and low risk and undo",
			plan: ActionPlan{
				Name:      "claim",
				Provider:  "azure",
				Action:    ActionAssignSecondaryIP,
				Mode:      ActionModeDryRun,
				RiskLevel: "low",
				Target:    map[string]string{"address": "10.0.0.6", "nicRef": "nic-1"},
				Undo:      &ActionUndo{Action: ActionUnassignSecondaryIP},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateActionPlan(tt.plan); err != nil {
				t.Fatalf("ValidateActionPlan: unexpected error: %v", err)
			}
		})
	}
}

func TestValidateActionPlanRejectsExecuteExactly(t *testing.T) {
	plan := validAssignSecondaryIP()
	plan.Mode = "execute"
	err := ValidateActionPlan(plan)
	if err == nil {
		t.Fatalf("expected error for mode=execute")
	}
	const want = "actionPlan mode=execute is not supported; provider actions are dry-run/display-only in Phase 4.1"
	if err.Error() != want {
		t.Fatalf("error = %q, want exactly %q", err.Error(), want)
	}
}

func TestValidateActionPlanRejects(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ActionPlan)
		want   string
	}{
		{
			name:   "missing name",
			mutate: func(p *ActionPlan) { p.Name = "" },
			want:   "name is required",
		},
		{
			name:   "missing provider",
			mutate: func(p *ActionPlan) { p.Provider = "" },
			want:   "provider is required",
		},
		{
			name:   "bad provider",
			mutate: func(p *ActionPlan) { p.Provider = "digitalocean" },
			want:   "provider \"digitalocean\" must be one of",
		},
		{
			name:   "bad action",
			mutate: func(p *ActionPlan) { p.Action = "delete-everything" },
			want:   "action \"delete-everything\" must be one of",
		},
		{
			name:   "missing target.address",
			mutate: func(p *ActionPlan) { delete(p.Target, "address") },
			want:   "requires target.address",
		},
		{
			name:   "missing target.nicRef",
			mutate: func(p *ActionPlan) { delete(p.Target, "nicRef") },
			want:   "requires target.nicRef",
		},
		{
			name:   "bad risk level",
			mutate: func(p *ActionPlan) { p.RiskLevel = "extreme" },
			want:   "riskLevel \"extreme\" must be one of",
		},
		{
			name:   "bad mode",
			mutate: func(p *ActionPlan) { p.Mode = "apply" },
			want:   "mode \"apply\" must be empty or dry-run",
		},
		{
			name:   "bad undo action",
			mutate: func(p *ActionPlan) { p.Undo = &ActionUndo{Action: "nope"} },
			want:   "undo.action \"nope\" must be one of",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := validAssignSecondaryIP()
			tt.mutate(&plan)
			err := ValidateActionPlan(plan)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}
