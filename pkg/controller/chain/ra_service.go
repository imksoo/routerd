// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
	ra "github.com/imksoo/routerd/pkg/routeradvertisement"
	state "github.com/imksoo/routerd/pkg/state"
	"github.com/imksoo/routerd/pkg/vxlan"
	"github.com/mdlayher/ndp"
)

const raJournalName = "router-advertisements-native"

type raResourceIntent struct {
	StatusKind    string
	ResourceID    string
	Name          string
	Spec          api.IPv6RouterAdvertisementSpec
	Limits        api.RouterAdvertisementLifetimes
	Delegated     *api.IPv6DelegatedAddressSpec
	DelegatedName string
	SelfDNS       []raSelfDNSCandidate
	Native        bool
	Autonomous    bool
}

type raInterfacePlan struct {
	Interface         string
	Resources         []raResourceIntent
	MFlag, OFlag      bool
	MTU               int
	Preference        string
	RouterLifetime    uint16
	NativeIndex       int
	NativeScope       string
	NativeParentToken string
}

type raLoopState struct {
	cancel context.CancelFunc
	wake   chan struct{}
	first  chan error
	done   chan struct{}
	err    error
}

// RouterAdvertisementService is the sole routerd RA owner for a generation.
// Its mutex serializes source observation, pending receipt, wire send, and
// status commit against configuration replacement. Receipts only authorize
// withdrawal; current prefixes always come from current configuration/PD.
type RouterAdvertisementService struct {
	mu            sync.Mutex
	lifecycleMu   sync.Mutex
	lifetimeLoops []*raLoopState
	ctx           context.Context
	cancel        context.CancelFunc
	dryRun        bool
	store         Store
	declared      *api.Router
	preparedFor   *api.Router
	prepared      []raInterfacePlan
	resultsDirty  map[string]bool
	plans         map[string]raInterfacePlan
	loops         map[string]*raLoopState
	open          func(string) (ra.Endpoint, error)
	segments      *VXLANSegmentController
	scope         func(context.Context) (string, error)
	clock         func() time.Time
}

func NewRouterAdvertisementService(ctx context.Context, dryRun bool) *RouterAdvertisementService {
	ctx, cancel := context.WithCancel(ctx)
	return &RouterAdvertisementService{ctx: ctx, cancel: cancel, dryRun: dryRun, plans: map[string]raInterfacePlan{}, loops: map[string]*raLoopState{}, open: ra.Listen, scope: platform.KernelNetworkScope, clock: func() time.Time { return time.Now().UTC() }}
}

func (s *RouterAdvertisementService) Stop(ctx context.Context) error {
	s.lifecycleMu.Lock()
	s.cancel()
	loops := append([]*raLoopState(nil), s.lifetimeLoops...)
	s.lifecycleMu.Unlock()
	var errs []error
	for _, loop := range loops {
		select {
		case <-loop.done:
			errs = append(errs, loop.err)
		case <-ctx.Done():
			return errors.Join(append(errs, ctx.Err())...)
		}
	}
	return errors.Join(errs...)
}

func (s *RouterAdvertisementService) Reconcile(ctx context.Context, declared, effective *api.Router, store Store) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if err := saveSuppressedRAStatuses(declared, effective, store, s.dryRun); err != nil {
		return err
	}
	s.mu.Lock()
	configurationChanged := s.preparedFor != declared
	if configurationChanged {
		prepared, err := prepareRAPlans(declared)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		s.preparedFor, s.prepared = declared, prepared
	}
	plans, selectionErr := selectRAPlans(s.prepared, effective)
	s.store = store
	s.declared = declared
	s.plans = plans
	var outcomes []error
	if selectionErr != nil {
		outcomes = append(outcomes, selectionErr)
		var conflicts *raPlanConflicts
		if errors.As(selectionErr, &conflicts) {
			for _, ifname := range conflicts.Interfaces {
				outcomes = append(outcomes, s.publishRAFailure(ifname, selectionErr))
			}
		}
	}
	if s.dryRun {
		defer s.mu.Unlock()
		for _, plan := range plans {
			_, statuses, err := buildRAAdvertisement(ctx, declared, store, plan, time.Now().UTC())
			if err != nil {
				return err
			}
			if err := saveRAStatuses(store, statuses, true); err != nil {
				return err
			}
		}
		return errors.Join(outcomes...)
	}
	journal, err := readRAJournal(store)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	interfaces := map[string]bool{}
	for ifname := range plans {
		interfaces[ifname] = true
	}
	for ifname := range journal {
		interfaces[ifname] = true
	}
	if configurationChanged {
		if s.resultsDirty == nil {
			s.resultsDirty = map[string]bool{}
		}
		stale, err := unconfiguredRAResults(store, declared)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		for _, object := range stale {
			ifname := statusvalue.Field(object.Status, "ifname")
			if interfaces[ifname] {
				s.resultsDirty[ifname] = true
			} else {
				deleter, ok := store.(state.ObjectDeleteStore)
				if !ok {
					s.mu.Unlock()
					return fmt.Errorf("RA result deletion unavailable")
				}
				if err := deleter.DeleteObject(object.APIVersion, object.Kind, object.Name); err != nil {
					s.mu.Unlock()
					return err
				}
			}
		}
	}
	var waits []*raLoopState
	var retiring []*raLoopState
	for ifname, loop := range s.loops {
		select {
		case <-loop.done:
			delete(s.loops, ifname)
			s.lifecycleMu.Lock()
			for i, old := range s.lifetimeLoops {
				if old == loop {
					s.lifetimeLoops = append(s.lifetimeLoops[:i], s.lifetimeLoops[i+1:]...)
					break
				}
			}
			s.lifecycleMu.Unlock()
		default:
			if _, exists := plans[ifname]; !exists {
				loop.cancel()
				retiring = append(retiring, loop)
			} else {
				select {
				case loop.wake <- struct{}{}:
				default:
				}
			}
		}
	}
	for ifname := range interfaces {
		if _, exists := s.loops[ifname]; exists {
			continue
		}
		endpoint, err := s.open(ifname)
		if err != nil {
			outcomes = append(outcomes, fmt.Errorf("open RA interface %s: %w", ifname, err))
			continue
		}
		loopCtx, cancel := context.WithCancel(s.ctx)
		loop := &raLoopState{cancel: cancel, wake: make(chan struct{}, 1), first: make(chan error, 1), done: make(chan struct{})}
		s.lifecycleMu.Lock()
		if err := s.ctx.Err(); err != nil {
			s.lifecycleMu.Unlock()
			cancel()
			endpoint.Conn.Close()
			s.mu.Unlock()
			return err
		}
		s.loops[ifname] = loop
		s.lifetimeLoops = append(s.lifetimeLoops, loop)
		s.lifecycleMu.Unlock()
		waits = append(waits, loop)
		name := ifname
		go func() {
			first := true
			engine := ra.Loop{Conn: endpoint.Conn, Source: endpoint.Source, InterfaceIndex: endpoint.Index, Wake: loop.wake,
				MaxInterval: func() time.Duration {
					s.mu.Lock()
					defer s.mu.Unlock()
					life := s.plans[name].RouterLifetime
					if life > 0 && life < 16 {
						return time.Duration(life) * time.Second
					}
					return 16 * time.Second
				},
				Transmit: func(_ time.Time, final bool) error {
					sendCtx := loopCtx
					var cancelFinal context.CancelFunc
					if final {
						sendCtx, cancelFinal = context.WithTimeout(context.WithoutCancel(loopCtx), 2*time.Second)
						defer cancelFinal()
					}
					err := s.transmit(sendCtx, name, endpoint, final)
					if first {
						loop.first <- err
						first = false
					}
					return err
				},
			}
			err := engine.Run(loopCtx)
			if first {
				loop.first <- err
			}
			loop.err = err
			close(loop.done)
		}()
		if _, wanted := plans[name]; !wanted {
			cancel()
		}
	}
	s.mu.Unlock()
	for _, loop := range retiring {
		select {
		case <-loop.done:
			outcomes = append(outcomes, loop.err)
		case <-ctx.Done():
			return errors.Join(append(outcomes, ctx.Err())...)
		}
	}
	for _, loop := range waits {
		select {
		case err := <-loop.first:
			outcomes = append(outcomes, err)
		case <-ctx.Done():
			return errors.Join(append(outcomes, ctx.Err())...)
		}
	}
	return errors.Join(outcomes...)
}

func (s *RouterAdvertisementService) advertisementParent(ctx context.Context, ifname string, active bool, recordedToken string) (vxlan.SegmentLink, error) {
	child := LANAddressController{Router: s.declared, DeclaredRouter: s.declared, Segments: s.segments}
	if !active && recordedToken != "" {
		owner := child.segmentParentOwner()
		return owner.ObserveRecordedInterface(ctx, ifname, recordedToken)
	}
	link, err := child.delegatedSegmentParent(ctx, ifname)
	if err != nil {
		return link, err
	}
	if !link.Exists && recordedToken != "" {
		return link, fmt.Errorf("RA declared native parent no longer acquired")
	}
	return link, nil
}

func (s *RouterAdvertisementService) transmit(ctx context.Context, ifname string, endpoint ra.Endpoint, final bool) (resultErr error) {
	s.mu.Lock()
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, s.publishRAFailure(ifname, resultErr))
		}
		s.mu.Unlock()
	}()
	journal, err := readRAJournal(s.store)
	if err != nil {
		return err
	}
	plan, active := s.plans[ifname]
	scope, err := s.scope(ctx)
	if err != nil {
		return err
	}
	prior := journal[ifname]
	if prior.IfIndex != endpoint.Index || prior.Scope != scope {
		delete(journal, ifname)
		if prior.Interface != "" {
			if err := saveRAJournal(s.store, journal); err != nil {
				return err
			}
		}
	}
	if prior.IfIndex != endpoint.Index || prior.Scope != scope {
		prior = raReceipt{}
	}
	parent, err := s.advertisementParent(ctx, ifname, active, prior.ParentToken)
	if err != nil {
		return err
	}
	if parent.Exists && (parent.Index != endpoint.Index || parent.Scope != scope) {
		return fmt.Errorf("RA native parent differs from socket interface")
	}
	if !active && prior.ParentToken != "" && !parent.Exists {
		delete(journal, ifname)
		return saveRAJournal(s.store, journal)
	}
	if prior.ParentToken != parent.Token {
		delete(journal, ifname)
		if err := saveRAJournal(s.store, journal); err != nil {
			return err
		}
		prior = raReceipt{}
	}
	plan.NativeIndex, plan.NativeScope, plan.NativeParentToken = endpoint.Index, scope, parent.Token
	if final && active && journal[ifname].Interface == "" {
		return nil
	}
	var packet *ndp.RouterAdvertisement
	var statuses []raResourceStatus
	if active && !final {
		packet, statuses, err = buildRAAdvertisement(ctx, s.declared, s.store, plan, s.clock())
		if err != nil {
			return err
		}
	} else {
		packet = &ndp.RouterAdvertisement{CurrentHopLimit: 64}
	}
	wanted := map[string]bool{}
	for _, option := range packet.Options {
		if prefix, ok := option.(*ndp.PrefixInformation); ok && prefix.ValidLifetime > 0 {
			wanted[netip.PrefixFrom(prefix.Prefix, int(prefix.PrefixLength)).String()] = true
		}
	}
	// Normal serve shutdown relinquishes the router role without deprecating
	// prefixes still valid on another router. Removed intent explicitly withdraws.
	if !active || !final {
		for _, prefix := range journal[ifname].Prefixes {
			if !wanted[prefix] {
				p, _ := netip.ParsePrefix(prefix)
				packet.Options = append(packet.Options, &ndp.PrefixInformation{Prefix: p.Addr(), PrefixLength: uint8(p.Bits()), OnLink: true, AutonomousAddressConfiguration: true})
			}
		}
	}
	if final {
		packet.RouterLifetime = 0
	}
	if len(packet.Options) == 0 && !active && prior.Interface == "" {
		return nil
	}
	previousReceipt := journal[ifname]
	wantedDNS, wantedDomains := raDNSValues(packet)
	beforeDNS := unionRAStrings(previousReceipt.DNSServers, wantedDNS)
	beforeDomains := unionRAStrings(previousReceipt.Domains, wantedDomains)
	if !active || !final {
		appendRADNSWithdrawals(packet, previousReceipt)
	}
	before := append([]string(nil), journal[ifname].Prefixes...)
	for prefix := range wanted {
		if !containsString(before, prefix) {
			before = append(before, prefix)
		}
	}
	sort.Strings(before)
	if len(before) > 0 || len(beforeDNS) > 0 || len(beforeDomains) > 0 || packet.RouterLifetime > 0 || previousReceipt.Router {
		journal[ifname] = raReceipt{Interface: ifname, IfIndex: endpoint.Index, Scope: scope, ParentToken: parent.Token, Prefixes: before, DNSServers: beforeDNS, Domains: beforeDomains, Router: packet.RouterLifetime > 0 || previousReceipt.Router}
	} else {
		delete(journal, ifname)
	}
	if err := saveRAJournal(s.store, journal); err != nil {
		return err
	}
	observedAt := s.clock()
	if active && !final {
		// Persistence can block. Re-observe after it, then subtract time spent
		// reading sources before the actual write. A source switch or expiry
		// cannot revive the packet prepared before that wait.
		observedAt = s.clock()
		fresh, nextStatuses, err := buildRAAdvertisement(ctx, s.declared, s.store, plan, observedAt)
		if err != nil {
			return err
		}
		freshWanted := raAdvertisedPrefixes(fresh)
		if len(freshWanted) != len(wanted) {
			return fmt.Errorf("RA prefix authority changed while recording pending advertisement")
		}
		for prefix := range wanted {
			if !freshWanted[prefix] {
				return fmt.Errorf("RA prefix authority changed while recording pending advertisement")
			}
		}
		packet, statuses = fresh, nextStatuses
		appendRADNSWithdrawals(packet, previousReceipt)
		wantedDNS, wantedDomains = raDNSValues(packet)
	}

	freshParent, err := s.advertisementParent(ctx, ifname, active, parent.Token)
	if err != nil {
		return err
	}
	if freshParent.Exists != parent.Exists || freshParent.Token != parent.Token || freshParent.Index != parent.Index || freshParent.Scope != parent.Scope {
		return fmt.Errorf("RA native parent changed while recording intent")
	}
	if active && !final {
		hadPositivePrefix := len(raAdvertisedPrefixes(packet)) > 0
		clampRAPacketLifetimes(packet, s.clock().Sub(observedAt))
		wantedDNS, wantedDomains = raDNSValues(packet)
		wanted = raAdvertisedPrefixes(packet)
		for _, prefix := range before {
			if !wanted[prefix] {
				p, _ := netip.ParsePrefix(prefix)
				packet.Options = append(packet.Options, &ndp.PrefixInformation{Prefix: p.Addr(), PrefixLength: uint8(p.Bits()), OnLink: true, AutonomousAddressConfiguration: true})
			}
		}
		if hadPositivePrefix && len(wanted) == 0 {
			packet.RouterLifetime = 0
		}
		for i := range statuses {
			var live []string
			for _, prefix := range statuses[i].Prefixes {
				if wanted[prefix] {
					live = append(live, prefix)
				}
			}
			statuses[i].Prefixes = live
			if len(live) == 0 && statuses[i].Pending == "" {
				statuses[i].Pending = "PrefixNotReady"
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := endpoint.Write(packet); err != nil {
		return err
	}
	if !final {
		if err := saveRAStatuses(s.store, statuses, false); err != nil {
			return err
		}
	}
	if s.resultsDirty[ifname] {
		stale, err := unconfiguredRAResults(s.store, s.declared)
		if err != nil {
			return err
		}
		deleter, ok := s.store.(state.ObjectDeleteStore)
		if !ok {
			return fmt.Errorf("RA result deletion unavailable")
		}
		for _, object := range stale {
			if statusvalue.Field(object.Status, "ifname") == ifname {
				if err := deleter.DeleteObject(object.APIVersion, object.Kind, object.Name); err != nil {
					return err
				}
			}
		}
		delete(s.resultsDirty, ifname)
	}
	if !final || !active {
		var next []string
		for prefix := range wanted {
			next = append(next, prefix)
		}
		sort.Strings(next)
		if len(next) == 0 && len(wantedDNS) == 0 && len(wantedDomains) == 0 && packet.RouterLifetime == 0 {
			delete(journal, ifname)
		} else {
			journal[ifname] = raReceipt{Interface: ifname, IfIndex: endpoint.Index, Scope: scope, ParentToken: parent.Token, Prefixes: next, DNSServers: wantedDNS, Domains: wantedDomains, Router: packet.RouterLifetime > 0}
		}
		if err := saveRAJournal(s.store, journal); err != nil {
			return err
		}
	}
	return nil
}

func raAdvertisedPrefixes(packet *ndp.RouterAdvertisement) map[string]bool {
	prefixes := map[string]bool{}
	for _, option := range packet.Options {
		if prefix, ok := option.(*ndp.PrefixInformation); ok && prefix.ValidLifetime > 0 {
			prefixes[netip.PrefixFrom(prefix.Prefix, int(prefix.PrefixLength)).String()] = true
		}
	}
	return prefixes
}

func clampRAPacketLifetimes(packet *ndp.RouterAdvertisement, elapsed time.Duration) {
	if elapsed <= 0 {
		return
	}
	seconds := (elapsed + time.Second - 1) / time.Second
	loss := seconds * time.Second
	clamp := func(value time.Duration) time.Duration {
		if value == time.Duration(infinitePDLifetime)*time.Second {
			return value
		}
		if value <= loss {
			return 0
		}
		return value - loss
	}
	for _, option := range packet.Options {
		switch value := option.(type) {
		case *ndp.PrefixInformation:
			value.PreferredLifetime = clamp(value.PreferredLifetime)
			value.ValidLifetime = clamp(value.ValidLifetime)
		case *ndp.RecursiveDNSServer:
			value.Lifetime = clamp(value.Lifetime)
		case *ndp.DNSSearchList:
			value.Lifetime = clamp(value.Lifetime)
		}
	}
}

type raReceipt struct {
	Interface   string   `json:"interface"`
	IfIndex     int      `json:"ifIndex"`
	Scope       string   `json:"scope"`
	ParentToken string   `json:"parentToken"`
	Prefixes    []string `json:"prefixes"`
	Router      bool     `json:"router"`
	DNSServers  []string `json:"dnsServers,omitempty"`
	Domains     []string `json:"domains,omitempty"`
}

func readRAJournal(store Store) (map[string]raReceipt, error) {
	status, err := store.ReadObjectStatus(api.RouterAPIVersion, "Router", raJournalName)
	if err != nil {
		return nil, err
	}
	journal := map[string]raReceipt{}
	if status == nil {
		return journal, nil
	}
	var receipts []raReceipt
	switch raw := status["interfaces"].(type) {
	case []raReceipt:
		receipts = raw
	case []any:
		for _, value := range raw {
			entry, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid RA receipt")
			}
			name, ok := entry["interface"].(string)
			if !ok {
				return nil, fmt.Errorf("invalid RA receipt interface")
			}
			values, ok := entry["prefixes"].([]any)
			if !ok {
				return nil, fmt.Errorf("invalid RA receipt prefixes")
			}
			index, ok := entry["ifIndex"].(float64)
			if !ok || index < 1 || index > 2147483647 || index != float64(int(index)) {
				return nil, fmt.Errorf("invalid RA receipt index")
			}
			scope, ok := entry["scope"].(string)
			if !ok || scope == "" {
				return nil, fmt.Errorf("invalid RA receipt scope")
			}
			parentToken, ok := entry["parentToken"].(string)
			if !ok {
				return nil, fmt.Errorf("invalid RA parent receipt")
			}
			if len(entry) < 6 || len(entry) > 8 {
				return nil, fmt.Errorf("invalid RA receipt shape")
			}
			router, ok := entry["router"].(bool)
			if !ok {
				return nil, fmt.Errorf("invalid RA router receipt")
			}
			r := raReceipt{Interface: name, IfIndex: int(index), Scope: scope, ParentToken: parentToken, Router: router}
			for _, value := range values {
				prefix, ok := value.(string)
				if !ok {
					return nil, fmt.Errorf("invalid RA receipt prefix")
				}
				r.Prefixes = append(r.Prefixes, prefix)
			}
			for _, field := range []string{"dnsServers", "domains"} {
				if raw, exists := entry[field]; exists {
					items, ok := raw.([]any)
					if !ok {
						return nil, fmt.Errorf("invalid RA receipt %s", field)
					}
					for _, item := range items {
						value, ok := item.(string)
						if !ok || value == "" {
							return nil, fmt.Errorf("invalid RA receipt %s", field)
						}
						if field == "dnsServers" {
							r.DNSServers = append(r.DNSServers, value)
						} else {
							r.Domains = append(r.Domains, value)
						}
					}
				}
			}
			receipts = append(receipts, r)
		}
	default:
		return nil, fmt.Errorf("invalid RA withdrawal journal")
	}
	for _, receipt := range receipts {
		if receipt.IfIndex < 1 || receipt.Scope == "" || strings.TrimSpace(receipt.Interface) == "" || strings.ContainsAny(receipt.Interface, "/\x00\n\r") {
			return nil, fmt.Errorf("invalid RA receipt interface")
		}
		if _, duplicate := journal[receipt.Interface]; duplicate {
			return nil, fmt.Errorf("duplicate RA receipt interface")
		}
		for _, value := range receipt.Prefixes {
			prefix, err := netip.ParsePrefix(value)
			if err != nil || !prefix.Addr().Is6() || prefix != prefix.Masked() {
				return nil, fmt.Errorf("invalid RA receipt prefix %q", value)
			}
		}
		for _, server := range receipt.DNSServers {
			address, err := netip.ParseAddr(server)
			if err != nil || !address.Is6() || address.Is4In6() || address.IsMulticast() || address.IsUnspecified() {
				return nil, fmt.Errorf("invalid RA DNS receipt")
			}
		}
		journal[receipt.Interface] = receipt
	}
	return journal, nil
}

func saveRAJournal(store Store, journal map[string]raReceipt) error {
	names := make([]string, 0, len(journal))
	for name := range journal {
		names = append(names, name)
	}
	sort.Strings(names)
	receipts := make([]raReceipt, 0, len(names))
	for _, name := range names {
		receipt := journal[name]
		if receipt.Prefixes == nil {
			receipt.Prefixes = []string{}
		}
		receipts = append(receipts, receipt)
	}
	return store.SaveObjectStatus(api.RouterAPIVersion, "Router", raJournalName, map[string]any{"interfaces": receipts})
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

type raResourceStatus struct {
	IfName    string
	Kind      string
	Name      string
	Interface string
	Prefixes  []string
	Pending   string
	RDNSS     []string
}

func saveRAStatuses(store Store, statuses []raResourceStatus, dryRun bool) error {
	for _, status := range statuses {
		phase := "Applied"
		if dryRun {
			phase = "Rendered"
		}
		if status.Pending != "" {
			phase = "Pending"
		}
		prefix := ""
		if len(status.Prefixes) > 0 {
			prefix = status.Prefixes[0]
		}
		kind := firstNonEmpty(status.Kind, "IPv6RouterAdvertisement")
		previous, err := store.ReadObjectStatus(api.NetAPIVersion, kind, status.Name)
		if err != nil {
			return err
		}
		next := map[string]any{"phase": phase, "reason": status.Pending, "interface": status.Interface, "prefix": prefix, "prefixes": status.Prefixes, "rdnss": status.RDNSS, "renderer": "routerd", "dryRun": dryRun, "ifname": status.IfName}
		if kind == "DHCPv6Server" {
			next["mode"] = "ra-only"
		}
		next = preserveStatusFields(next, previous, "managedBy", "unitName")
		if err := store.SaveObjectStatus(api.NetAPIVersion, kind, status.Name, next); err != nil {
			return err
		}
	}
	return nil
}

func (c DHCPv6ServerController) reconcileAdvertisements(ctx context.Context, effective *api.Router) error {
	if c.Advertisements != nil {
		return c.Advertisements.Reconcile(ctx, c.Router, effective, c.Store)
	}
	needed := false
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind == "IPv6RouterAdvertisement" || resource.Kind == "DHCPv6Server" {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	if !c.DryRun {
		return fmt.Errorf("serve-owned RA service unavailable")
	}
	service := NewRouterAdvertisementService(ctx, true)
	defer service.cancel()
	return service.Reconcile(ctx, c.Router, effective, c.Store)
}

func saveSuppressedRAStatuses(declared, effective *api.Router, store Store, dryRun bool) error {
	selected := map[string]bool{}
	if effective != nil {
		for _, resource := range effective.Spec.Resources {
			selected[resource.ID()] = true
		}
	}
	if declared == nil {
		return nil
	}
	for _, resource := range declared.Spec.Resources {
		if resource.Kind != "IPv6RouterAdvertisement" {
			continue
		}
		spec, err := resource.IPv6RouterAdvertisementSpec()
		if err != nil {
			return err
		}
		reason := ""
		if !selected[resource.ID()] {
			reason = "WhenFalse"
		} else {
			ready, err := resourcequery.ReadDependenciesReady(store, spec.DependsOn)
			if err != nil {
				return err
			}
			if !ready {
				reason = "DependsOnFalse"
			}
		}
		if reason != "" {
			if err := saveRAStatuses(store, []raResourceStatus{{Name: resource.Metadata.Name, Interface: spec.Interface, Pending: reason}}, dryRun); err != nil {
				return err
			}
		}
	}
	return nil
}

func raDNSValues(packet *ndp.RouterAdvertisement) (servers, domains []string) {
	for _, option := range packet.Options {
		switch value := option.(type) {
		case *ndp.RecursiveDNSServer:
			if value.Lifetime > 0 {
				for _, address := range value.Servers {
					servers = append(servers, address.String())
				}
			}
		case *ndp.DNSSearchList:
			if value.Lifetime > 0 {
				domains = append(domains, value.DomainNames...)
			}
		}
	}
	return unionRAStrings(nil, servers), unionRAStrings(nil, domains)
}
func unionRAStrings(old, next []string) []string {
	values := map[string]bool{}
	for _, value := range old {
		values[value] = true
	}
	for _, value := range next {
		values[value] = true
	}
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
func appendRADNSWithdrawals(packet *ndp.RouterAdvertisement, prior raReceipt) {
	servers, domains := raDNSValues(packet)
	var removedServers []netip.Addr
	for _, server := range prior.DNSServers {
		if !containsString(servers, server) {
			removedServers = append(removedServers, netip.MustParseAddr(server))
		}
	}
	if len(removedServers) > 0 {
		packet.Options = append(packet.Options, &ndp.RecursiveDNSServer{Servers: removedServers})
	}
	var removedDomains []string
	for _, domain := range prior.Domains {
		if !containsString(domains, domain) {
			removedDomains = append(removedDomains, domain)
		}
	}
	if len(removedDomains) > 0 {
		packet.Options = append(packet.Options, &ndp.DNSSearchList{DomainNames: removedDomains})
	}
}

func (c DHCPv6ServerController) saveSuppressedRAOnlyServers(effective *api.Router) error {
	selected := map[string]bool{}
	for _, resource := range effective.Spec.Resources {
		selected[resource.ID()] = true
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DHCPv6Server" || selected[resource.ID()] {
			continue
		}
		spec, err := resource.DHCPv6ServerSpec()
		if err != nil {
			return err
		}
		if spec.Mode != "ra-only" {
			continue
		}
		matches, err := c.resourceWhenMatches(resource)
		if err != nil {
			return err
		}
		reason := "DependsOnFalse"
		if !matches {
			reason = "WhenFalse"
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6Server", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": reason, "mode": "ra-only", "interface": spec.Interface, "renderer": "routerd", "dryRun": c.DryRun}); err != nil {
			return err
		}
	}
	return nil
}

func unconfiguredRAResults(store Store, declared *api.Router) ([]state.ObjectStatus, error) {
	lister, ok := store.(state.ObjectStatusLister)
	if !ok {
		return nil, fmt.Errorf("RA result enumeration unavailable")
	}
	objects, err := lister.ListObjectStatuses()
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	if declared != nil {
		for _, resource := range declared.Spec.Resources {
			known[resource.Kind+"/"+resource.Metadata.Name] = true
		}
	}
	var stale []state.ObjectStatus
	for _, object := range objects {
		if object.APIVersion != api.NetAPIVersion || known[object.Kind+"/"+object.Name] || object.Status["renderer"] != "routerd" || statusvalue.Field(object.Status, "ifname") == "" {
			continue
		}
		if object.Kind == "IPv6RouterAdvertisement" || object.Kind == "DHCPv6Server" && object.Status["mode"] == "ra-only" {
			stale = append(stale, object)
		}
	}
	return stale, nil
}
func (s *RouterAdvertisementService) publishRAFailure(ifname string, cause error) error {
	seen := map[string]bool{}
	plans := []raInterfacePlan{s.plans[ifname]}
	plans = append(plans, s.prepared...)
	var failures []error
	for _, plan := range plans {
		if plan.Interface != ifname {
			continue
		}
		for _, intent := range plan.Resources {
			if intent.Name == "" || seen[intent.StatusKind+"/"+intent.Name] {
				continue
			}
			seen[intent.StatusKind+"/"+intent.Name] = true
			kind := firstNonEmpty(intent.StatusKind, "IPv6RouterAdvertisement")
			current, err := s.store.ReadObjectStatus(api.NetAPIVersion, kind, intent.Name)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			next := copyStatusMap(current)
			next["phase"], next["reason"], next["message"], next["renderer"], next["ifname"] = "Error", "AdvertisementFailed", cause.Error(), "routerd", ifname
			if err := s.store.SaveObjectStatus(api.NetAPIVersion, kind, intent.Name, next); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}
