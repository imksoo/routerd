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
type policyIPv4Effect struct {
	Kind      string `json:"kind"`
	Table     int    `json:"table"`
	Priority  int    `json:"priority"`
	Mark      int    `json:"mark"`
	Metric    int    `json:"metric"`
	Interface string `json:"interface"`
	Gateway   string `json:"gateway"`
	IfIndex   int    `json:"ifIndex"`
}

type policyIPv4Receipt struct {
	Effect  policyIPv4Effect `json:"effect"`
	Scope   string           `json:"scope"`
	Pending string           `json:"pending"`
}

type policyIPv4Journal struct{ Receipts []policyIPv4Receipt }

const policyIPv4JournalName = "ipv4-policy-native"

func validPolicyIPv4Effect(e policyIPv4Effect) bool {
	if e.Table < 1 || e.Table > 2147483647 {
		return false
	}
	switch e.Kind {
	case "rule":
		return e.Priority > 0 && e.Priority <= 2147483647 && e.Mark > 0 && uint64(e.Mark) <= 1<<32-1 && e.Metric == 0 && e.Interface == "" && e.Gateway == "" && e.IfIndex == 0
	case "route":
		if e.Priority != 0 || e.Mark != 0 || e.Metric < 0 || e.Interface == "" || e.IfIndex < 1 {
			return false
		}
		if e.Gateway != "" {
			ip, err := netip.ParseAddr(e.Gateway)
			return err == nil && ip.Is4()
		}
		return true
	default:
		return false
	}
}

// Decode the existing status boundary explicitly. The journal has one typed
// in-memory representation; JSON encoding is owned by the SQLite status store.
func (c IPv4PolicyRouteController) readPolicyIPv4Journal() (policyIPv4Journal, error) {
	status, err := c.Store.ReadObjectStatus(api.RouterAPIVersion, "Router", policyIPv4JournalName)
	if err != nil {
		return policyIPv4Journal{}, err
	}
	journal := policyIPv4Journal{Receipts: []policyIPv4Receipt{}}
	if len(status) == 0 {
		return journal, nil
	}
	version, ok := statusInt(status["version"])
	if !ok || version != 1 {
		return journal, errors.New("unsupported IPv4 policy ownership journal")
	}
	switch rows := status["receipts"].(type) {
	case []policyIPv4Receipt:
		journal.Receipts = append(journal.Receipts, rows...)
	case []any:
		for _, row := range rows {
			r, ok := row.(map[string]any)
			if !ok {
				return journal, errors.New("invalid IPv4 policy receipt")
			}
			effect, ok := r["effect"].(map[string]any)
			if !ok {
				return journal, errors.New("invalid IPv4 policy effect")
			}
			var e policyIPv4Effect
			for key, target := range map[string]*int{"table": &e.Table, "priority": &e.Priority, "mark": &e.Mark, "metric": &e.Metric, "ifIndex": &e.IfIndex} {
				n, ok := statusInt(effect[key])
				if !ok {
					return journal, fmt.Errorf("invalid IPv4 policy %s", key)
				}
				*target = n
			}
			for key, target := range map[string]*string{"kind": &e.Kind, "interface": &e.Interface, "gateway": &e.Gateway} {
				s, ok := effect[key].(string)
				if !ok {
					return journal, fmt.Errorf("invalid IPv4 policy %s", key)
				}
				*target = s
			}
			scope, ok := r["scope"].(string)
			if !ok {
				return journal, errors.New("invalid IPv4 policy kernel scope")
			}
			pending, ok := r["pending"].(string)
			if !ok {
				return journal, errors.New("invalid IPv4 policy pending operation")
			}
			journal.Receipts = append(journal.Receipts, policyIPv4Receipt{Effect: e, Scope: scope, Pending: pending})
		}
	default:
		return journal, errors.New("IPv4 policy receipts are not an array")
	}
	seen := map[policyIPv4Receipt]bool{}
	for _, r := range journal.Receipts {
		if r.Scope == "" || !validPolicyIPv4Effect(r.Effect) || r.Pending != "" && r.Pending != "add" && r.Pending != "delete" || seen[r] {
			return journal, errors.New("invalid IPv4 policy ownership journal")
		}
		seen[r] = true
	}
	return journal, nil
}

func (c IPv4PolicyRouteController) savePolicyIPv4Journal(j policyIPv4Journal) error {
	return c.Store.SaveObjectStatus(api.RouterAPIVersion, "Router", policyIPv4JournalName, map[string]any{"version": 1, "receipts": append([]policyIPv4Receipt{}, j.Receipts...)})
}

func (c IPv4PolicyRouteController) policyIPv4Scope(ctx context.Context) (string, error) {
	if c.NativeScope != nil {
		return c.NativeScope(ctx)
	}
	return platform.KernelNetworkScope(ctx)
}

func (c IPv4PolicyRouteController) policyIPv4Index(ctx context.Context, ifname string) (int, bool, error) {
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

func (c IPv4PolicyRouteController) verifyPolicyIPv4Effect(ctx context.Context, r policyIPv4Receipt) error {
	scope, err := c.policyIPv4Scope(ctx)
	if err != nil {
		return err
	}
	if scope == "" || scope != r.Scope {
		return errors.New("IPv4 policy kernel scope changed")
	}
	if r.Effect.Kind == "route" {
		index, present, err := c.policyIPv4Index(ctx, r.Effect.Interface)
		if err != nil {
			return err
		}
		if !present || index != r.Effect.IfIndex {
			return errors.New("IPv4 policy native interface identity changed")
		}
	}
	return nil
}

func policyIPv4EffectLess(a, b policyIPv4Effect) bool {
	// Routes acquire before rules; withdrawal uses the reverse order.
	if a.Kind != b.Kind {
		return a.Kind == "route"
	}
	return fmt.Sprint(a) < fmt.Sprint(b)
}

func samePolicyIPv4Effects(a, b []policyIPv4Effect) bool {
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

func (c IPv4PolicyRouteController) policyIPv4Premise(ctx context.Context, scope string, desired []policyIPv4Effect) error {
	current, err := c.policyIPv4Scope(ctx)
	if err != nil {
		return err
	}
	if current == "" || current != scope {
		return errors.New("IPv4 policy kernel scope changed")
	}
	fresh, err := c.desiredPolicyIPv4Effects(ctx)
	if err != nil {
		return err
	}
	if !samePolicyIPv4Effects(fresh, desired) {
		return errors.New("IPv4 policy intent or source changed while persistence waited")
	}
	return nil
}

func (c IPv4PolicyRouteController) reconcilePolicyIPv4Effects(ctx context.Context, desired []policyIPv4Effect) error {
	if c.DryRun {
		return nil
	}
	journal, err := c.readPolicyIPv4Journal()
	if err != nil {
		return err
	}
	if len(desired) == 0 && len(journal.Receipts) == 0 {
		return nil
	}
	scope, err := c.policyIPv4Scope(ctx)
	if err != nil {
		return err
	}
	if scope == "" {
		return errors.New("IPv4 policy kernel scope is unavailable")
	}
	wanted := map[policyIPv4Effect]bool{}
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
		matching, _, err := c.observePolicyIPv4Effect(ctx, r.Effect)
		if err != nil {
			return err
		}
		if matching {
			return errors.New("pending IPv4 policy effect is present without an acknowledged owner; inspect and remove it before retrying")
		}
		journal.Receipts = append(journal.Receipts[:i], journal.Receipts[i+1:]...)
		if err := c.savePolicyIPv4Journal(journal); err != nil {
			return err
		}
	}
	// Check every new tuple before changing any forwarding effect. A foreign
	// selector must not gain our route merely because routes acquire first.
	for _, e := range desired {
		_, collision, err := c.observePolicyIPv4Effect(ctx, e)
		if err != nil {
			return err
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
			matching, _, err := c.observePolicyIPv4Effect(ctx, r.Effect)
			if err != nil {
				return err
			}
			if matching {
				if err := c.verifyPolicyIPv4Effect(ctx, r); err != nil {
					return err
				}
				owned = true
				break
			}
		}
		if !owned {
			return errors.New("foreign IPv4 policy rule/route occupies the requested native tuple")
		}
	}
	// Stop selectors before removing their routes. Never flush a shared table.
	for _, kind := range []string{"rule", "route"} {
		for i := 0; i < len(journal.Receipts); {
			r := journal.Receipts[i]
			if r.Scope != scope || r.Effect.Kind != kind || wanted[r.Effect] {
				i++
				continue
			}
			matching, _, err := c.observePolicyIPv4Effect(ctx, r.Effect)
			if err != nil {
				return err
			}
			if matching {
				if err := c.verifyPolicyIPv4Effect(ctx, r); err != nil {
					return err
				}
				journal.Receipts[i].Pending = "delete"
				if err := c.savePolicyIPv4Journal(journal); err != nil {
					return err
				}
				if err := c.policyIPv4Premise(ctx, scope, desired); err != nil {
					journal.Receipts[i].Pending = ""
					return errors.Join(err, c.savePolicyIPv4Journal(journal))
				}
				if err := c.verifyPolicyIPv4Effect(ctx, r); err != nil {
					return err
				}
				matching, _, err = c.observePolicyIPv4Effect(ctx, r.Effect)
				if err != nil {
					return err
				}
				if matching {
					if err := c.mutatePolicyIPv4Effect(ctx, r.Effect, "del"); err != nil {
						return err
					}
					matching, _, err = c.observePolicyIPv4Effect(ctx, r.Effect)
					if err != nil {
						return err
					}
					if matching {
						return errors.New("IPv4 policy deletion did not read back absent")
					}
				}
			}
			journal.Receipts = append(journal.Receipts[:i], journal.Receipts[i+1:]...)
			if err := c.savePolicyIPv4Journal(journal); err != nil {
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
		matching, collision, err := c.observePolicyIPv4Effect(ctx, e)
		if err != nil {
			return err
		}
		if position >= 0 && matching {
			if err := c.verifyPolicyIPv4Effect(ctx, journal.Receipts[position]); err != nil {
				return err
			}
			continue
		}
		if collision {
			return errors.New("foreign IPv4 policy rule/route occupies the requested native tuple")
		}
		r := policyIPv4Receipt{Effect: e, Scope: scope, Pending: "add"}
		if position < 0 {
			journal.Receipts = append(journal.Receipts, r)
			position = len(journal.Receipts) - 1
		} else {
			journal.Receipts[position] = r
		}
		if err := c.savePolicyIPv4Journal(journal); err != nil {
			return err
		}
		if err := c.policyIPv4Premise(ctx, scope, desired); err != nil {
			journal.Receipts = append(journal.Receipts[:position], journal.Receipts[position+1:]...)
			return errors.Join(err, c.savePolicyIPv4Journal(journal))
		}
		if err := c.verifyPolicyIPv4Effect(ctx, r); err != nil {
			return err
		}
		_, collision, err = c.observePolicyIPv4Effect(ctx, e)
		if err != nil {
			return err
		}
		if collision {
			return errors.New("IPv4 policy native tuple changed before exclusive acquisition")
		}
		if err := c.mutatePolicyIPv4Effect(ctx, e, "add"); err != nil {
			return err
		}
		matching, _, err = c.observePolicyIPv4Effect(ctx, e)
		if err != nil {
			return err
		}
		if !matching {
			return errors.New("IPv4 policy acquisition did not read back the exact native tuple")
		}
		if err := c.verifyPolicyIPv4Effect(ctx, r); err != nil {
			return err
		}
		journal.Receipts[position].Pending = ""
		if err := c.savePolicyIPv4Journal(journal); err != nil {
			return err
		}
		if err := c.policyIPv4Premise(ctx, scope, desired); err != nil {
			return err
		}
	}
	return c.policyIPv4Premise(ctx, scope, desired)
}

func (c IPv4PolicyRouteController) mutatePolicyIPv4Effect(ctx context.Context, e policyIPv4Effect, verb string) error {
	args := []string{"-4", e.Kind, verb}
	if e.Kind == "route" {
		args = append(args, "default")
		if e.Gateway != "" {
			args = append(args, "via", e.Gateway)
		}
		args = append(args, "dev", e.Interface, "table", strconv.Itoa(e.Table), "metric", strconv.Itoa(e.Metric), "proto", "static")
	} else {
		args = append(args, "priority", strconv.Itoa(e.Priority), "from", "all", "fwmark", fmt.Sprintf("0x%x/0xffffffff", e.Mark), "table", strconv.Itoa(e.Table), "protocol", "static")
	}
	out, err := c.commandOutput(ctx, "ip", args...)
	if err != nil {
		return fmt.Errorf("ip %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c IPv4PolicyRouteController) observePolicyIPv4Effect(ctx context.Context, e policyIPv4Effect) (bool, bool, error) {
	args := []string{"-4", e.Kind, "show"}
	if e.Kind == "route" {
		args = append(args, "default", "table", strconv.Itoa(e.Table))
	} else {
		args = append(args, "priority", strconv.Itoa(e.Priority))
	}
	out, err := c.commandOutput(ctx, "ip", args...)
	if err != nil {
		return false, false, fmt.Errorf("observe IPv4 policy %s: %w: %s", e.Kind, err, strings.TrimSpace(string(out)))
	}
	matching, collision := false, false
	candidates := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if e.Kind == "route" {
			if f[0] != "default" && f[0] != "0.0.0.0/0" {
				return false, false, errors.New("IPv4 policy route observation contains an unexpected destination")
			}
			metric := 0
			gateway, device, protocol := "", "", ""
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
					if value != "link" && value != "global" {
						valid = false
					}
				case "table":
					if value != strconv.Itoa(e.Table) {
						valid = false
					}
				default:
					valid = false
				}
			}
			if metric != e.Metric {
				continue
			}
			collision = true
			candidates++
			if valid && device == e.Interface && gateway == e.Gateway && protocol == "static" {
				matching = true
			}
		} else {
			priority, err := strconv.Atoi(strings.TrimSuffix(f[0], ":"))
			if err != nil {
				return false, false, errors.New("invalid IPv4 policy rule priority observation")
			}
			if priority != e.Priority {
				continue
			}
			from, mark, table, protocol := "", "", "", ""
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
			n, err := strconv.ParseUint(parts[0], 0, 32)
			mask := uint64(0xffffffff)
			if len(parts) > 2 {
				valid = false
			}
			if len(parts) == 2 {
				var maskErr error
				mask, maskErr = strconv.ParseUint(parts[1], 0, 32)
				if maskErr != nil {
					valid = false
				}
			}
			if err == nil && int(n) == e.Mark && mask == 0xffffffff && table == strconv.Itoa(e.Table) && from == "all" {
				collision = true
				candidates++
				if valid && protocol == "static" {
					matching = true
				}
			}
		}
	}
	if candidates > 1 {
		return false, false, errors.New("IPv4 policy native deletion identity is ambiguous")
	}
	return matching, collision, nil
}

func sortedPolicyIPv4Effects(input []policyIPv4Effect) ([]policyIPv4Effect, error) {
	seen := map[policyIPv4Effect]bool{}
	var out []policyIPv4Effect
	for _, e := range input {
		if !validPolicyIPv4Effect(e) {
			return nil, errors.New("invalid IPv4 policy native effect")
		}
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return policyIPv4EffectLess(out[i], out[j]) })
	return out, nil
}
