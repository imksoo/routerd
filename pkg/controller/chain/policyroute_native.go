// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

// The policy controller owns individual native effects, never an entire table
// or all rules at a priority. Plain routes/rules have no private kernel cookie:
// presence after an interrupted write is uncertainty, not an adoption receipt.
type policyRouteEffect struct {
	Family    int    `json:"family"`
	Source    string `json:"source"`
	InputIf   string `json:"inputIf"`
	Kind      string `json:"kind"`
	Table     int    `json:"table"`
	Priority  int    `json:"priority"`
	Mark      int    `json:"mark"`
	Metric    int    `json:"metric"`
	Interface string `json:"interface"`
	Gateway   string `json:"gateway"`
	IfIndex   int    `json:"ifIndex"`
}

type policyRouteReceipt struct {
	Effect  policyRouteEffect `json:"effect"`
	Scope   string            `json:"scope"`
	Pending string            `json:"pending"`
}

type policyRouteJournal struct{ Receipts []policyRouteReceipt }

func policyRouteJournalName(family int) string { return fmt.Sprintf("ipv%d-policy-native", family) }

func validPolicyRouteEffect(e policyRouteEffect) bool {
	if e.Family != 4 && e.Family != 6 || e.Table < 1 || uint64(e.Table) > 1<<32-1 {
		return false
	}
	switch e.Kind {
	case "rule":
		if e.Priority < 1 || e.Priority > 2147483647 || e.Metric != 0 || e.Interface != "" || e.Gateway != "" || e.IfIndex != 0 {
			return false
		}
		if e.Family == 4 {
			return e.Mark > 0 && uint64(e.Mark) <= 1<<32-1 && e.Source == "" && e.InputIf == ""
		}
		source, err := netip.ParseAddr(e.Source)
		return e.Mark == 0 && (e.InputIf == "lo" && e.Source == "" || e.InputIf == "" && err == nil && source.Is6())
	case "route":
		if e.Priority != 0 || e.Mark != 0 || e.Metric < 0 || e.Interface == "" || e.IfIndex < 1 || e.InputIf != "" {
			return false
		}
		if e.Gateway != "" {
			ip, err := netip.ParseAddr(e.Gateway)
			if err != nil || ip.Is4() != (e.Family == 4) {
				return false
			}
		}
		if e.Family == 4 {
			return e.Source == ""
		}
		source, err := netip.ParseAddr(e.Source)
		return err == nil && source.Is6() && e.Gateway != ""
	}
	return false
}

// Decode the existing status boundary explicitly. The journal has one typed
// in-memory representation; JSON encoding is owned by the SQLite status store.
func (c IPv4PolicyRouteController) readPolicyRouteJournal(family int) (policyRouteJournal, error) {
	status, err := c.Store.ReadObjectStatus(api.RouterAPIVersion, "Router", policyRouteJournalName(family))
	if err != nil {
		return policyRouteJournal{}, err
	}
	journal := policyRouteJournal{Receipts: []policyRouteReceipt{}}
	if len(status) == 0 {
		return journal, nil
	}
	version, ok := statusInt(status["version"])
	if !ok || version != 2 {
		return journal, errors.New("unsupported native policy ownership journal")
	}
	switch rows := status["receipts"].(type) {
	case []policyRouteReceipt:
		journal.Receipts = append(journal.Receipts, rows...)
	case []any:
		for _, row := range rows {
			r, ok := row.(map[string]any)
			if !ok {
				return journal, errors.New("invalid native policy receipt")
			}
			effect, ok := r["effect"].(map[string]any)
			if !ok {
				return journal, errors.New("invalid native policy effect")
			}
			var e policyRouteEffect
			for key, target := range map[string]*int{"family": &e.Family, "table": &e.Table, "priority": &e.Priority, "mark": &e.Mark, "metric": &e.Metric, "ifIndex": &e.IfIndex} {
				n, ok := statusInt(effect[key])
				if number, numeric := effect[key].(float64); numeric && float64(n) != number {
					ok = false
				}
				if !ok {
					return journal, fmt.Errorf("invalid native policy %s", key)
				}
				*target = n
			}
			for key, target := range map[string]*string{"source": &e.Source, "inputIf": &e.InputIf, "kind": &e.Kind, "interface": &e.Interface, "gateway": &e.Gateway} {
				s, ok := effect[key].(string)
				if !ok {
					return journal, fmt.Errorf("invalid native policy %s", key)
				}
				*target = s
			}
			scope, ok := r["scope"].(string)
			if !ok {
				return journal, errors.New("invalid native policy kernel scope")
			}
			pending, ok := r["pending"].(string)
			if !ok {
				return journal, errors.New("invalid native policy pending operation")
			}
			journal.Receipts = append(journal.Receipts, policyRouteReceipt{Effect: e, Scope: scope, Pending: pending})
		}
	default:
		return journal, errors.New("native policy receipts are not an array")
	}
	seen := map[policyRouteReceipt]bool{}
	for _, r := range journal.Receipts {
		key := r
		key.Pending = ""
		if r.Scope == "" || !validPolicyRouteEffect(r.Effect) || r.Effect.Family != family || r.Pending != "" && r.Pending != "add" && r.Pending != "delete" || seen[key] {
			return journal, errors.New("invalid native policy ownership journal")
		}
		seen[key] = true
	}
	return journal, nil
}

func (c IPv4PolicyRouteController) savePolicyRouteJournal(family int, j policyRouteJournal) error {
	return c.Store.SaveObjectStatus(api.RouterAPIVersion, "Router", policyRouteJournalName(family), map[string]any{"version": 2, "receipts": append([]policyRouteReceipt{}, j.Receipts...)})
}

func (c IPv4PolicyRouteController) policyRouteScope(ctx context.Context) (string, error) {
	if c.NativeScope != nil {
		return c.NativeScope(ctx)
	}
	return platform.KernelNetworkScope(ctx)
}

func (c IPv4PolicyRouteController) policyRouteIndex(ctx context.Context, ifname string) (int, bool, error) {
	out, err := c.commandOutput(ctx, "ip", "-o", "link", "show", "dev", ifname)
	if err != nil {
		text := strings.ToLower(string(out))
		if strings.Contains(text, "does not exist") || strings.Contains(text, "cannot find device") {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("observe policy interface %s: %w: %s", ifname, err, strings.TrimSpace(string(out)))
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return 0, false, errors.New("policy interface observation has no native identity")
	}
	index, err := strconv.Atoi(strings.TrimSuffix(fields[0], ":"))
	if err != nil || index < 1 {
		return 0, false, errors.New("policy interface observation has no index")
	}
	name := strings.SplitN(strings.TrimSuffix(fields[1], ":"), "@", 2)[0]
	if name != ifname {
		return 0, false, errors.New("policy interface observation identifies another device")
	}
	return index, true, nil
}

func (c IPv4PolicyRouteController) verifyPolicyRouteEffect(ctx context.Context, r policyRouteReceipt) error {
	scope, err := c.policyRouteScope(ctx)
	if err != nil {
		return err
	}
	if scope == "" || scope != r.Scope {
		return errors.New("native policy kernel scope changed")
	}
	if r.Effect.Kind == "route" {
		index, present, err := c.policyRouteIndex(ctx, r.Effect.Interface)
		if err != nil {
			return err
		}
		if !present || index != r.Effect.IfIndex {
			return errors.New("native policy native interface identity changed")
		}
	}
	return nil
}

func policyRouteEffectLess(a, b policyRouteEffect) bool {
	// Routes acquire before rules; withdrawal uses the reverse order.
	if a.Kind != b.Kind {
		return a.Kind == "route"
	}
	return fmt.Sprint(a) < fmt.Sprint(b)
}

func samePolicyRouteEffects(a, b []policyRouteEffect) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (c IPv4PolicyRouteController) policyRoutePremise(ctx context.Context, family int, scope string, desired []policyRouteEffect) error {
	current, err := c.policyRouteScope(ctx)
	if err != nil {
		return err
	}
	if current == "" || current != scope {
		return errors.New("native policy kernel scope changed")
	}
	var fresh []policyRouteEffect
	if family == 4 {
		fresh, err = c.desiredPolicyIPv4Effects(ctx)
	} else {
		fresh, err = c.desiredPolicyIPv6Effects(ctx)
	}
	if err != nil {
		return err
	}
	if !samePolicyRouteEffects(fresh, desired) {
		return errors.New("native policy intent or source changed while persistence waited")
	}
	return nil
}

func (c IPv4PolicyRouteController) reconcilePolicyRouteEffects(ctx context.Context, family int, desired []policyRouteEffect) error {
	if c.DryRun {
		return nil
	}
	journal, err := c.readPolicyRouteJournal(family)
	if err != nil {
		return err
	}
	if len(desired) == 0 && len(journal.Receipts) == 0 {
		return nil
	}
	scope, err := c.policyRouteScope(ctx)
	if err != nil {
		return err
	}
	if scope == "" {
		return errors.New("native policy kernel scope is unavailable")
	}
	wanted := map[policyRouteEffect]bool{}
	for _, e := range desired {
		wanted[e] = true
	}
	// Pending presence cannot prove whether this process, a previous process, or
	// an unrelated writer created it. Only exact absence releases that fence.
	for i := 0; i < len(journal.Receipts); {
		r := journal.Receipts[i]
		if r.Scope != scope || r.Pending == "" {
			i++
			continue
		}
		matching, _, err := c.observePolicyRouteEffect(ctx, r.Effect)
		if err != nil {
			return err
		}
		if matching {
			return errors.New("pending native policy effect is present without an acknowledged owner; inspect and remove it before retrying")
		}
		journal.Receipts = append(journal.Receipts[:i], journal.Receipts[i+1:]...)
		if err := c.savePolicyRouteJournal(family, journal); err != nil {
			return err
		}
	}
	changingTables := map[int]bool{}
	for _, receipt := range journal.Receipts {
		if receipt.Scope == scope && receipt.Effect.Kind == "route" && !wanted[receipt.Effect] {
			changingTables[receipt.Effect.Table] = true
		}
	}
	// Check every new tuple before changing any forwarding effect. A foreign
	// selector must not gain our route merely because routes acquire first.
	for _, e := range desired {
		matching, collision, err := c.observePolicyRouteEffect(ctx, e)
		if err != nil {
			return err
		}
		if e.Kind == "route" && !matching {
			changingTables[e.Table] = true
		}
		if !collision {
			continue
		}
		owned := false
		for _, r := range journal.Receipts {
			if r.Scope != scope || r.Pending != "" || r.Effect.Kind != e.Kind {
				continue
			}
			sameSlot := r.Effect == e || e.Kind == "route" && r.Effect.Table == e.Table && r.Effect.Metric == e.Metric
			if !sameSlot {
				continue
			}
			matching, _, err := c.observePolicyRouteEffect(ctx, r.Effect)
			if err != nil {
				return err
			}
			if matching {
				if err := c.verifyPolicyRouteEffect(ctx, r); err != nil {
					return err
				}
				owned = true
				break
			}
		}
		if !owned {
			return errors.New("foreign native policy rule/route occupies the requested native tuple")
		}
	}
	// Stop selectors before removing their routes. Never flush a shared table.
	for _, kind := range []string{"rule", "route"} {
		for i := 0; i < len(journal.Receipts); {
			r := journal.Receipts[i]
			keep := wanted[r.Effect] && !(r.Effect.Kind == "rule" && changingTables[r.Effect.Table])
			if r.Scope != scope || r.Effect.Kind != kind || keep {
				i++
				continue
			}
			matching, _, err := c.observePolicyRouteEffect(ctx, r.Effect)
			if err != nil {
				return err
			}
			if matching {
				if err := c.verifyPolicyRouteEffect(ctx, r); err != nil {
					return err
				}
				journal.Receipts[i].Pending = "delete"
				if err := c.savePolicyRouteJournal(family, journal); err != nil {
					return err
				}
				if err := c.policyRoutePremise(ctx, family, scope, desired); err != nil {
					journal.Receipts[i].Pending = ""
					return errors.Join(err, c.savePolicyRouteJournal(family, journal))
				}
				if err := c.verifyPolicyRouteEffect(ctx, r); err != nil {
					return err
				}
				matching, _, err = c.observePolicyRouteEffect(ctx, r.Effect)
				if err != nil {
					return err
				}
				if matching {
					if err := c.mutatePolicyRouteEffect(ctx, r.Effect, "del"); err != nil {
						return err
					}
					matching, _, err = c.observePolicyRouteEffect(ctx, r.Effect)
					if err != nil {
						return err
					}
					if matching {
						return errors.New("native policy deletion did not read back absent")
					}
				}
			}
			journal.Receipts = append(journal.Receipts[:i], journal.Receipts[i+1:]...)
			if err := c.savePolicyRouteJournal(family, journal); err != nil {
				return err
			}
		}
	}
	for _, e := range desired {
		position := -1
		for i, r := range journal.Receipts {
			if r.Scope == scope && r.Effect == e {
				position = i
				break
			}
		}
		matching, collision, err := c.observePolicyRouteEffect(ctx, e)
		if err != nil {
			return err
		}
		if position >= 0 && matching {
			if err := c.verifyPolicyRouteEffect(ctx, journal.Receipts[position]); err != nil {
				return err
			}
			continue
		}
		if collision {
			return errors.New("foreign native policy rule/route occupies the requested native tuple")
		}
		r := policyRouteReceipt{Effect: e, Scope: scope, Pending: "add"}
		if position < 0 {
			journal.Receipts = append(journal.Receipts, r)
			position = len(journal.Receipts) - 1
		} else {
			journal.Receipts[position] = r
		}
		if err := c.savePolicyRouteJournal(family, journal); err != nil {
			return err
		}
		if err := c.policyRoutePremise(ctx, family, scope, desired); err != nil {
			journal.Receipts = append(journal.Receipts[:position], journal.Receipts[position+1:]...)
			return errors.Join(err, c.savePolicyRouteJournal(family, journal))
		}
		if err := c.verifyPolicyRouteEffect(ctx, r); err != nil {
			return err
		}
		_, collision, err = c.observePolicyRouteEffect(ctx, e)
		if err != nil {
			return err
		}
		if collision {
			return errors.New("native policy native tuple changed before exclusive acquisition")
		}
		if err := c.mutatePolicyRouteEffect(ctx, e, "add"); err != nil {
			return err
		}
		matching, _, err = c.observePolicyRouteEffect(ctx, e)
		if err != nil {
			return err
		}
		if !matching {
			return errors.New("native policy acquisition did not read back the exact native tuple")
		}
		if err := c.verifyPolicyRouteEffect(ctx, r); err != nil {
			return err
		}
		journal.Receipts[position].Pending = ""
		if err := c.savePolicyRouteJournal(family, journal); err != nil {
			return err
		}
		if err := c.policyRoutePremise(ctx, family, scope, desired); err != nil {
			return err
		}
	}
	return c.policyRoutePremise(ctx, family, scope, desired)
}

func (c IPv4PolicyRouteController) mutatePolicyRouteEffect(ctx context.Context, e policyRouteEffect, verb string) error {
	args := []string{fmt.Sprintf("-%d", e.Family), e.Kind, verb}
	if e.Kind == "route" {
		args = append(args, "default")
		if e.Gateway != "" {
			args = append(args, "via", e.Gateway)
		}
		args = append(args, "dev", e.Interface, "table", strconv.Itoa(e.Table), "metric", strconv.Itoa(e.Metric), "proto", "static")
		if e.Source != "" {
			args = append(args, "src", e.Source)
		}
	} else {
		args = append(args, "priority", strconv.Itoa(e.Priority), "from", firstNonEmpty(e.SourceWithPrefix(), "all"))
		if e.Mark != 0 {
			args = append(args, "fwmark", fmt.Sprintf("0x%x/0xffffffff", e.Mark))
		}
		if e.InputIf != "" {
			args = append(args, "iif", e.InputIf)
		}
		args = append(args, "table", strconv.Itoa(e.Table), "protocol", "static")
	}
	out, err := c.commandOutput(ctx, "ip", args...)
	if err != nil {
		return fmt.Errorf("ip %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e policyRouteEffect) SourceWithPrefix() string {
	if e.Source == "" {
		return ""
	}
	return e.Source + "/128"
}

func (c IPv4PolicyRouteController) observePolicyRouteEffect(ctx context.Context, e policyRouteEffect) (bool, bool, error) {
	args := []string{fmt.Sprintf("-%d", e.Family), "-N", e.Kind, "show"}
	if e.Kind == "route" {
		args = append(args, "table", "all", "exact", "default")
	} else {
		args = append(args, "priority", strconv.Itoa(e.Priority))
	}
	out, err := c.commandOutput(ctx, "ip", args...)
	if err != nil {
		return false, false, fmt.Errorf("observe native policy %s: %w: %s", e.Kind, err, strings.TrimSpace(string(out)))
	}
	matching, collision := false, false
	candidates := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if e.Kind == "route" {
			table := 254 // iproute2 omits the main table even in table-all output.
			for i, field := range f {
				if field == "table" {
					if i+1 >= len(f) {
						return false, false, errors.New("native policy route lacks table identity")
					}
					table, err = strconv.Atoi(f[i+1])
					if err != nil {
						return false, false, err
					}
				}
			}
			// Other tables can carry RA expiry, multipath and route types that
			// have no bearing on our exact default-route identity.
			if table != e.Table {
				continue
			}
			if f[0] != "default" && f[0] != "0.0.0.0/0" && f[0] != "::/0" {
				return false, false, errors.New("native policy route observation contains an unexpected destination")
			}
			metric := 0
			gateway, device, protocol, source := "", "", "", ""
			valid := true
			for i := 1; i < len(f); i++ {
				key := f[i]
				if i+1 >= len(f) {
					valid = false
					break
				}
				value := f[i+1]
				i++
				switch key {
				case "dev":
					device = value
				case "src":
					source = value
				case "pref":
					if value != "medium" {
						valid = false
					}
				case "via":
					gateway = value
				case "metric":
					metric, err = strconv.Atoi(value)
					if err != nil {
						return false, false, err
					}
				case "proto":
					protocol = value
				case "scope":
					if value != "link" && value != "global" && value != "253" && value != "0" {
						valid = false
					}
				case "table":
					table, err = strconv.Atoi(value)
					if err != nil {
						return false, false, err
					}
				default:
					valid = false
				}
			}
			if table != e.Table || metric != e.Metric {
				continue
			}
			collision = true
			candidates++
			if valid && device == e.Interface && gateway == e.Gateway && source == e.Source && (protocol == "static" || protocol == "4") {
				matching = true
			}
		} else {
			priority, err := strconv.Atoi(strings.TrimSuffix(f[0], ":"))
			if err != nil {
				return false, false, errors.New("invalid native policy rule priority observation")
			}
			if priority != e.Priority {
				continue
			}
			from, mark, table, protocol, inputIf := "", "", "", "", ""
			valid := true
			for i := 1; i < len(f); i++ {
				if i+1 >= len(f) {
					valid = false
					break
				}
				key, value := f[i], f[i+1]
				i++
				switch key {
				case "from":
					from = value
				case "iif":
					inputIf = value
				case "fwmark":
					mark = value
				case "lookup":
					table = value
				case "proto":
					protocol = value
				default:
					valid = false
				}
			}
			parts := strings.Split(mark, "/")
			mask := uint64(0xffffffff)
			var n uint64
			if mark != "" {
				var parseErr error
				n, parseErr = strconv.ParseUint(parts[0], 0, 32)
				if parseErr != nil || len(parts) > 2 {
					return false, false, errors.New("invalid native policy rule mark observation")
				}
				if len(parts) == 2 {
					mask, parseErr = strconv.ParseUint(parts[1], 0, 32)
					if parseErr != nil {
						return false, false, errors.New("invalid native policy rule mask observation")
					}
				}
			} else {
				mask = 0 // No mark selector applies to every mark, including ours.
			}
			if e.Family == 4 {
				if uint64(e.Mark)&mask != n&mask {
					continue
				}
			} else {
				if e.InputIf != "" && inputIf != "" && e.InputIf != inputIf {
					continue
				}
				if e.Source != "" && from != "all" {
					prefix, parseErr := netip.ParsePrefix(from)
					if parseErr != nil {
						address, addrErr := netip.ParseAddr(from)
						if addrErr != nil {
							return false, false, errors.New("invalid native policy source selector")
						}
						prefix = netip.PrefixFrom(address, 128)
					}
					if !prefix.Contains(netip.MustParseAddr(e.Source)) {
						continue
					}
				}
			}
			collision = true
			exactSelector := e.Family == 4 && mark != "" && int(n) == e.Mark && mask == 0xffffffff && from == "all" && inputIf == ""
			if e.Family == 6 {
				expectedSource := "all"
				if e.Source != "" {
					expectedSource = e.Source
				}
				exactSelector = mark == "" && strings.TrimSuffix(from, "/128") == expectedSource && inputIf == e.InputIf
			}
			if exactSelector && table == strconv.Itoa(e.Table) && (protocol == "static" || protocol == "4") {
				candidates++
				if valid {
					matching = true
				}
			}
		}
	}
	if candidates > 1 {
		return false, false, errors.New("native policy native deletion identity is ambiguous")
	}
	return matching, collision, nil
}

func sortedPolicyRouteEffects(input []policyRouteEffect) ([]policyRouteEffect, error) {
	seen := map[policyRouteEffect]bool{}
	var out []policyRouteEffect
	for _, e := range input {
		if !validPolicyRouteEffect(e) {
			return nil, errors.New("invalid native policy native effect")
		}
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return policyRouteEffectLess(out[i], out[j]) })
	return out, nil
}
