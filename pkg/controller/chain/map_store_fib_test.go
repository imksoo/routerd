// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"

	"github.com/imksoo/routerd/pkg/bgp"
)

// Unrelated controller fixtures have no BGP receipts. Real FIB effects and
// persistence failures are exercised with SQLite, never this display map.
func (mapStore) ReadBGPFIB(context.Context) (bgp.FIBJournal, bool, error) {
	return bgp.FIBJournal{}, false, nil
}

func (mapStore) SaveBGPFIB(context.Context, bgp.FIBJournal) error {
	return errors.New("BGP FIB effects require a SQLite fixture")
}
