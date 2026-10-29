// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"strings"
	"testing"
)

func TestActionPlanCorruptDatabaseValuesFailAtReadBoundary(t *testing.T) {
	for _, tc := range []struct{ column, value string }{
		{"target_json", `{"address":17}`},
		{"parameters_json", `{"region":false}`},
		{"undo_json", `{"action":"unassign-secondary-ip","unknown":true}`},
		{"undo_json", `{"action":"unassign-secondary-ip"} {}`},
		{"observed_json", `{"priorSourceDestCheck":true}`},
		{"updated_at", "not-a-time"},
	} {
		t.Run(tc.column+tc.value, func(t *testing.T) {
			store := mustOpenStore(t)
			defer store.Close()
			if _, err := store.ImportAction(sampleActionRecord("corrupt")); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec("UPDATE action_executions SET "+tc.column+" = ?", tc.value); err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.GetActionByIdempotencyKey("corrupt"); err == nil || !strings.Contains(err.Error(), tc.column) {
				t.Fatalf("corrupt %s was accepted: %v", tc.column, err)
			}
			if _, err := store.ListActions(ActionExecutionFilter{}); err == nil {
				t.Fatal("corrupt plan accepted by journal listing")
			}
		})
	}
}
