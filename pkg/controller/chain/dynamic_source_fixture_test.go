// SPDX-License-Identifier: BSD-3-Clause

package chain

import routerstate "github.com/imksoo/routerd/pkg/state"

// This ordinary fixture owns no dynamic producer records.
func (s mapStore) ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error) {
	return nil, nil
}
