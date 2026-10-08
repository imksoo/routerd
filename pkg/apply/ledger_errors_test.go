// SPDX-License-Identifier: BSD-3-Clause

package apply

import (
	"errors"
	"testing"

	"github.com/imksoo/routerd/pkg/resource"
)

type failingOwnershipLedger struct {
	resource.Ledger
	err error
}

func (l failingOwnershipLedger) Owns(resource.Artifact) (bool, error) { return false, l.err }
func (l failingOwnershipLedger) All() ([]resource.Artifact, error)    { return nil, l.err }

func TestLedgerReadErrorsBlockOrphanAndAdoptionPlans(t *testing.T) {
	failure := errors.New("injected ownership read failure")
	ledger := failingOwnershipLedger{Ledger: resource.NewLedger(), err: failure}
	engine := &Engine{Command: fakeCommand(map[string]string{}), OSNetworking: &osNetworking{}}
	router := overlapRouter(true)
	if plan, err := engine.LedgerOwnedOrphanPlan(router, ledger); !errors.Is(err, failure) || len(plan.Actions) != 0 {
		t.Fatalf("orphan plan hid ownership failure: %+v, %v", plan, err)
	}
	if candidates, artifacts, err := engine.AdoptionCandidateArtifacts(router, ledger); !errors.Is(err, failure) || candidates != nil || artifacts != nil {
		t.Fatalf("adoption plan hid ownership failure: %+v, %+v, %v", candidates, artifacts, err)
	}
}
