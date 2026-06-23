// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"encoding/json"
	"strings"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

func latestAssignedAddresses(actions []routerstate.ActionExecutionRecord) map[string]bool {
	type actionKey struct {
		providerRef string
		address     string
	}
	latest := map[actionKey]routerstate.ActionExecutionRecord{}
	for _, a := range actions {
		if a.Action != "assign-secondary-ip" && a.Action != "unassign-secondary-ip" {
			continue
		}
		addr := actionTargetAddress(a.TargetJSON)
		if addr == "" {
			continue
		}
		key := actionKey{providerRef: a.ProviderRef, address: addr}
		if prev, ok := latest[key]; !ok || a.ID > prev.ID {
			latest[key] = a
		}
	}
	out := map[string]bool{}
	for _, a := range latest {
		if a.Action != "assign-secondary-ip" || a.Status != routerstate.ActionSucceeded {
			continue
		}
		if addr := actionTargetAddress(a.TargetJSON); addr != "" {
			out[strings.TrimSuffix(strings.TrimSpace(addr), "/32")] = true
		}
	}
	return out
}

func actionTargetAddress(targetJSON string) string {
	if targetJSON == "" {
		return ""
	}
	var target map[string]string
	if err := json.Unmarshal([]byte(targetJSON), &target); err != nil {
		return ""
	}
	return strings.TrimSpace(target["address"])
}
