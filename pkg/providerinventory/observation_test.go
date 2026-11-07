// SPDX-License-Identifier: BSD-3-Clause

package providerinventory

import (
	"context"
	"testing"
)

func TestInventoryDistinguishesExplicitEmptyFromMissingObservations(t *testing.T) {
	for _, sample := range []struct {
		name, status string
		accepted     bool
	}{
		{"empty", `{"status":"succeeded","localIPs":[],"observedCandidates":[]}`, true},
		{"missing", `{"status":"succeeded","localIPs":[]}`, false},
		{"unknown", `{"status":"succeeded","localIPs":null,"observedCandidates":[]}`, false},
	} {
		t.Run(sample.name, func(t *testing.T) {
			body := `{"apiVersion":"providerinventory.routerd.net/v1alpha1","kind":"ObservePrivateIPsResult","status":` + sample.status + `}`
			binary := writeInventoryPlugin(t, body)
			result, _, err := RunInventory(context.Background(), inventorySpec(binary), NewObservePrivateIPsRequest(ObservePrivateIPsRequestSpec{Provider: "aws"}))
			if sample.accepted {
				if err != nil || result.Status.LocalIPs == nil || result.Status.ObservedCandidates == nil || len(result.Status.LocalIPs) != 0 || len(result.Status.ObservedCandidates) != 0 {
					t.Fatalf("explicit empty observation = %+v %v", result, err)
				}
			} else if err == nil {
				t.Fatalf("unknown observation accepted as absence: %+v", result)
			}
		})
	}
}
