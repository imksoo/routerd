// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controller/framework"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/vxlan"
)

// VXLANSegmentController owns the native link, flood entries and its own bridge
// port. Receipts survive removal of the declaring resource and public statuses.
type VXLANSegmentController struct {
	Router   *api.Router
	Store    Store
	Native   vxlan.SegmentNative
	StateDir string
	DryRun   bool
	// scope is captured once by this operation, never an independent authority.
	scope string
	// Admit rechecks the actual current selection before forwarding activation.
	// The runtime supplies its read-only effective-view reader; standalone users
	// must explicitly own an immutable or otherwise current configuration source.
	Admit func(context.Context, string, vxlan.SegmentConfig) (bool, error)
}

var errSegmentIntentChanged = errors.New("VXLAN segment intent changed")

type vxlanSegmentOwner struct {
	Version     int                   `json:"version"`
	Resource    string                `json:"resource"`
	Withdrawing bool                  `json:"withdrawing"`
	Identity    vxlan.SegmentIdentity `json:"identity"`
	Plan        vxlan.SegmentConfig   `json:"plan"`
}

func (c VXLANSegmentController) receiptDir() string {
	dir := c.StateDir
	if dir == "" {
		defaults, _ := platform.Current()
		dir = defaults.StateDir
	}
	return filepath.Join(dir, "vxlan-native", fmt.Sprintf("%x", sha256.Sum256([]byte(c.scope))))
}

func (c VXLANSegmentController) receiptPath(resource string) string {
	return filepath.Join(c.receiptDir(), fmt.Sprintf("segment-%x.json", sha256.Sum256([]byte(resource))))
}

func (c VXLANSegmentController) readOwners() ([]vxlanSegmentOwner, error) {
	entries, err := os.ReadDir(c.receiptDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var owners []vxlanSegmentOwner
	seen := map[string]bool{}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "segment-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if entry.IsDir() {
			return nil, fmt.Errorf("VXLAN receipt %s is a directory", entry.Name())
		}
		data, err := os.ReadFile(filepath.Join(c.receiptDir(), entry.Name()))
		if err != nil {
			return nil, err
		}
		var owner vxlanSegmentOwner
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&owner); err != nil {
			return nil, fmt.Errorf("decode VXLAN receipt %s: %w", entry.Name(), err)
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			return nil, fmt.Errorf("trailing data in VXLAN receipt %s", entry.Name())
		}
		token, err := hex.DecodeString(owner.Identity.Token)
		if err != nil || len(token) != 16 || owner.Version != 1 || owner.Resource == "" || filepath.Base(c.receiptPath(owner.Resource)) != entry.Name() || owner.Identity.Scope != c.scope || owner.Identity.CreationName == "" || owner.Identity.Index < 0 || !owner.Identity.Pending && owner.Identity.Index == 0 || owner.Identity.UnderlayIndex <= 0 || owner.Identity.BridgeIndex < 0 {
			return nil, fmt.Errorf("invalid native VXLAN receipt %s", entry.Name())
		}
		if err := validateSegmentPlan(owner.Plan); err != nil {
			return nil, fmt.Errorf("invalid VXLAN receipt plan %s: %w", entry.Name(), err)
		}
		if seen[owner.Plan.IfName] {
			return nil, fmt.Errorf("multiple VXLAN receipts claim interface %s", owner.Plan.IfName)
		}
		seen[owner.Plan.IfName] = true
		owners = append(owners, owner)
	}
	return owners, nil
}

func (c VXLANSegmentController) saveOwner(owner vxlanSegmentOwner) error {
	data, err := json.Marshal(owner)
	if err != nil {
		return err
	}
	return writeAtomicArtifact(c.receiptPath(owner.Resource), append(data, '\n'), 0o600)
}

func (c VXLANSegmentController) removeOwner(resource string) error {
	if err := os.Remove(c.receiptPath(resource)); err != nil && !os.IsNotExist(err) {
		return err
	}
	dir, err := os.Open(c.receiptDir())
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (c VXLANSegmentController) Reconcile(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.Router == nil || c.Store == nil {
		return errors.New("VXLAN router and status publisher are required")
	}
	plans, err := segmentPlans(c.Router)
	if err != nil {
		return err
	}
	if c.DryRun {
		for name, plan := range plans {
			if err := c.saveStatus(name, plan, "Planned", nil); err != nil {
				return err
			}
		}
		return nil
	}
	if c.Admit == nil {
		return errors.New("VXLAN current intent reader is required")
	}
	if err := c.captureNativeScope(ctx); err != nil {
		return err
	}
	if err := makeVXLANReceiptDir(c.receiptDir()); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(c.receiptDir(), ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("another VXLAN native operation owns the receipt lock: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	owners, err := c.readOwners()
	if err != nil {
		return err
	}
	current := map[string]vxlanSegmentOwner{}
	for _, owner := range owners {
		plan, desired := plans[owner.Resource]
		if !desired || owner.Withdrawing || !sameSegmentPlan(owner.Plan, plan) {
			if err := c.withdraw(ctx, owner); err != nil {
				return err
			}
			continue
		}
		current[owner.Resource] = owner
	}
	names := make([]string, 0, len(plans))
	for name := range plans {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if err := c.reconcileSegment(ctx, name, plans[name], current[name]); err != nil {
			return err
		}
	}
	return nil
}

func (c VXLANSegmentController) reconcileSegment(ctx context.Context, name string, plan vxlan.SegmentConfig, owner vxlanSegmentOwner) error {
	var observed vxlan.SegmentLink
	var err error
	if owner.Resource != "" {
		observed, err = c.Native.ObserveOwned(ctx, plan, owner.Identity)
	} else {
		observed, err = c.Native.Observe(ctx, plan.IfName)
	}
	if err != nil {
		return err
	}
	if owner.Resource != "" {
		topology, err := c.Native.Resolve(ctx, plan, owner.Identity.Token)
		if err != nil {
			return c.saveError(name, plan, err)
		}
		if owner.Identity.Scope != topology.Scope || owner.Identity.UnderlayIndex != topology.UnderlayIndex || owner.Identity.BridgeIndex != topology.BridgeIndex || !observed.Exists && owner.Identity.Index > 0 {
			if err := c.withdraw(ctx, owner); err != nil {
				return err
			}
			owner = vxlanSegmentOwner{}
			observed, err = c.Native.Observe(ctx, plan.IfName)
			if err != nil {
				return err
			}
		}
	}
	if owner.Resource == "" {
		if observed.Exists {
			return c.saveError(name, plan, fmt.Errorf("VXLAN interface %s is unowned", plan.IfName))
		}
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return err
		}
		topology, err := c.Native.Resolve(ctx, plan, hex.EncodeToString(token[:]))
		if err != nil {
			return c.saveError(name, plan, err)
		}
		if topology.Token != hex.EncodeToString(token[:]) || topology.Scope != c.scope || topology.CreationName == "" || topology.UnderlayIndex <= 0 || topology.BridgeIndex < 0 {
			return errors.New("VXLAN native preparation lacks ownership identity")
		}
		topology.Pending = true
		owner = vxlanSegmentOwner{Version: 1, Resource: name, Identity: topology, Plan: plan}
		if err := c.saveOwner(owner); err != nil {
			return err
		}
	}
	if !observed.Exists {
		if err := c.confirm(ctx, name, plan); err != nil {
			return c.failedAdmission(ctx, owner, err)
		}
		createErr := c.Native.Create(ctx, plan, owner.Identity)
		observed, err = c.Native.ObserveOwned(ctx, plan, owner.Identity)
		if err != nil {
			return c.saveError(name, plan, errors.Join(createErr, err))
		}
		if err := vxlan.CheckSegmentIdentity(plan, owner.Identity, observed); err != nil {
			return c.saveError(name, plan, errors.Join(createErr, err))
		}
		owner.Identity.Index = observed.Index
		if err := c.saveOwner(owner); err != nil {
			return err
		}
		if createErr != nil {
			return c.saveError(name, plan, createErr)
		}
	} else if err := vxlan.CheckSegmentIdentity(plan, owner.Identity, observed); err != nil {
		return c.saveError(name, plan, err)
	}
	if owner.Identity.Index == 0 {
		owner.Identity.Index = observed.Index
		if err := c.saveOwner(owner); err != nil {
			return err
		}
	}
	if err := vxlan.CheckSegmentReadback(plan, owner.Identity, observed); err != nil {
		// An applied receipt already owns this complete input. Repair retains
		// its strict immutable-tuple guard; it must not become a new acquisition.
		if err := c.Native.Configure(ctx, plan, owner.Identity, func(ctx context.Context) error { return c.confirm(ctx, name, plan) }); err != nil {
			return c.failedAdmission(ctx, owner, err)
		}
		observed, err = c.Native.ObserveOwned(ctx, plan, owner.Identity)
		if err != nil {
			return c.saveError(name, plan, err)
		}
		if err := vxlan.CheckSegmentReadback(plan, owner.Identity, observed); err != nil {
			return c.saveError(name, plan, err)
		}
	}
	if err := c.confirm(ctx, name, plan); err != nil {
		return c.failedAdmission(ctx, owner, err)
	}
	if owner.Identity.Pending {
		owner.Identity.Pending = false
		if err := c.saveOwner(owner); err != nil {
			return err
		}
	}
	return c.saveStatus(name, plan, "Healthy", nil)
}

func (c VXLANSegmentController) confirm(ctx context.Context, name string, plan vxlan.SegmentConfig) error {
	matches, err := c.Admit(ctx, name, plan)
	if err != nil {
		return err
	}
	if !matches {
		return errSegmentIntentChanged
	}
	return nil
}

func (c VXLANSegmentController) failedAdmission(ctx context.Context, owner vxlanSegmentOwner, err error) error {
	if errors.Is(err, errSegmentIntentChanged) {
		return c.withdraw(ctx, owner)
	}
	return c.saveError(owner.Resource, owner.Plan, err)
}

func (c VXLANSegmentController) withdraw(ctx context.Context, owner vxlanSegmentOwner) error {
	owner.Withdrawing = true
	if err := c.saveOwner(owner); err != nil {
		return err
	}
	observed, err := c.Native.ObserveOwned(ctx, owner.Plan, owner.Identity)
	if err != nil {
		return err
	}
	if observed.Scope != owner.Identity.Scope {
		return c.saveError(owner.Resource, owner.Plan, errors.New("VXLAN ownership belongs to another kernel network scope"))
	}
	if observed.Exists {
		if err := vxlan.CheckSegmentIdentity(owner.Plan, owner.Identity, observed); err != nil {
			return c.saveError(owner.Resource, owner.Plan, err)
		}
		if owner.Identity.Index == 0 {
			owner.Identity.Index = observed.Index
			if err := c.saveOwner(owner); err != nil {
				return err
			}
		}
		if err := c.Native.Remove(ctx, owner.Plan, owner.Identity); err != nil {
			return c.saveError(owner.Resource, owner.Plan, err)
		}
		observed, err = c.Native.ObserveOwned(ctx, owner.Plan, owner.Identity)
		if err != nil || observed.Exists {
			return c.saveError(owner.Resource, owner.Plan, errors.Join(err, fmt.Errorf("VXLAN %s was not confirmed absent after withdrawal", owner.Plan.IfName)))
		}
	}
	if err := c.removeOwner(owner.Resource); err != nil {
		return err
	}
	return c.saveStatus(owner.Resource, owner.Plan, "Removed", nil)
}

func (c VXLANSegmentController) saveStatus(name string, plan vxlan.SegmentConfig, phase string, failure error) error {
	status := map[string]any{"phase": phase, "ifname": plan.IfName, "vni": plan.VNI, "localAddress": plan.Local, "remotes": append([]string(nil), plan.Remotes...), "multicastGroup": plan.Group, "underlayIfname": plan.Underlay, "bridgeIfname": plan.Bridge, "managedBy": "routerd"}
	if failure != nil {
		status["error"] = failure.Error()
	}
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "VXLANSegment", name, status)
}

func (c VXLANSegmentController) saveError(name string, plan vxlan.SegmentConfig, err error) error {
	return errors.Join(err, c.saveStatus(name, plan, "Error", err))
}

func sameSegmentPlan(a, b vxlan.SegmentConfig) bool {
	return a.IfName == b.IfName && a.VNI == b.VNI && a.Local == b.Local && slices.Equal(a.Remotes, b.Remotes) && a.Group == b.Group && a.Underlay == b.Underlay && a.Port == b.Port && a.MTU == b.MTU && a.Bridge == b.Bridge
}

func segmentPlans(router *api.Router) (map[string]vxlan.SegmentConfig, error) {
	plans := map[string]vxlan.SegmentConfig{}
	aliases := bridgeInterfaceAliases(router)
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "VXLANSegment" {
			continue
		}
		spec, err := resource.VXLANSegmentSpec()
		if err != nil {
			return nil, err
		}
		plan := vxlan.SegmentConfig{IfName: firstNonEmpty(spec.IfName, resource.Metadata.Name), VNI: spec.VNI, Local: spec.LocalAddress, Remotes: append([]string(nil), spec.Remotes...), Group: spec.MulticastGroup, Underlay: aliases[spec.UnderlayInterface], Port: chainFirstNonZero(spec.UDPPort, 4789), MTU: spec.MTU, Bridge: aliases[spec.Bridge]}
		// This is an already admitted effective graph. When filtering can
		// remove a declared dependency; that is known ineligibility, whereas
		// an unreadable condition would have failed observation before here.
		if plan.Underlay == "" || spec.Bridge != "" && plan.Bridge == "" {
			continue
		}
		for _, bridge := range router.Spec.Resources {
			if bridge.Kind != "Bridge" {
				continue
			}
			bridgeSpec, err := bridge.BridgeSpec()
			if err != nil {
				return nil, err
			}
			if slices.Contains(bridgeSpec.Members, resource.Metadata.Name) {
				ifname := firstNonEmpty(bridgeSpec.IfName, bridge.Metadata.Name)
				if plan.Bridge != "" && plan.Bridge != ifname {
					return nil, fmt.Errorf("%s has conflicting bridge attachments", resource.ID())
				}
				plan.Bridge = ifname
			}
		}
		if err := validateSegmentPlan(plan); err != nil {
			return nil, fmt.Errorf("%s: %w", resource.ID(), err)
		}
		local, _ := netip.ParseAddr(plan.Local)
		plan.Local = local.WithZone("").Unmap().String()
		for i, remote := range plan.Remotes {
			address, _ := netip.ParseAddr(remote)
			plan.Remotes[i] = address.WithZone("").Unmap().String()
		}
		if plan.Group != "" {
			group, _ := netip.ParseAddr(plan.Group)
			plan.Group = group.Unmap().String()
		}
		slices.Sort(plan.Remotes)
		plan.Remotes = slices.Compact(plan.Remotes)
		plans[resource.Metadata.Name] = plan
	}
	return plans, nil
}

func validateSegmentPlan(plan vxlan.SegmentConfig) error {
	if plan.IfName == "" || plan.Underlay == "" || plan.VNI < 1 || plan.VNI > 16777215 || plan.Port < 1 || plan.Port > 65535 {
		return errors.New("VXLAN interface, underlay, VNI and UDP port are required")
	}
	if _, err := netip.ParseAddr(plan.Local); err != nil {
		return err
	}
	if (len(plan.Remotes) == 0) == (plan.Group == "") {
		return errors.New("VXLAN requires a unicast flood set or multicast group")
	}
	for _, remote := range plan.Remotes {
		if _, err := netip.ParseAddr(remote); err != nil {
			return err
		}
	}
	if plan.Group != "" {
		group, err := netip.ParseAddr(plan.Group)
		if err != nil || !group.IsMulticast() {
			return errors.New("VXLAN group must be multicast")
		}
	}
	return nil
}

// vxlanSegmentController binds a single captured observation to each operation.
// Admission is repeated at the native activation boundary after any slow work.
func (r *Runner) vxlanSegmentController(store eventedStore, actor VXLANSegmentController) framework.Controller {
	return framework.FuncController{ControllerName: "vxlan-segment", Every: 30 * time.Second, Subs: withDynamicConfigPartSubscriptions(statusSubscriptions("Interface", "Bridge", "VXLANSegment")), NextAfter: func() time.Duration {
		view, err := r.ObserveRuntimeRouter(time.Now().UTC(), platform.CurrentOS())
		if err != nil || view.NextChangeAt.IsZero() {
			return 0
		}
		return max(time.Until(view.NextChangeAt), time.Nanosecond)
	}, PeriodicFunc: func(ctx context.Context) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		view, err := r.ObserveRuntimeRouter(time.Now().UTC(), platform.CurrentOS())
		if err != nil {
			return false, err
		}
		current := actor
		current.Router = view.EffectiveRouter
		current.Store = store.withRouter(view.EffectiveRouter)
		current.Admit = func(ctx context.Context, name string, plan vxlan.SegmentConfig) (bool, error) {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			latest, err := r.ObserveRuntimeRouter(time.Now().UTC(), platform.CurrentOS())
			if err != nil {
				return false, err
			}
			plans, err := segmentPlans(latest.EffectiveRouter)
			if err != nil {
				return false, err
			}
			selected, ok := plans[name]
			return ok && sameSegmentPlan(selected, plan), ctx.Err()
		}
		return didWorkError(current.Reconcile(ctx))
	}}
}

func (c *VXLANSegmentController) captureNativeScope(ctx context.Context) error {
	var err error
	if c.Native == nil {
		c.Native, err = vxlan.NewSegmentNative(platform.CurrentOS(), platform.KernelNetworkScope)
		if err != nil {
			return err
		}
	}
	c.scope, err = c.Native.KernelScope(ctx)
	if err != nil || c.scope == "" {
		return errors.Join(err, errors.New("VXLAN native operation requires its kernel network scope"))
	}
	return ctx.Err()
}

// ObserveOwnedInterface admits child effects only on an applied native parent.
// It shares the actor's actual journal and kernel-cookie boundary and publishes
// no status or new intent. Presence without this exact proof is an error.
func (c VXLANSegmentController) ObserveOwnedInterface(ctx context.Context, resource string) (vxlan.SegmentLink, error) {
	if err := c.captureNativeScope(ctx); err != nil {
		return vxlan.SegmentLink{}, err
	}
	absent := vxlan.SegmentLink{Scope: c.scope}
	if c.Router == nil {
		return absent, errors.New("VXLAN current router is required")
	}
	plans, err := segmentPlans(c.Router)
	if err != nil {
		return absent, err
	}
	plan, selected := plans[resource]
	if !selected {
		return absent, nil
	}
	owners, err := c.readOwners()
	if err != nil {
		return absent, err
	}
	for _, owner := range owners {
		if owner.Resource != resource {
			continue
		}
		if owner.Identity.Pending || owner.Withdrawing || !sameSegmentPlan(owner.Plan, plan) {
			return absent, errors.New("VXLAN parent acquisition or withdrawal is not complete")
		}
		observed, err := c.Native.ObserveOwned(ctx, plan, owner.Identity)
		if err != nil {
			return absent, err
		}
		if observed.Scope != c.scope {
			return absent, errors.New("VXLAN parent kernel scope changed")
		}
		if !observed.Exists {
			return absent, nil
		}
		if err := vxlan.CheckSegmentReadback(plan, owner.Identity, observed); err != nil {
			return absent, err
		}
		return observed, ctx.Err()
	}
	return absent, nil
}

// ObserveRecordedInterface verifies cleanup authority without requiring the old
// parent to remain declared. It does not admit activation or invent ownership
// from a same-name native link. Child receipts retain the exact parent token.
func (c VXLANSegmentController) ObserveRecordedInterface(ctx context.Context, ifname, token string) (vxlan.SegmentLink, error) {
	if err := c.captureNativeScope(ctx); err != nil {
		return vxlan.SegmentLink{}, err
	}
	absent := vxlan.SegmentLink{Scope: c.scope}
	if ifname == "" || token == "" {
		return absent, errors.New("recorded VXLAN parent identity is required")
	}
	owners, err := c.readOwners()
	if err != nil {
		return absent, err
	}
	for _, owner := range owners {
		if owner.Plan.IfName != ifname || owner.Identity.Token != token {
			continue
		}
		if owner.Identity.Pending || owner.Identity.Index <= 0 {
			return absent, errors.New("recorded VXLAN parent acquisition is incomplete")
		}
		link, err := c.Native.ObserveOwned(ctx, owner.Plan, owner.Identity)
		if err != nil {
			return absent, err
		}
		if link.Scope != c.scope {
			return absent, errors.New("recorded VXLAN parent kernel scope changed")
		}
		if !link.Exists {
			return absent, nil
		}
		if err := vxlan.CheckSegmentIdentity(owner.Plan, owner.Identity, link); err != nil {
			return absent, err
		}
		return link, ctx.Err()
	}
	link, err := c.Native.Observe(ctx, ifname)
	if err != nil {
		return absent, err
	}
	if link.Scope != c.scope {
		return absent, errors.New("unrecorded VXLAN parent kernel scope changed")
	}
	if link.Exists {
		return absent, errors.New("native VXLAN parent exists without its recorded ownership")
	}
	return absent, ctx.Err()
}

// Every newly created path component must be durable before a pending receipt
// can authorize a native effect. Syncing only the leaf file is insufficient.
func makeVXLANReceiptDir(path string) error {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("VXLAN receipt path %s is not a directory", path)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(path)
	if err := makeVXLANReceiptDir(parent); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	dir, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
