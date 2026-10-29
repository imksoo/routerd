// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"time"

	bgpstate "github.com/imksoo/routerd/pkg/bgp"
)

// FIBIntentReader belongs to the calling BGP actor and re-observes its current
// live routing authority after a durable pending write.
type FIBIntentReader func(context.Context) ([]FIBRoute, error)

type FIBStore interface {
	ReadBGPFIB(context.Context) (bgpstate.FIBJournal, bool, error)
	SaveBGPFIB(context.Context, bgpstate.FIBJournal) error
}

type fibLocalAddress struct {
	Address netip.Addr
	Prefix  netip.Prefix
}
type fibObservedRoute struct {
	Route bgpstate.NativeFIBRoute
	Exact bool
}
type fibSnapshot struct {
	KernelScope string
	Routes      []fibObservedRoute
	Addresses   []fibLocalAddress
}

// The backend observes native state and performs only exclusive adds and exact
// withdrawals. The journal and all recovery decisions have this one owner.
type fibBackend interface {
	Snapshot(context.Context) (fibSnapshot, error)
	Prepare(context.Context, FIBRoute, fibSnapshot) ([]bgpstate.NativeFIBRoute, string, error)
	Add(context.Context, bgpstate.NativeFIBRoute) error
	Delete(context.Context, bgpstate.NativeFIBRoute) error
}

type nativeFIBSyncer struct{ backend fibBackend }

func newFIBSyncResult() FIBSyncResult {
	return FIBSyncResult{Installed: map[string]bool{}, Unsupported: map[string]string{}, PreferredSource: map[string]string{}, PreferredSourceSkipped: map[string]bool{}, PreferredSourceSkippedReason: map[string]string{}}
}

func (s *nativeFIBSyncer) ClearBGP(ctx context.Context, store FIBStore) error {
	_, err := s.SyncBGP(ctx, store, nil, func(context.Context) ([]FIBRoute, error) { return nil, nil })
	return err
}

func (s *nativeFIBSyncer) SyncBGP(ctx context.Context, store FIBStore, routes []FIBRoute, current FIBIntentReader) (FIBSyncResult, error) {
	result := newFIBSyncResult()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if current == nil {
		return result, fmt.Errorf("current BGP FIB intent reader is unavailable")
	}
	if store == nil {
		return result, fmt.Errorf("BGP FIB journal is unavailable")
	}
	journal, found, err := store.ReadBGPFIB(ctx)
	if err != nil {
		return result, fmt.Errorf("read BGP FIB journal: %w", err)
	}
	if found {
		if err := bgpstate.ValidateFIBJournal(journal); err != nil {
			return result, err
		}
	}
	snapshot, err := s.backend.Snapshot(ctx)
	if err != nil {
		return result, err
	}
	if snapshot.KernelScope == "" {
		return result, fmt.Errorf("BGP FIB native identity is unavailable")
	}
	if found && journal.KernelScope != snapshot.KernelScope && (len(journal.Applied) > 0 || journal.Pending != nil) {
		return result, fmt.Errorf("BGP FIB journal belongs to another kernel network identity")
	}
	journal.KernelScope = snapshot.KernelScope
	if journal.Applied == nil {
		journal.Applied = []bgpstate.NativeFIBRoute{}
	}
	if journal.Pending != nil {
		if err := s.recover(ctx, store, &journal, snapshot); err != nil {
			return result, err
		}
		snapshot, err = s.backend.Snapshot(ctx)
		if err != nil {
			return result, err
		}
	}
	desired := map[string][]bgpstate.NativeFIBRoute{}
	rawDesired := map[string]FIBRoute{}
	for _, r := range routes {
		rawDesired[normalizeRoutePrefix(r.Prefix)] = r
		p, err := netip.ParsePrefix(r.Prefix)
		if err != nil {
			return result, fmt.Errorf("invalid desired BGP FIB prefix %q", r.Prefix)
		}
		r.Prefix = p.Masked().String()
		localHost := false
		for _, a := range snapshot.Addresses {
			if a.Address == p.Addr() && p.Bits() == a.Address.BitLen() {
				localHost = true
			}
		}
		if localHost {
			continue
		}
		if r.PreferredSource == "" {
			best := -1
			for _, a := range snapshot.Addresses {
				if a.Address.Is4() == p.Addr().Is4() && a.Address != p.Addr() && a.Prefix.Contains(p.Addr()) && a.Prefix.Bits() > best {
					r.PreferredSource = a.Address.String()
					best = a.Prefix.Bits()
				}
			}
		}
		if r.PreferredSource != "" {
			local := false
			for _, a := range snapshot.Addresses {
				local = local || a.Address.String() == r.PreferredSource
			}
			if !local {
				result.PreferredSourceSkipped[r.Prefix] = true
				result.PreferredSourceSkippedReason[r.Prefix] = "LocalAddressMissing"
				r.PreferredSource = ""
			}
		}
		native, unsupported, err := s.backend.Prepare(ctx, r, snapshot)
		if err != nil {
			return result, err
		}
		if unsupported != "" {
			result.Unsupported[r.Prefix] = unsupported
			continue
		}
		if _, ok := desired[r.Prefix]; ok {
			return result, fmt.Errorf("duplicate desired BGP FIB prefix %s", r.Prefix)
		}
		desired[r.Prefix] = native
	}
	// Withdraw from recorded ownership before adding a replacement. Native routes
	// that merely use the BGP protocol number are never imported into this ledger.
	for _, owned := range append([]bgpstate.NativeFIBRoute(nil), journal.Applied...) {
		wanted := desired[owned.Prefix]
		match := -1
		for i, r := range wanted {
			if bgpstate.EqualNativeFIBRoute(owned, r) {
				match = i
				break
			}
		}
		if match >= 0 && snapshot.exact(owned) && snapshot.conflicts(owned, journal.Applied) {
			return result, fmt.Errorf("BGP FIB prefix %s also contains an unowned native route", owned.Prefix)
		}
		if match >= 0 && snapshot.exact(owned) {
			result.Installed[owned.Prefix] = true
			desired[owned.Prefix] = append(wanted[:match], wanted[match+1:]...)
			continue
		}
		journal.Pending = &bgpstate.FIBOperation{Action: "withdraw", Route: owned}
		if err := store.SaveBGPFIB(ctx, journal); err != nil {
			return result, err
		}
		if err := s.withdrawCurrent(ctx, store, &journal, current, routes); err != nil {
			return result, err
		}
		snapshot, err = s.backend.Snapshot(ctx)
		if err != nil {
			return result, err
		}
	}
	prefixes := make([]string, 0, len(desired))
	for p := range desired {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)
	for _, prefix := range prefixes {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		for _, native := range desired[prefix] {
			if snapshot.conflicts(native, journal.Applied) {
				return result, fmt.Errorf("BGP FIB prefix %s conflicts with an unowned native route", prefix)
			}
			journal.Pending = &bgpstate.FIBOperation{Action: "add", Route: native}
			if err := store.SaveBGPFIB(ctx, journal); err != nil {
				return result, err
			}
			if err := s.addCurrent(ctx, store, &journal, current, routes, rawDesired[prefix]); err != nil {
				return result, err
			}
			snapshot, err = s.backend.Snapshot(ctx)
			if err != nil {
				return result, err
			}
			result.Installed[prefix] = true
		}
	}
	for prefix := range result.Installed {
		source := ""
		seen := false
		for _, receipt := range journal.Applied {
			if receipt.Prefix != prefix {
				continue
			}
			if !seen {
				source = receipt.PreferredSource
				seen = true
			} else if receipt.PreferredSource != source {
				source = ""
				break
			}
		}
		if source != "" {
			result.PreferredSource[prefix] = source
		}
	}
	return result, nil
}

func (s *nativeFIBSyncer) recover(ctx context.Context, store FIBStore, j *bgpstate.FIBJournal, snapshot fibSnapshot) error {
	p := j.Pending
	if p == nil {
		return nil
	}
	present := snapshot.exact(p.Route)
	if p.Action == "add" {
		present = present || snapshot.conflicts(p.Route, j.Applied)
	}
	if present {
		p.Unknown = true
		if err := store.SaveBGPFIB(ctx, *j); err != nil {
			return err
		}
		return fmt.Errorf("BGP FIB %s %s has an unknown prior outcome; inspect and remove the ambiguous native route before retrying", p.Action, p.Route.Prefix)
	}
	return completeFIBOperation(ctx, store, j)
}

func (s *nativeFIBSyncer) withdrawCurrent(ctx context.Context, store FIBStore, j *bgpstate.FIBJournal, current FIBIntentReader, planned []FIBRoute) (err error) {
	issued := false
	defer func() {
		if !issued && j.Pending != nil {
			err = errors.Join(err, abandonUnissuedFIB(ctx, store, j))
		}
	}()
	if err := checkFIBIntent(ctx, current, planned); err != nil {
		return err
	}
	snapshot, err := s.backend.Snapshot(ctx)
	if err != nil {
		return err
	}
	if snapshot.KernelScope != j.KernelScope {
		return fmt.Errorf("BGP FIB native identity changed before withdrawal")
	}
	route := j.Pending.Route
	if snapshot.exact(route) {
		if err := ctx.Err(); err != nil {
			return err
		}
		issued = true
		if err := s.backend.Delete(ctx, route); err != nil {
			return fmt.Errorf("withdraw BGP FIB route %s: %w", route.Prefix, err)
		}
		observed, err := s.backend.Snapshot(ctx)
		if err != nil {
			return err
		}
		if observed.KernelScope != j.KernelScope {
			return fmt.Errorf("BGP FIB native identity changed during withdrawal")
		}
		if observed.exact(route) {
			return fmt.Errorf("BGP FIB withdrawal %s is still present", route.Prefix)
		}
	}
	return completeFIBOperation(ctx, store, j)
}

func (s *nativeFIBSyncer) addCurrent(ctx context.Context, store FIBStore, j *bgpstate.FIBJournal, current FIBIntentReader, planned []FIBRoute, raw FIBRoute) (err error) {
	issued := false
	defer func() {
		if !issued && j.Pending != nil {
			err = errors.Join(err, abandonUnissuedFIB(ctx, store, j))
		}
	}()
	if err := checkFIBIntent(ctx, current, planned); err != nil {
		return err
	}
	snapshot, err := s.backend.Snapshot(ctx)
	if err != nil {
		return err
	}
	if snapshot.KernelScope != j.KernelScope {
		return fmt.Errorf("BGP FIB native identity changed before add")
	}
	native := j.Pending.Route
	if snapshot.conflicts(native, j.Applied) {
		return fmt.Errorf("BGP FIB prefix %s changed before add", native.Prefix)
	}
	prepared, _, err := s.prepareCurrent(ctx, raw, snapshot)
	if err != nil {
		return err
	}
	stillDesired := false
	for _, r := range prepared {
		stillDesired = stillDesired || bgpstate.EqualNativeFIBRoute(r, native)
	}
	if !stillDesired {
		return fmt.Errorf("BGP FIB route %s topology changed before add", native.Prefix)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	issued = true
	if err := s.backend.Add(ctx, native); err != nil {
		return fmt.Errorf("add BGP FIB route %s: %w", native.Prefix, err)
	}
	observed, err := s.backend.Snapshot(ctx)
	if err != nil {
		return err
	}
	if observed.KernelScope != j.KernelScope {
		return fmt.Errorf("BGP FIB native identity changed during add")
	}
	if !observed.exact(native) {
		return fmt.Errorf("BGP FIB route %s did not match its native write", native.Prefix)
	}
	j.Applied = append(j.Applied, native)
	j.Pending = nil
	return store.SaveBGPFIB(ctx, *j)
}

// The invocation knows no native operation was issued. A crash cannot make this
// claim, so startup recovery never uses this path. Cleanup has its own bounded
// context when request cancellation interrupted the pre-effect checks.
func abandonUnissuedFIB(ctx context.Context, store FIBStore, j *bgpstate.FIBJournal) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	next := *j
	next.Pending = nil
	if err := store.SaveBGPFIB(cleanup, next); err != nil {
		return fmt.Errorf("clear unissued BGP FIB operation: %w", err)
	}
	j.Pending = nil
	return nil
}

func completeFIBOperation(ctx context.Context, store FIBStore, j *bgpstate.FIBJournal) error {
	p := j.Pending
	if p.Action == "withdraw" {
		applied := make([]bgpstate.NativeFIBRoute, 0, len(j.Applied))
		for _, r := range j.Applied {
			if !bgpstate.EqualNativeFIBRoute(r, p.Route) {
				applied = append(applied, r)
			}
		}
		j.Applied = applied
	}
	j.Pending = nil
	return store.SaveBGPFIB(ctx, *j)
}

func checkFIBIntent(ctx context.Context, current FIBIntentReader, planned []FIBRoute) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	live, err := current(ctx)
	if err != nil {
		return err
	}
	if !sameFIBIntent(live, planned) {
		return fmt.Errorf("BGP FIB desired routes changed before native effect")
	}
	return ctx.Err()
}
func sameFIBIntent(a, b []FIBRoute) bool {
	normalize := func(routes []FIBRoute) []FIBRoute {
		out := make([]FIBRoute, len(routes))
		for i, r := range routes {
			r.Prefix = normalizeRoutePrefix(r.Prefix)
			r.NextHops = normalizeRouteNextHops(r.NextHops)
			out[i] = r
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })
		return out
	}
	a, b = normalize(a), normalize(b)
	if len(a) != len(b) {
		return false
	}
	for i, r := range a {
		if r.Prefix != b[i].Prefix || r.PreferredSource != b[i].PreferredSource || !slices.Equal(r.NextHops, b[i].NextHops) {
			return false
		}
	}
	return true
}

func (s *nativeFIBSyncer) prepareCurrent(ctx context.Context, r FIBRoute, snapshot fibSnapshot) ([]bgpstate.NativeFIBRoute, string, error) {
	p, err := netip.ParsePrefix(r.Prefix)
	if err != nil {
		return nil, "", err
	}
	r.Prefix = p.Masked().String()
	if r.PreferredSource == "" {
		best := -1
		for _, a := range snapshot.Addresses {
			if a.Address.Is4() == p.Addr().Is4() && a.Address != p.Addr() && a.Prefix.Contains(p.Addr()) && a.Prefix.Bits() > best {
				r.PreferredSource = a.Address.String()
				best = a.Prefix.Bits()
			}
		}
	}
	if r.PreferredSource != "" {
		local := false
		for _, a := range snapshot.Addresses {
			local = local || a.Address.String() == r.PreferredSource
		}
		if !local {
			r.PreferredSource = ""
		}
	}
	return s.backend.Prepare(ctx, r, snapshot)
}

func (s fibSnapshot) exact(want bgpstate.NativeFIBRoute) bool {
	for _, r := range s.Routes {
		if r.Exact && bgpstate.EqualNativeFIBRoute(r.Route, want) {
			return true
		}
	}
	return false
}

func (s fibSnapshot) conflicts(want bgpstate.NativeFIBRoute, owned []bgpstate.NativeFIBRoute) bool {
	for _, r := range s.Routes {
		if r.Route.Prefix != want.Prefix {
			continue
		}
		known := false
		if r.Exact {
			for _, receipt := range owned {
				if bgpstate.EqualNativeFIBRoute(receipt, r.Route) {
					known = true
					break
				}
			}
		}
		if !known {
			return true
		}
	}
	return false
}
