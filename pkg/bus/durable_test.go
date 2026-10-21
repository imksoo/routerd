// SPDX-License-Identifier: BSD-3-Clause
package bus

import (
	"context"
	"errors"
	"testing"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestDurablePublishRequiresJournalSuccessEvenForNonfatalError(t *testing.T) {
	for _, store := range []EventStore{nil, failingEventStore{err: nonFatalStoreError{errors.New("disk full")}}, failingEventStore{err: nil}} {
		b := NewWithStore(store)
		ch, cancel := b.Subscribe(context.Background(), Subscription{}, 1)
		if err := b.PublishDurable(context.Background(), daemonapi.DaemonEvent{Type: "routerd.test.event"}); err == nil {
			t.Error("unconfirmed write succeeded")
		}
		select {
		case <-ch:
			t.Error("unconfirmed event delivered live")
		default:
		}
		cancel()
	}
}
