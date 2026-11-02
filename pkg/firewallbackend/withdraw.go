// SPDX-License-Identifier: BSD-3-Clause

package firewallbackend

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/nftstate"
	"github.com/imksoo/routerd/pkg/render"
)

// withdraw replaces a removed policy with a router-owned fail-closed table.
// A router can retain forwarding while a Sysctl resource is withdrawn, so
// deleting the last filter table would make an explicit configuration removal
// silently permissive. The rules file is retained until the native effect is
// acknowledged; a failed observation or replacement remains retryable.
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
	fallback := Ruleset{
		Backend: ruleset.Backend,
		Path:    ruleset.Path,
		Data:    failClosedFirewallRuleset(),
	}
	changed, err := b.Diff(fallback)
	if err != nil {
		return false, err
	}
	if !changed && nftstate.RecentlyVerified(ruleset.Path, time.Now().UTC()) {
		return false, nil
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
	}
	if err := writeRuleset(fallback.Path, fallback.Data); err != nil {
		return false, err
	}
	if out, err := b.run(ctx, nft, "-c", "-f", fallback.Path); err != nil {
		return false, fmt.Errorf("%s -c -f %s: %w: %s", nft, fallback.Path, err, strings.TrimSpace(string(out)))
	}
	if out, err := b.run(ctx, nft, "-f", fallback.Path); err != nil {
		return false, fmt.Errorf("%s -f %s: %w: %s", nft, fallback.Path, err, strings.TrimSpace(string(out)))
	}
	return changed, nftstate.MarkVerified(fallback.Path, time.Now().UTC())
}

func failClosedFirewallRuleset() []byte {
	marker := strconv.Quote(render.NftablesRouterdOwnerMarker)
	return []byte("destroy table inet routerd_filter\n" +
		"add table inet routerd_filter { comment " + marker + "; }\n" +
		"table inet routerd_filter {\n" +
		"  comment " + marker + "\n" +
		"  chain input { type filter hook input priority filter; policy drop; }\n" +
		"  chain forward { type filter hook forward priority filter; policy drop; }\n" +
		"  chain output { type filter hook output priority filter; policy accept; }\n" +
		"}\n")
}
