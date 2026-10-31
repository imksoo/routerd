// SPDX-License-Identifier: BSD-3-Clause

package chain

import "context"

// The kernel may delete a finite address immediately on DAD failure. Watching
// before adding it preserves that result when a later address dump is empty.
type ipv6DADWatcher interface {
	Wait(context.Context) (failed bool, err error)
	Close()
}
