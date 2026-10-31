// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/pdclient"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type dhcpv6AddressStore interface {
	ReadDHCPv6AddressRecord(string) (routerstate.DHCPv6AddressRecord, error)
	SaveDHCPv6AddressRecord(string, routerstate.DHCPv6AddressRecord, routerstate.DHCPv6AddressState) (routerstate.DHCPv6AddressRecord, error)
	DeleteDHCPv6AddressRecord(string, routerstate.DHCPv6AddressRecord) error
}

type dhcpv6AddressSource struct {
	Config                  routerstate.DHCPv6AddressSource
	Available               bool
	SupervisorOwner, Socket string
}

// DHCPv6AddressController owns native IA_NA addresses. The helper owns protocol
// exchanges; only a current acquisition authorizes a finite native /128.
type DHCPv6AddressController struct {
	Router, DeclaredRouter *api.Router
	Store                  Store
	OperatingSystem        platform.OS
	DryRun                 bool
	Now                    func() time.Time
	Source                 func(context.Context, string) (dhcpv6AddressSource, error)
	ReadLease              func(context.Context, dhcpv6AddressSource) (pdclient.Snapshot, error)
	Decline                func(context.Context, dhcpv6AddressSource, daemonapi.DHCPv6DeclineRequest) error
	ObserveAddress         func(context.Context, string, string, string) (delegatedAddressObservation, error)
	Command                commandFunc
	Parents                nativeParentReader
}

func (c DHCPv6AddressController) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}
func (c DHCPv6AddressController) currentOS() platform.OS {
	if c.OperatingSystem != "" {
		return c.OperatingSystem
	}
	p, _ := platform.Current()
	return p.OS
}

func (c DHCPv6AddressController) Reconcile(ctx context.Context) error {
	if c.DryRun {
		return nil
	}
	active := map[string]bool{}
	if c.Router != nil {
		for _, r := range c.Router.Spec.Resources {
			if r.APIVersion == api.NetAPIVersion && r.Kind == "DHCPv6Address" {
				active[r.Metadata.Name] = true
			}
		}
	}
	names := map[string]bool{}
	for name := range active {
		names[name] = true
	}
	if lister, ok := c.Store.(interface {
		ListObjectStatuses() ([]routerstate.ObjectStatus, error)
	}); ok {
		statuses, err := lister.ListObjectStatuses()
		if err != nil {
			return err
		}
		for _, s := range statuses {
			if s.APIVersion == api.NetAPIVersion && s.Kind == "DHCPv6Address" {
				names[s.Name] = true
			}
		}
	}
	if len(names) == 0 {
		return nil
	}
	store, ok := c.Store.(dhcpv6AddressStore)
	if !ok {
		return fmt.Errorf("DHCPv6Address requires durable native ownership storage")
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	var result error
	for _, name := range ordered {
		if err := c.reconcileAddress(ctx, store, name, active[name]); err != nil {
			result = errors.Join(result, fmt.Errorf("DHCPv6Address/%s: %w", name, err))
		}
	}
	return result
}

func (c DHCPv6AddressController) reconcileAddress(ctx context.Context, store dhcpv6AddressStore, name string, active bool) error {
	record, err := store.ReadDHCPv6AddressRecord(name)
	if err != nil {
		return err
	}
	state := record.State
	// The current record is local to this reconcile, including its nested journal.
	save := func() error {
		next, e := store.SaveDHCPv6AddressRecord(name, record, state)
		if e == nil {
			record = next
		}
		return e
	}
	state.Phase, state.Reason, state.Message = "Pending", "WaitingForLease", ""
	var source dhcpv6AddressSource
	if active {
		if c.Source == nil {
			return fmt.Errorf("DHCPv6 source unavailable")
		}
		source, err = c.Source(ctx, name)
		if err != nil {
			state.Message = err.Error()
		} else if source.Config.IfName != "" {
			if state.Source.IfName != "" && !sameDHCPv6Association(state.Source, source.Config) {
				state.Leases = nil
				state.PendingDeclines = nil
				state.SnapshotAt = time.Time{}
			}
			state.Source = source.Config
			if source.Available {
				read := c.ReadLease
				if read == nil {
					read = readDHCPv6AddressLease
				}
				snapshot, e := read(ctx, source)
				if e == nil {
					e = validateDHCPv6AddressSnapshot(snapshot, source.Config, c.now())
				}
				if e == nil {
					// Recheck the supervisor/configuration after the blocking socket read.
					current, checkErr := c.Source(ctx, name)
					if checkErr != nil || current != source {
						e = fmt.Errorf("DHCPv6 source changed while reading lease")
					}
				}
				if e != nil {
					state.Message = e.Error()
				} else if !snapshot.UpdatedAt.Before(state.SnapshotAt) {
					leases := slices.Clone(snapshot.Addresses)
					if snapshot.State == pdclient.StateReleased || snapshot.State == pdclient.StateExpired {
						leases = nil
					}
					if !slices.Equal(leases, state.Leases) || state.ServerDUID != snapshot.ServerDUID {
						state.SnapshotAt = snapshot.UpdatedAt
						state.Leases = leases
						state.ServerDUID = snapshot.ServerDUID
					}
				}
			}
		}
	} else {
		state.Leases = nil
		state.PendingDeclines = nil
	}
	now := c.now()
	state.Leases = slices.DeleteFunc(state.Leases, func(a pdclient.AddressSnapshot) bool {
		return remainingPDLifetime(a.AcquiredAt, uint32(a.Valid), now) == 0
	})
	state.PendingDeclines = slices.DeleteFunc(state.PendingDeclines, func(d routerstate.DHCPv6AddressDecline) bool {
		return d.ServerDUID != state.ServerDUID || !slices.ContainsFunc(state.Leases, func(a pdclient.AddressSnapshot) bool {
			return a.Address == d.Address && a.AcquiredAt.Equal(d.AcquiredAt)
		})
	})
	if err := save(); err != nil {
		return err
	}
	// First retire all addresses whose acquisition is no longer authorized.
	if state.Native != nil {
		for i := 0; i < len(state.Native.Addresses); {
			owned := state.Native.Addresses[i]
			obs, e := c.observe(ctx, state.Native.IfName, owned.Address, state.Native.ParentToken)
			if e != nil {
				return e
			}
			sameParent := obs.ifIndex == state.Native.IfIndex && obs.scope == state.Native.Scope && obs.parentToken == state.Native.ParentToken
			if !sameParent || !obs.present || owned.Held && !now.Before(owned.ValidUntil) {
				state.Native.Addresses = append(state.Native.Addresses[:i], state.Native.Addresses[i+1:]...)
				if err := save(); err != nil {
					return err
				}
				continue
			}
			if obs.dadFailed && owned.Held {
				address := netip.MustParsePrefix(owned.Address).Addr().String()
				if !slices.ContainsFunc(state.PendingDeclines, func(d routerstate.DHCPv6AddressDecline) bool {
					return d.Address == address && d.AcquiredAt.Equal(owned.AcquiredAt)
				}) {
					state.PendingDeclines = append(state.PendingDeclines, routerstate.DHCPv6AddressDecline{Address: address, AcquiredAt: owned.AcquiredAt, ServerDUID: state.ServerDUID})
					if err := save(); err != nil {
						return err
					}
				}
			}
			wanted := desiredDHCPv6Lease(state, owned.Address)
			retire := !active || !sameDHCPv6Association(state.Native.Source, state.Source) || wanted == nil || obs.dadFailed
			if owned.Pending != nil {
				// A lost acknowledgment cannot establish ownership of a present tuple.
				// Keep the journal until absence, expiry, or a changed parent proves it safe.
				state.Reason = "NativeOperationUncertain"
				if err := save(); err != nil {
					return err
				}
				i++
				continue
			}
			if retire {
				state.Native.Addresses[i].Pending = &routerstate.DHCPv6AddressIntent{Operation: "withdraw"}
				if err := save(); err != nil {
					return err
				}
				check, e := c.observe(ctx, state.Native.IfName, owned.Address, state.Native.ParentToken)
				if e != nil {
					return e
				}
				if check.ifIndex != obs.ifIndex || check.scope != obs.scope || check.parentToken != obs.parentToken {
					return fmt.Errorf("DHCPv6 native parent changed before withdrawal")
				}
				if check.present {
					command, args := ipv6StaticAddressDeleteCommand(c.currentOS(), state.Native.IfName, owned.Address)
					if e := c.command(ctx, command, args...); e != nil {
						return e
					}
					check, e = c.observe(ctx, state.Native.IfName, owned.Address, state.Native.ParentToken)
					if e != nil {
						return e
					}
					if check.present && check.ifIndex == obs.ifIndex && check.scope == obs.scope && check.parentToken == obs.parentToken {
						return fmt.Errorf("DHCPv6 address remained after withdrawal")
					}
				}
				state.Native.Addresses = append(state.Native.Addresses[:i], state.Native.Addresses[i+1:]...)
				if err := save(); err != nil {
					return err
				}
				continue
			}
			i++
		}
		if len(state.Native.Addresses) == 0 {
			state.Native = nil
			if err := save(); err != nil {
				return err
			}
		}
	}
	if !active {
		if state.Native != nil {
			return fmt.Errorf("DHCPv6 address withdrawal is pending")
		}
		return store.DeleteDHCPv6AddressRecord(name, record)
	}
	for _, d := range state.PendingDeclines {
		if !source.Available {
			continue
		}
		current, e := c.Source(ctx, name)
		if e != nil || current != source {
			continue
		}
		decline := c.Decline
		if decline == nil {
			decline = declineDHCPv6Address
		}
		e = decline(ctx, source, daemonapi.DHCPv6DeclineRequest{SupervisorOwner: source.SupervisorOwner, ServerDUID: d.ServerDUID, Addresses: []daemonapi.DHCPv6DeclineAddress{{Address: d.Address, AcquiredAt: d.AcquiredAt}}})
		if e != nil {
			state.Message = e.Error()
		}
	}
	applied := 0
	for _, lease := range state.Leases {
		address := lease.Address + "/128"
		if desiredDHCPv6Lease(state, address) == nil {
			continue
		}
		if err := c.applyAddress(ctx, store, name, &record, &state, lease); err != nil {
			return err
		}
		if state.Native != nil {
			for _, owned := range state.Native.Addresses {
				if owned.Address == address && owned.Held && owned.Pending == nil {
					obs, e := c.observe(ctx, state.Native.IfName, address, state.Native.ParentToken)
					if e != nil {
						return e
					}
					if obs.ifIndex == state.Native.IfIndex && obs.scope == state.Native.Scope && obs.parentToken == state.Native.ParentToken && obs.usable && c.now().Before(owned.ValidUntil) {
						applied++
					}
				}
			}
		}
	}
	if applied > 0 {
		state.Phase, state.Reason = "Applied", "AddressReady"
	} else if len(state.Leases) > 0 {
		state.Reason = "WaitingForDAD"
	}
	if len(state.PendingDeclines) > 0 {
		state.Reason = "DuplicateAddress"
	}
	return save()
}

func sameDHCPv6Association(a, b routerstate.DHCPv6AddressSource) bool {
	return a.IfName == b.IfName && a.ClientDUID == b.ClientDUID && a.IAID == b.IAID
}
func desiredDHCPv6Lease(state routerstate.DHCPv6AddressState, address string) *pdclient.AddressSnapshot {
	for i := range state.Leases {
		a := &state.Leases[i]
		if a.Address+"/128" != address {
			continue
		}
		if slices.ContainsFunc(state.PendingDeclines, func(d routerstate.DHCPv6AddressDecline) bool {
			return d.Address == a.Address && d.AcquiredAt.Equal(a.AcquiredAt)
		}) {
			return nil
		}
		return a
	}
	return nil
}
func (c DHCPv6AddressController) command(ctx context.Context, name string, args ...string) error {
	if c.Command != nil {
		return c.Command(ctx, name, args...)
	}
	return runCommandContext(ctx, name, args...)
}

func (c DHCPv6AddressController) applyAddress(ctx context.Context, store dhcpv6AddressStore, name string, record *routerstate.DHCPv6AddressRecord, state *routerstate.DHCPv6AddressState, lease pdclient.AddressSnapshot) error {
	address := lease.Address + "/128"
	index := -1
	if state.Native != nil {
		for i, a := range state.Native.Addresses {
			if a.Address == address {
				index = i
				break
			}
		}
	}
	preferredUntil := lease.AcquiredAt.Add(time.Duration(lease.Preferred) * time.Second)
	validUntil := lease.AcquiredAt.Add(time.Duration(lease.Valid) * time.Second)
	if index >= 0 {
		owned := state.Native.Addresses[index]
		if owned.Pending != nil {
			return nil
		}
		if owned.AcquiredAt.Equal(lease.AcquiredAt) && owned.PreferredUntil.Equal(preferredUntil) && owned.ValidUntil.Equal(validUntil) {
			return nil
		}
	}
	obs, err := c.observe(ctx, state.Source.IfName, address, "")
	if err != nil {
		return err
	}
	if obs.ifIndex == 0 {
		return fmt.Errorf("DHCPv6 native interface unavailable")
	}
	if index < 0 && len(obs.sameHost) > 0 {
		return fmt.Errorf("DHCPv6 address conflicts with an unowned native address")
	}
	if index >= 0 && (!obs.present || obs.ifIndex != state.Native.IfIndex || obs.scope != state.Native.Scope || obs.parentToken != state.Native.ParentToken) {
		return fmt.Errorf("DHCPv6 address identity changed before renewal")
	}
	current, err := c.Source(ctx, name)
	if err != nil {
		return err
	}
	if !sameDHCPv6Association(current.Config, state.Source) {
		return fmt.Errorf("DHCPv6 source changed before apply")
	}
	if remainingPDLifetime(lease.AcquiredAt, uint32(lease.Valid), c.now()) == 0 {
		return nil
	}
	if state.Native == nil {
		state.Native = &routerstate.DHCPv6AddressReceipt{Source: state.Source, IfName: state.Source.IfName, IfIndex: obs.ifIndex, Scope: obs.scope, ParentToken: obs.parentToken}
	}
	if state.Native.IfName != state.Source.IfName || state.Native.IfIndex != obs.ifIndex || state.Native.Scope != obs.scope || state.Native.ParentToken != obs.parentToken {
		return fmt.Errorf("previous DHCPv6 native parent still requires withdrawal")
	}
	fresh := index < 0
	if fresh {
		index = len(state.Native.Addresses)
		state.Native.Addresses = append(state.Native.Addresses, routerstate.DHCPv6OwnedAddress{Address: address})
	}
	state.Native.Addresses[index].Pending = &routerstate.DHCPv6AddressIntent{Operation: "apply", AcquiredAt: lease.AcquiredAt, PreferredUntil: preferredUntil, ValidUntil: validUntil}
	saved, err := store.SaveDHCPv6AddressRecord(name, *record, *state)
	if err != nil {
		return err
	}
	*record = saved
	check, err := c.observe(ctx, state.Source.IfName, address, state.Native.ParentToken)
	if err != nil {
		return err
	}
	if check.ifIndex != obs.ifIndex || check.scope != obs.scope || check.parentToken != obs.parentToken || fresh && len(check.sameHost) > 0 {
		return fmt.Errorf("DHCPv6 native identity changed before apply")
	}
	current, err = c.Source(ctx, name)
	if err != nil {
		return err
	}
	if !sameDHCPv6Association(current.Config, state.Source) {
		return fmt.Errorf("DHCPv6 source changed before apply")
	}
	preferred := remainingPDLifetime(lease.AcquiredAt, uint32(lease.Preferred), c.now())
	valid := remainingPDLifetime(lease.AcquiredAt, uint32(lease.Valid), c.now())
	if valid == 0 {
		return fmt.Errorf("DHCPv6 lease expired before apply")
	}
	command, args := ipv6DelegatedAddressApplyCommand(c.currentOS(), state.Source.IfName, address, preferred, valid)
	if fresh && c.currentOS() == platform.OSLinux {
		args[2] = "add"
	}
	if err := c.command(ctx, command, args...); err != nil {
		return err
	}
	check, err = c.observe(ctx, state.Source.IfName, address, state.Native.ParentToken)
	if err != nil {
		return err
	}
	if check.ifIndex != obs.ifIndex || check.scope != obs.scope || check.parentToken != obs.parentToken || !check.present || !check.lifetimeKnown || check.validSeconds == 0 || check.validSeconds > valid || check.preferredSeconds > preferred || check.preferredSeconds > check.validSeconds {
		return fmt.Errorf("DHCPv6 native lifetime or identity readback differs from apply")
	}
	state.Native.Addresses[index] = routerstate.DHCPv6OwnedAddress{Address: address, Held: true, AcquiredAt: lease.AcquiredAt, PreferredUntil: preferredUntil, ValidUntil: validUntil}
	saved, err = store.SaveDHCPv6AddressRecord(name, *record, *state)
	if err == nil {
		*record = saved
	}
	return err
}

func validateDHCPv6AddressSnapshot(s pdclient.Snapshot, source routerstate.DHCPv6AddressSource, now time.Time) error {
	if s.Resource != source.Worker || s.Interface != source.IfName || s.ClientDUID != source.ClientDUID || s.IAID != source.IAID || s.Mode != source.Mode || s.UpdatedAt.IsZero() || s.UpdatedAt.After(now) || len(s.Addresses) > 2048 {
		return fmt.Errorf("DHCPv6 lease identity or bounds differ from current source")
	}
	seen := map[netip.Addr]bool{}
	for _, a := range s.Addresses {
		ip, err := netip.ParseAddr(a.Address)
		if err != nil || !ip.Is6() || ip.Is4In6() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() || ip.Zone() != "" || ip.String() != a.Address || seen[ip] || a.AcquiredAt.IsZero() || a.AcquiredAt.After(now) || a.Valid <= 0 || a.Valid > 0xffffffff || a.Preferred < 0 || a.Preferred > a.Valid || s.ServerDUID == "" {
			return fmt.Errorf("DHCPv6 lease has invalid address authority")
		}
		seen[ip] = true
	}
	return nil
}

func (c DHCPv6AddressController) observe(ctx context.Context, ifname, address, parentToken string) (delegatedAddressObservation, error) {
	if c.ObserveAddress != nil {
		return c.ObserveAddress(ctx, ifname, address, parentToken)
	}
	parents := c.Parents
	parents.Router = c.DeclaredRouter
	parents.Store = c.Store
	read := func() (nativeParentLink, error) {
		if parentToken != "" {
			return parents.observeRecorded(ctx, ifname, parentToken)
		}
		return parents.observeDeclared(ctx, ifname)
	}
	before, err := read()
	if err != nil {
		return delegatedAddressObservation{}, err
	}
	if (before.Declared || parentToken != "") && !before.Exists {
		return delegatedAddressObservation{scope: before.Scope}, nil
	}
	observed, err := observeNativeIPv6Address(ctx, ifname, address, c.currentOS(), false)
	if err != nil {
		return observed, err
	}
	after, err := read()
	if err != nil {
		return observed, err
	}
	if before.Exists != after.Exists || before.Index != after.Index || before.Scope != after.Scope || before.Token != after.Token {
		return observed, fmt.Errorf("DHCPv6 native parent changed during observation")
	}
	if after.Exists {
		if observed.ifIndex != after.Index || observed.scope != after.Scope {
			return observed, fmt.Errorf("DHCPv6 native parent differs from interface")
		}
		observed.parentToken = after.Token
	}
	return observed, nil
}
