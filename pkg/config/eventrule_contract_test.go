// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestEventRuleAdmissionRetainsOperatorsAndRejectsUnsupportedIntent(t *testing.T) {
	validate := func(pattern api.EventRulePatternSpec) error {
		router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "rules"}, Spec: api.RouterSpec{Resources: []api.Resource{testResource(api.NetAPIVersion, "EventRule", "test", api.EventRuleSpec{Pattern: pattern, Emit: api.EventRuleEmitSpec{Topic: "routerd.test.output"}})}}}
		return ValidateForOS(router, platform.OSLinux)
	}
	for _, operator := range []string{"all_of", "any_of", "sequence", "window", "absence", "throttle", "debounce", "count"} {
		pattern := api.EventRulePatternSpec{Operator: operator, Topic: "routerd.input", Interval: "0s"}
		if err := validate(pattern); err != nil {
			t.Fatalf("supported %s rejected: %v", operator, err)
		}
		pattern.Rate = 2
		if err := validate(pattern); err == nil || !strings.Contains(err.Error(), "rate is not implemented") {
			t.Fatalf("unsupported rate accepted for %s: %v", operator, err)
		}
		pattern.Rate = 0
		pattern.Quiet = "-1s"
		if err := validate(pattern); err == nil || !strings.Contains(err.Error(), "must be non-negative") {
			t.Fatalf("negative quiet admitted for %s: %v", operator, err)
		}
	}
	if err := validate(api.EventRulePatternSpec{Operator: "absence", Expected: "routerd.expected"}); err == nil || !strings.Contains(err.Error(), "requires") {
		t.Fatalf("untriggerable absence accepted: %v", err)
	}
}
