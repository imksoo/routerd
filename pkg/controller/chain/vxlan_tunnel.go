// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"github.com/imksoo/routerd/pkg/vxlan"
)

type VXLANTunnelController struct {
	// Router is the effective router for this reconciliation generation.
	Router *api.Router
	// DeclaredRouter retains resources filtered from Router by spec.when. It is
	// required to perform fail-closed teardown on BACKUP and unknown HA roles.
	DeclaredRouter  *api.Router
	Store           Store
	DryRun          bool
	OperatingSystem platform.OS
	NetworkdDir     string
	StartedAt       time.Time
	Mu              *sync.Mutex
}

var errVXLANGateRevisionChanged = errors.New("VXLAN gate dependency revision changed")

type floodFDBEntry struct {
	Destination string
	Self        bool
	Permanent   bool
}

func (entry floodFDBEntry) exact() bool {
	return entry.Destination != "" && entry.Self && entry.Permanent
}

func (c VXLANTunnelController) Reconcile(ctx context.Context) error {
	if c.Mu != nil {
		c.Mu.Lock()
		defer c.Mu.Unlock()
	}
	if c.Store == nil {
		return nil
	}
	osName := c.OperatingSystem
	if osName == "" {
		osName = platform.CurrentOS()
	}
	if osName != platform.OSLinux {
		return nil
	}
	declared := c.DeclaredRouter
	if declared == nil {
		declared = c.Router
	}
	if declared == nil {
		return nil
	}
	if err := c.cleanupExtraVXLANs(ctx, declared); err != nil {
		return err
	}

	effective := vxlansByName(c.Router)
	aliases := bridgeInterfaceAliases(declared)
	seen := map[string]bool{}
	for _, resource := range declared.Spec.Resources {
		if resource.Kind != "VXLANTunnel" {
			continue
		}
		seen[resource.Metadata.Name] = true
		gate := c.gateState(resource)
		active, retained := effective[resource.Metadata.Name]
		if !retained || gate != vxlanGateEnabled {
			reason := gate
			if gate == vxlanGateEnabled {
				reason = vxlanGateSuppressed
			}
			if err := c.teardownOne(ctx, resource, reason); err != nil {
				return err
			}
			continue
		}
		if err := c.reconcileResource(ctx, active, aliases); err != nil {
			return err
		}
	}

	// Dynamic effective resources are not necessarily present in the startup
	// router. Reconcile them too, but retain the same unknown-is-disabled gate.
	for name, resource := range effective {
		if seen[name] {
			continue
		}
		if gate := c.gateState(resource); gate != vxlanGateEnabled {
			if err := c.teardownOne(ctx, resource, gate); err != nil {
				return err
			}
			continue
		}
		if err := c.reconcileResource(ctx, resource, bridgeInterfaceAliases(c.Router)); err != nil {
			return err
		}
	}
	return nil
}

func (c VXLANTunnelController) cleanupExtraVXLANs(ctx context.Context, declared *api.Router) error {
	retained := map[string]bool{}
	for _, router := range []*api.Router{declared, c.Router} {
		if router == nil {
			continue
		}
		for _, resource := range router.Spec.Resources {
			switch resource.Kind {
			case "VXLANTunnel":
				spec, err := resource.VXLANTunnelSpec()
				if err != nil {
					return err
				}
				retained[firstNonEmpty(spec.IfName, resource.Metadata.Name)] = true
			case "VXLANSegment":
				spec, err := resource.VXLANSegmentSpec()
				if err != nil {
					return err
				}
				retained[firstNonEmpty(spec.IfName, resource.Metadata.Name)] = true
			}
		}
	}
	out, err := c.command(ctx, "ip", "-j", "link", "show", "type", "vxlan")
	if err != nil {
		return fmt.Errorf("list VXLAN links: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var links []struct {
		IfName string `json:"ifname"`
	}
	if err := json.Unmarshal(out, &links); err != nil {
		return fmt.Errorf("decode VXLAN links: %w", err)
	}
	filesChanged := false
	for _, suffix := range []string{".owner", ".netdev", ".network"} {
		paths, err := filepath.Glob(c.networkdPath("/etc/systemd/network/31-routerd-*" + suffix))
		if err != nil {
			return err
		}
		for _, path := range paths {
			ifname := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "31-routerd-"), suffix)
			if suffix != ".owner" && retained[ifname] {
				continue
			}
			if c.DryRun {
				continue
			}
			if err := c.removeFile(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if suffix != ".owner" {
				filesChanged = true
			}
		}
	}
	if filesChanged && !c.DryRun {
		if output, err := c.command(ctx, "networkctl", "reload"); err != nil {
			return fmt.Errorf("reload after removing stale VXLAN files: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	if !c.DryRun {
		for _, link := range links {
			if retained[link.IfName] {
				continue
			}
			if output, err := c.command(ctx, "ip", "link", "delete", "dev", link.IfName); err != nil {
				return fmt.Errorf("remove extra VXLAN %s: %w: %s", link.IfName, err, strings.TrimSpace(string(output)))
			}
		}
	}
	return nil
}

type vxlanGateState string

const (
	vxlanGateEnabled    vxlanGateState = "Enabled"
	vxlanGateFalse      vxlanGateState = "WhenFalse"
	vxlanGateUnknown    vxlanGateState = "WhenUnknown"
	vxlanGateSuppressed vxlanGateState = "Suppressed"
)

func (c VXLANTunnelController) gateState(resource api.Resource) vxlanGateState {
	when := resourcequery.ResourceWhen(resource)
	if !resourcequery.ResourceWhenPresent(when) {
		return vxlanGateEnabled
	}
	state, ok := c.Store.(resourcequery.StateStore)
	if !ok || state == nil {
		return vxlanGateUnknown
	}
	if vxlanWhenAcceptsUnknown(when) {
		return vxlanGateUnknown
	}
	if resourcequery.ResourceWhenMatches(when, state) {
		if !c.whenFreshThisBoot(when, state) {
			return vxlanGateUnknown
		}
		return vxlanGateEnabled
	}
	if resourcequery.ResourceWhenIndeterminate(when, state) {
		return vxlanGateUnknown
	}
	return vxlanGateFalse
}

func vxlanWhenAcceptsUnknown(when api.ResourceWhenSpec) bool {
	for _, match := range when.State {
		if strings.EqualFold(match.Status, string(routerstate.StatusUnknown)) || strings.EqualFold(match.Equals, "unknown") {
			return true
		}
		for _, value := range match.In {
			if strings.EqualFold(value, "unknown") {
				return true
			}
		}
	}
	for _, child := range append(append([]api.ResourceWhenSpec(nil), when.All...), when.Any...) {
		if vxlanWhenAcceptsUnknown(child) {
			return true
		}
	}
	return false
}

func (c VXLANTunnelController) whenFreshThisBoot(when api.ResourceWhenSpec, state resourcequery.StateStore) bool {
	if c.StartedAt.IsZero() {
		return true
	}
	for name, match := range when.State {
		if !stateReferenceFresh(name, match, state, c.StartedAt) {
			return false
		}
	}
	for _, child := range append(append([]api.ResourceWhenSpec(nil), when.All...), when.Any...) {
		if !c.whenFreshThisBoot(child, state) {
			return false
		}
	}
	return true
}

func stateReferenceFresh(ref string, match api.StateMatchSpec, state resourcequery.StateStore, startedAt time.Time) bool {
	maxAge := 30 * time.Second
	if match.MaxAge != "" {
		parsed, err := time.ParseDuration(match.MaxAge)
		if err != nil || parsed <= 0 {
			return false
		}
		maxAge = parsed
	}
	now := state.Now().UTC()
	trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(ref, "${"), "}"))
	if left, _, ok := strings.Cut(trimmed, ".status."); ok {
		kind, name, valid := resourcequery.SplitResource(left)
		store, storeOK := state.(resourcequery.Store)
		if !valid || !storeOK {
			return false
		}
		status := store.ObjectStatus(resourcequery.APIVersionForKind(kind), kind, name)
		for _, key := range []string{"observedAt", "updatedAt", "lastTransitionAt"} {
			if observed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(fmt.Sprint(status[key]))); err == nil {
				return !observed.Before(startedAt) && now.Sub(observed) <= maxAge
			}
		}
		return false
	}
	value := state.Get(ref)
	return !value.UpdatedAt.IsZero() && !value.UpdatedAt.Before(startedAt) && now.Sub(value.UpdatedAt) <= maxAge
}

func (c VXLANTunnelController) NextExpiryAfter() time.Duration {
	state, ok := c.Store.(resourcequery.StateStore)
	if !ok || state == nil || c.DeclaredRouter == nil {
		return 0
	}
	now := state.Now().UTC()
	var earliest time.Time
	for _, resource := range c.DeclaredRouter.Spec.Resources {
		if resource.Kind != "VXLANTunnel" {
			continue
		}
		walkWhen(resourcequery.ResourceWhen(resource), func(ref string, match api.StateMatchSpec) {
			updated, ok := stateReferenceUpdatedAt(ref, state)
			if !ok {
				return
			}
			maxAge := 30 * time.Second
			if match.MaxAge != "" {
				if parsed, err := time.ParseDuration(match.MaxAge); err == nil && parsed > 0 {
					maxAge = parsed
				} else {
					return
				}
			}
			deadline := updated.Add(maxAge)
			if earliest.IsZero() || deadline.Before(earliest) {
				earliest = deadline
			}
		})
	}
	if earliest.IsZero() {
		return 0
	}
	if !earliest.After(now) {
		// The reconciliation that just preceded this scheduling decision has
		// already observed and failed closed on the expired value. Returning the
		// normal interval prevents a permanently stale producer from resetting
		// the framework ticker to 1ms forever.
		return 0
	}
	return earliest.Sub(now)
}

func walkWhen(when api.ResourceWhenSpec, visit func(string, api.StateMatchSpec)) {
	for ref, match := range when.State {
		visit(ref, match)
	}
	for _, child := range when.All {
		walkWhen(child, visit)
	}
	for _, child := range when.Any {
		walkWhen(child, visit)
	}
}

func stateReferenceUpdatedAt(ref string, state resourcequery.StateStore) (time.Time, bool) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(ref, "${"), "}"))
	if left, _, ok := strings.Cut(trimmed, ".status."); ok {
		kind, name, valid := resourcequery.SplitResource(left)
		store, storeOK := state.(resourcequery.Store)
		if !valid || !storeOK {
			return time.Time{}, false
		}
		status := store.ObjectStatus(resourcequery.APIVersionForKind(kind), kind, name)
		for _, key := range []string{"observedAt", "updatedAt", "lastTransitionAt"} {
			if observed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(fmt.Sprint(status[key]))); err == nil {
				return observed.UTC(), true
			}
		}
		return time.Time{}, false
	}
	value := state.Get(ref)
	return value.UpdatedAt.UTC(), !value.UpdatedAt.IsZero()
}

// dependencyRevision is a deterministic version tuple over exactly the
// producer values referenced by spec.when. It provides a conservative CAS:
// any producer change between gate evaluation and a kernel commit invalidates
// the commit, including changes that leave the boolean predicate unchanged.
func (c VXLANTunnelController) dependencyRevision(resource api.Resource) (string, bool) {
	state, ok := c.Store.(resourcequery.StateStore)
	if !ok || state == nil {
		return "", false
	}
	entries := map[string]any{}
	walkWhen(resourcequery.ResourceWhen(resource), func(ref string, _ api.StateMatchSpec) {
		trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(ref, "${"), "}"))
		if left, _, found := strings.Cut(trimmed, ".status."); found {
			kind, name, valid := resourcequery.SplitResource(left)
			store, storeOK := state.(resourcequery.Store)
			if valid && storeOK {
				entries[ref] = store.ObjectStatus(resourcequery.APIVersionForKind(kind), kind, name)
			}
			return
		}
		entries[ref] = state.Get(ref)
	})
	data, err := json.Marshal(entries)
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), true
}

func (c VXLANTunnelController) gateRevision(resource api.Resource) (string, bool) {
	if c.gateState(resource) != vxlanGateEnabled {
		return "", false
	}
	return c.dependencyRevision(resource)
}

func (c VXLANTunnelController) revisionStillCurrent(resource api.Resource, revision string) bool {
	current, ok := c.gateRevision(resource)
	return ok && current == revision
}

func vxlansByName(router *api.Router) map[string]api.Resource {
	out := map[string]api.Resource{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind == "VXLANTunnel" {
			out[resource.Metadata.Name] = resource
		}
	}
	return out
}

func (c VXLANTunnelController) reconcileResource(ctx context.Context, resource api.Resource, aliases map[string]string) error {
	spec, err := resource.VXLANTunnelSpec()
	if err != nil {
		return err
	}
	cfg := vxlan.Config{
		Name:              resource.Metadata.Name,
		IfName:            firstNonEmpty(spec.IfName, resource.Metadata.Name),
		VNI:               spec.VNI,
		LocalAddress:      spec.LocalAddress,
		Peers:             append([]string(nil), spec.Peers...),
		UnderlayInterface: aliases[spec.UnderlayInterface],
		UDPPort:           spec.UDPPort,
		MTU:               spec.MTU,
		Bridge:            aliases[spec.Bridge],
		OuterDF:           spec.OuterDF,
	}
	if cfg.UnderlayInterface == "" {
		cfg.UnderlayInterface = spec.UnderlayInterface
	}
	if spec.Bridge != "" && cfg.Bridge == "" {
		cfg.Bridge = spec.Bridge
	}
	return c.reconcileOne(ctx, resource, cfg)
}

func (c VXLANTunnelController) reconcileOne(ctx context.Context, resource api.Resource, cfg vxlan.Config) error {
	status := c.baseStatus(cfg)
	status["gateState"] = string(vxlanGateEnabled)
	if c.DryRun {
		status["phase"] = "Planned"
		return c.Store.SaveObjectStatus(api.NetAPIVersion, "VXLANTunnel", resource.Metadata.Name, status)
	}
	guarded := resourcequery.ResourceWhenPresent(resourcequery.ResourceWhen(resource))
	gateRevision := ""
	if guarded {
		var ok bool
		gateRevision, ok = c.gateRevision(resource)
		if !ok {
			return c.teardownOne(ctx, resource, vxlanGateUnknown)
		}
	}

	observed, showErr := c.command(ctx, "ip", "-details", "link", "show", "dev", cfg.IfName)
	if showErr != nil && !linkObservationMissing(observed, showErr) {
		status["phase"], status["reason"], status["forwarding"], status["error"] = "Error", "LinkObserveFailed", "unknown", showErr.Error()
		return c.Store.SaveObjectStatus(api.NetAPIVersion, "VXLANTunnel", resource.Metadata.Name, status)
	}
	legacyRemoved, err := c.removeOwnedLegacyArtifacts(cfg.IfName)
	if err != nil {
		return err
	}
	if legacyRemoved {
		if _, err := c.command(ctx, "networkctl", "reload"); err != nil {
			return fmt.Errorf("reload after removing legacy VXLAN artifacts: %w", err)
		}
	}
	status["managedBy"] = "routerd"
	status["restartPersistent"] = false
	if showErr == nil && !vxlanDetailsConfigMatch(string(observed), cfg) {
		if err := requireVirtualLink(cfg.IfName); err != nil {
			return err
		}
		if _, err := c.command(ctx, "ip", "link", "set", "dev", cfg.IfName, "down"); err != nil {
			return err
		}
		if _, err := c.command(ctx, "ip", "link", "delete", "dev", cfg.IfName); err != nil {
			return err
		}
		showErr = errors.New("link removed for reconfiguration")
		status["recreated"] = true
	}
	if showErr != nil {
		if err := c.createVXLANDown(ctx, cfg); err != nil {
			return fmt.Errorf("VXLANTunnel/%s create failed: %w", resource.Metadata.Name, err)
		}
		status["created"] = true
	} else {
		if cfg.MTU != 0 {
			if _, err := c.command(ctx, "ip", "link", "set", "dev", cfg.IfName, "mtu", fmt.Sprint(cfg.MTU)); err != nil {
				return err
			}
		}
		if cfg.Bridge != "" {
			if _, err := c.command(ctx, "ip", "link", "set", "dev", cfg.IfName, "master", cfg.Bridge); err != nil {
				return err
			}
		} else if strings.Contains(string(observed), " master ") {
			if _, err := c.command(ctx, "ip", "link", "set", "dev", cfg.IfName, "nomaster"); err != nil {
				return err
			}
		}
	}
	if c.gateState(resource) != vxlanGateEnabled || (guarded && !c.revisionStillCurrent(resource, gateRevision)) {
		return c.teardownOne(ctx, resource, vxlanGateUnknown)
	}
	if err := c.reconcileFloodFDB(ctx, resource, cfg, gateRevision, guarded); err != nil {
		if errors.Is(err, errVXLANGateRevisionChanged) {
			return c.teardownOne(ctx, resource, vxlanGateUnknown)
		}
		return err
	}
	if c.gateState(resource) != vxlanGateEnabled || (guarded && !c.revisionStillCurrent(resource, gateRevision)) {
		return c.teardownOne(ctx, resource, vxlanGateUnknown)
	}
	if _, err := c.command(ctx, "ip", "link", "set", "dev", cfg.IfName, "up"); err != nil {
		return err
	}
	observed, err = c.command(ctx, "ip", "-details", "link", "show", "dev", cfg.IfName)
	if err != nil || !vxlanDetailsMatch(string(observed), cfg) {
		status["phase"] = "Drifted"
		status["reason"] = "ReadbackMismatch"
		status["forwarding"] = false
		if err != nil {
			status["error"] = err.Error()
		}
		return c.Store.SaveObjectStatus(api.NetAPIVersion, "VXLANTunnel", resource.Metadata.Name, status)
	}
	if c.gateState(resource) != vxlanGateEnabled || (guarded && !c.revisionStillCurrent(resource, gateRevision)) {
		// Synchronous post-commit fence. Even if a producer changes in the
		// final syscall window, forwarding is closed before returning.
		return c.teardownOne(ctx, resource, vxlanGateUnknown)
	}
	status["phase"] = "Healthy"
	status["forwarding"] = true
	status["lastEnabledAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "VXLANTunnel", resource.Metadata.Name, status)
}

func (c VXLANTunnelController) createVXLANDown(ctx context.Context, cfg vxlan.Config) error {
	for _, command := range vxlan.Commands(cfg) {
		joined := strings.Join(command, " ")
		if strings.Contains(joined, " link set dev "+cfg.IfName+" up") || (len(command) > 1 && command[0] == "bridge" && command[1] == "fdb") {
			continue
		}
		if _, err := c.command(ctx, command[0], command[1:]...); err != nil {
			return err
		}
	}
	return nil
}

func (c VXLANTunnelController) reconcileFloodFDB(ctx context.Context, resource api.Resource, cfg vxlan.Config, revision string, guarded bool) error {
	out, err := c.command(ctx, "bridge", "fdb", "show", "dev", cfg.IfName)
	if err != nil {
		return fmt.Errorf("read flood FDB on %s: %w", cfg.IfName, err)
	}
	desired := map[string]bool{}
	for _, peer := range cfg.Peers {
		desired[peer] = true
	}
	observedEntries := allZeroFDBEntries(string(out))
	observedByPeer := map[string][]floodFDBEntry{}
	for _, entry := range observedEntries {
		observedByPeer[entry.Destination] = append(observedByPeer[entry.Destination], entry)
	}
	peersToDelete := make([]string, 0)
	for peer, entries := range observedByPeer {
		if desired[peer] && len(entries) == 1 && entries[0].exact() {
			continue
		}
		for range entries {
			peersToDelete = append(peersToDelete, peer)
		}
	}
	sort.Strings(peersToDelete)
	for _, peer := range peersToDelete {
		if guarded && !c.revisionStillCurrent(resource, revision) {
			return fmt.Errorf("%w before FDB delete", errVXLANGateRevisionChanged)
		}
		if _, err := c.command(ctx, "bridge", "fdb", "del", "00:00:00:00:00:00", "dev", cfg.IfName, "dst", peer); err != nil {
			return fmt.Errorf("delete stale flood FDB %s from %s: %w", peer, cfg.IfName, err)
		}
		if guarded && !c.revisionStillCurrent(resource, revision) {
			return fmt.Errorf("%w after FDB delete", errVXLANGateRevisionChanged)
		}
	}
	for _, peer := range cfg.Peers {
		entries := observedByPeer[peer]
		if len(entries) == 1 && entries[0].exact() {
			continue
		}
		if guarded && !c.revisionStillCurrent(resource, revision) {
			return fmt.Errorf("%w before FDB append", errVXLANGateRevisionChanged)
		}
		if _, err := c.command(ctx, "bridge", "fdb", "append", "00:00:00:00:00:00", "dev", cfg.IfName, "dst", peer); err != nil {
			return err
		}
		if guarded && !c.revisionStillCurrent(resource, revision) {
			return fmt.Errorf("%w after FDB append", errVXLANGateRevisionChanged)
		}
	}
	readback, err := c.command(ctx, "bridge", "fdb", "show", "dev", cfg.IfName)
	if err != nil {
		return fmt.Errorf("read back flood FDB on %s: %w", cfg.IfName, err)
	}
	if guarded && !c.revisionStillCurrent(resource, revision) {
		return fmt.Errorf("%w before FDB readback commit", errVXLANGateRevisionChanged)
	}
	actual := allZeroFDBEntries(string(readback))
	if len(actual) != len(desired) {
		return fmt.Errorf("flood FDB readback mismatch on %s", cfg.IfName)
	}
	counts := map[string]int{}
	for _, entry := range actual {
		if !entry.exact() || !desired[entry.Destination] {
			return fmt.Errorf("flood FDB entry for %s has unexpected attributes on %s", entry.Destination, cfg.IfName)
		}
		counts[entry.Destination]++
	}
	for peer := range desired {
		if counts[peer] != 1 {
			return fmt.Errorf("flood FDB peer %s count=%d on %s", peer, counts[peer], cfg.IfName)
		}
	}
	return nil
}

func allZeroFDBEntries(output string) []floodFDBEntry {
	out := []floodFDBEntry{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "00:00:00:00:00:00" {
			continue
		}
		entry := floodFDBEntry{}
		for i := 1; i+1 < len(fields); i++ {
			if fields[i] == "dst" {
				entry.Destination = fields[i+1]
			}
		}
		for _, field := range fields[1:] {
			entry.Self = entry.Self || field == "self"
			entry.Permanent = entry.Permanent || field == "permanent"
		}
		if entry.Destination != "" {
			out = append(out, entry)
		}
	}
	return out
}

func (c VXLANTunnelController) teardownOne(ctx context.Context, resource api.Resource, gate vxlanGateState) error {
	spec, err := resource.VXLANTunnelSpec()
	if err != nil {
		return err
	}
	ifname := firstNonEmpty(spec.IfName, resource.Metadata.Name)
	status := c.baseStatus(vxlan.Config{Name: resource.Metadata.Name, IfName: ifname, VNI: spec.VNI, LocalAddress: spec.LocalAddress, Peers: spec.Peers, UnderlayInterface: spec.UnderlayInterface, MTU: spec.MTU, Bridge: spec.Bridge, OuterDF: spec.OuterDF})
	status["gateState"] = string(gate)
	status["forwarding"] = false
	if c.DryRun {
		status["phase"] = "Planned"
		status["reason"] = "WouldDisable"
		return c.Store.SaveObjectStatus(api.NetAPIVersion, "VXLANTunnel", resource.Metadata.Name, status)
	}

	observed, showErr := c.command(ctx, "ip", "-details", "link", "show", "dev", ifname)
	if showErr != nil && !linkObservationMissing(observed, showErr) {
		return c.saveTeardownError(resource, status, "LinkObserveFailed", showErr)
	}
	if showErr == nil {
		if err := requireVirtualLink(ifname); err != nil {
			return c.saveTeardownError(resource, status, "LinkTypeMismatch", err)
		}
		// Stop forwarding before deleting the interface.
		if _, err := c.command(ctx, "ip", "link", "set", "dev", ifname, "down"); err != nil {
			return c.saveTeardownError(resource, status, "LinkDownFailed", err)
		}
		if _, err := c.command(ctx, "ip", "link", "delete", "dev", ifname); err != nil {
			return c.saveTeardownError(resource, status, "LinkDeleteFailed", err)
		}
		status["deleted"] = true
	}
	removed, err := c.removeOwnedArtifacts(ifname)
	if err != nil {
		return c.saveTeardownError(resource, status, "ArtifactDeleteFailed", err)
	}
	if removed {
		if _, err := c.command(ctx, "networkctl", "reload"); err != nil {
			return c.saveTeardownError(resource, status, "NetworkdReloadFailed", err)
		}
	}
	finalObserved, finalErr := c.command(ctx, "ip", "-details", "link", "show", "dev", ifname)
	if finalErr == nil {
		return c.saveTeardownError(resource, status, "LinkReappeared", fmt.Errorf("VXLAN link %s exists after teardown", ifname))
	}
	if !linkObservationMissing(finalObserved, finalErr) {
		return c.saveTeardownError(resource, status, "LinkVerifyFailed", finalErr)
	}
	status["phase"] = "Disabled"
	status["reason"] = string(gate)
	status["lastDisabledAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "VXLANTunnel", resource.Metadata.Name, status)
}

// Physical interfaces and loopback cannot be replaced by a VXLAN link.
func requireVirtualLink(ifname string) error {
	path, err := filepath.EvalSymlinks(filepath.Join("/sys/class/net", ifname))
	if err != nil {
		return fmt.Errorf("observe interface %s: %w", ifname, err)
	}
	if ifname == "lo" || !strings.HasPrefix(path, "/sys/devices/virtual/net/") {
		return fmt.Errorf("interface %s is physical or loopback; cannot replace it with VXLAN", ifname)
	}
	return nil
}

func linkObservationMissing(output []byte, err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(string(output) + " " + err.Error())
	for _, marker := range []string{"not found", "does not exist", "cannot find device", "no such device"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func (c VXLANTunnelController) saveTeardownError(resource api.Resource, status map[string]any, reason string, err error) error {
	status["phase"] = "Error"
	status["reason"] = reason
	status["error"] = err.Error()
	_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "VXLANTunnel", resource.Metadata.Name, status)
	return fmt.Errorf("VXLANTunnel/%s fail-closed teardown: %w", resource.Metadata.Name, err)
}

func (c VXLANTunnelController) baseStatus(cfg vxlan.Config) map[string]any {
	return map[string]any{
		"phase":          "Pending",
		"ifname":         cfg.IfName,
		"vni":            cfg.VNI,
		"localAddress":   cfg.LocalAddress,
		"underlayIfname": cfg.UnderlayInterface,
		"bridgeIfname":   cfg.Bridge,
		"peers":          append([]string(nil), cfg.Peers...),
		"outerDF":        defaultVXLANOuterDF(cfg.OuterDF),
		"observedAt":     time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (c VXLANTunnelController) networkdPath(path string) string {
	base := strings.TrimSpace(c.NetworkdDir)
	if base == "" {
		defaults, _ := platform.Current()
		base = defaults.NetworkdDropinDir
	}
	if base == "" {
		base = "/etc/systemd/network"
	}
	rel := strings.TrimPrefix(filepath.Clean(path), filepath.Clean("/etc/systemd/network"))
	return filepath.Join(base, strings.TrimPrefix(rel, string(filepath.Separator)))
}

func (c VXLANTunnelController) artifactPaths(ifname string) []string {
	return []string{
		c.networkdPath(filepath.Join("/etc/systemd/network", "31-routerd-"+ifname+".owner")),
		// Legacy auto-activating artifacts are cleanup-only. Gated VXLANs are
		// runtime resources and must never be recreated before fresh predicates.
		c.networkdPath(filepath.Join("/etc/systemd/network", "31-routerd-"+ifname+".netdev")),
		c.networkdPath(filepath.Join("/etc/systemd/network", "31-routerd-"+ifname+".network")),
	}
}

func (c VXLANTunnelController) removeOwnedArtifacts(ifname string) (bool, error) {
	removed := false
	for i, path := range c.artifactPaths(ifname) {
		_, err := c.readFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return removed, err
		}
		if err := c.removeFile(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, err
		}
		removed = removed || i > 0
	}
	return removed, nil
}

func (c VXLANTunnelController) removeOwnedLegacyArtifacts(ifname string) (bool, error) {
	removed := false
	paths := c.artifactPaths(ifname)
	for _, path := range paths[1:] {
		_, err := c.readFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return removed, err
		}
		if err := c.removeFile(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, err
		}
		removed = true
	}
	return removed, nil
}

func (c VXLANTunnelController) readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (c VXLANTunnelController) removeFile(path string) error {
	return os.Remove(path)
}

func (c VXLANTunnelController) command(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func vxlanDetailsMatch(observed string, cfg vxlan.Config) bool {
	if !vxlanLinkFlag(observed, "UP") || !vxlanDetailsConfigMatch(observed, cfg) {
		return false
	}
	if cfg.MTU != 0 && !strings.Contains(observed, " mtu "+fmt.Sprint(cfg.MTU)+" ") {
		return false
	}
	if cfg.Bridge != "" {
		return strings.Contains(observed, "master "+cfg.Bridge)
	}
	return !strings.Contains(observed, " master ")
}

func vxlanLinkFlag(observed, wanted string) bool {
	start := strings.Index(observed, "<")
	end := strings.Index(observed, ">")
	if start < 0 || end <= start {
		return false
	}
	for _, flag := range strings.Split(observed[start+1:end], ",") {
		if strings.TrimSpace(flag) == wanted {
			return true
		}
	}
	return false
}

func vxlanDetailsConfigMatch(observed string, cfg vxlan.Config) bool {
	port := cfg.UDPPort
	if port == 0 {
		port = 4789
	}
	for _, want := range []string{
		"vxlan id " + fmt.Sprint(cfg.VNI),
		"local " + cfg.LocalAddress,
		"dev " + cfg.UnderlayInterface,
		"dstport " + fmt.Sprint(port),
		"nolearning",
	} {
		if !strings.Contains(observed, want) {
			return false
		}
	}
	if !strings.Contains(observed, " df "+defaultVXLANOuterDF(cfg.OuterDF)) {
		return false
	}
	return true
}

func defaultVXLANOuterDF(value string) string {
	if value == "" {
		return "inherit"
	}
	return value
}
