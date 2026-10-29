// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"reflect"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/pdclient"
)

func TestPDLiveProjectsValidOlderPrefixWithoutNewTransaction(t *testing.T) {
	epoch := time.Date(2026, 10, 29, 12, 0, 0, 500000000, time.UTC)
	deadline := epoch.Add(100*time.Second + 750*time.Millisecond)
	lease := PDLease{CurrentPrefix: "2001:db8:2000::/60", LastPrefix: "2001:db8:2000::/60", LastReplyAt: epoch, SnapshotUpdatedAt: epoch, LeaseUpdatedAt: epoch, LastObservedAt: epoch, PreferredSeconds: 4, ValidSeconds: 5, Phase: daemonapi.ResourcePhaseBound, PreviousPrefixes: []PDPreviousPrefix{{Prefix: "2001:db8:1000::/60", ExpiresAt: deadline}}}
	before := copyPDLease(lease)
	live := lease.Live(epoch.Add(6 * time.Second))
	if live.CurrentPrefix != "2001:db8:1000::/60" || live.PreferredSeconds != 0 || live.ValidSeconds != 100 || live.Phase != daemonapi.ResourcePhaseBound || !live.HasFreshTransactionEvidence(epoch.Add(6*time.Second)) {
		t.Fatalf("valid association lost: %+v", live)
	}
	if !live.LastReplyAt.Equal(epoch) || !live.SnapshotUpdatedAt.Equal(epoch) || !live.LeaseUpdatedAt.Equal(epoch) || !live.LastObservedAt.Equal(epoch) {
		t.Fatalf("projection fabricated a new observation: %+v", live)
	}
	if live.LastReplyAt.Add(time.Duration(live.ValidSeconds) * time.Second).After(deadline) {
		t.Fatal("promotion extended prefix deadline")
	}
	if !reflect.DeepEqual(lease, before) {
		t.Fatal("projection mutated persisted association")
	}
	if expired := lease.Live(deadline.Add(time.Second)); expired.CurrentPrefix != "" || expired.Phase != daemonapi.ResourcePhaseExpired {
		t.Fatalf("expired previous prefix revived: %+v", expired)
	}
	lease.Phase = daemonapi.ResourcePhaseReleased
	if released := lease.Live(epoch.Add(6 * time.Second)); released.CurrentPrefix != "" || released.Phase != daemonapi.ResourcePhaseReleased {
		t.Fatalf("explicit release overridden: %+v", released)
	}
}

func TestPDObservationKeepsGenuineEpochForColdSnapshotCurrentExpiry(t *testing.T) {
	epoch := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	snapshot := pdclient.Snapshot{State: pdclient.StateBound, CurrentPrefix: "2001:db8:2000::/60", UpdatedAt: epoch.Add(time.Second), LeaseUpdatedAt: epoch, Prefixes: []pdclient.PrefixSnapshot{
		{Prefix: "2001:db8:2000::/60", AcquiredAt: epoch, Preferred: 4, Valid: 5},
		{Prefix: "2001:db8:1000::/60", AcquiredAt: epoch.Add(-10 * time.Second), Preferred: 0, Valid: 100},
	}}
	live := ObservePDLease(PDLease{}, snapshot, epoch.Add(6*time.Second))
	if live.Phase != daemonapi.ResourcePhaseBound || live.CurrentPrefix != "2001:db8:1000::/60" || live.PreferredSeconds != 0 || live.ValidSeconds != 90 || !live.LastReplyAt.Equal(epoch) || !live.SnapshotUpdatedAt.Equal(snapshot.UpdatedAt) || !live.LeaseUpdatedAt.Equal(epoch) {
		t.Fatalf("cold recovery lost real remaining association: %+v", live)
	}
	snapshot.State = pdclient.StateReleased
	released := ObservePDLease(PDLease{}, snapshot, epoch.Add(6*time.Second))
	if released.CurrentPrefix != "" || released.Phase == daemonapi.ResourcePhaseBound {
		t.Fatalf("explicit withdrawal revived old prefix: %+v", released)
	}
}
