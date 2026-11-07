// SPDX-License-Identifier: BSD-3-Clause

package firewallbackend

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/nftstate"
	"github.com/imksoo/routerd/pkg/render"
)

// withdraw runs when the last firewall resource disappears, including after a
// controller restart. The rules file is retained until the native effect is
// acknowledged; a failed read or delete must remain retryable.
func (b Nftables) withdraw(ctx context.Context, ruleset Ruleset, dryRun bool) (bool, error) {
	if _, err := os.Stat(ruleset.Path); os.IsNotExist(err) {
		// An unused firewall must not require nft on a router with no firewall.
		return false, nil
	} else if err != nil {
		return false, err
	}
	if dryRun {
		return true, nil
	}
	nft := stringutil.FirstNonEmpty(b.Command, "nft")
	out, err := b.run(ctx, nft, "-j", "list", "tables")
	if err != nil {
		return false, fmt.Errorf("observe firewall withdrawal: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var inventory struct {
		Tables []struct {
			Table *struct {
				Family  string `json:"family"`
				Name    string `json:"name"`
				Handle  uint64 `json:"handle"`
				Comment string `json:"comment"`
			} `json:"table"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal(out, &inventory); err != nil {
		return false, fmt.Errorf("decode firewall withdrawal inventory: %w", err)
	}
	if inventory.Tables == nil {
		return false, fmt.Errorf("firewall withdrawal inventory lacks nftables array")
	}
	if err := nftstate.Invalidate(ruleset.Path); err != nil {
		return false, err
	}
	for _, entry := range inventory.Tables {
		table := entry.Table
		if table == nil || table.Family != "inet" || table.Name != "routerd_filter" {
			continue
		}
		if table.Comment != render.NftablesRouterdOwnerMarker || table.Handle == 0 {
			return false, fmt.Errorf("refusing withdrawal of inet routerd_filter without native routerd ownership and handle")
		}
		// A same-name replacement has a different handle. Never delete by name
		// after observing the owner of a particular native table.
		if out, err := b.run(ctx, nft, "delete", "table", "inet", "handle", strconv.FormatUint(table.Handle, 10)); err != nil {
			return false, fmt.Errorf("withdraw inet routerd_filter handle %d: %w: %s", table.Handle, err, strings.TrimSpace(string(out)))
		}
	}
	if err := os.Remove(ruleset.Path); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, nil
}
