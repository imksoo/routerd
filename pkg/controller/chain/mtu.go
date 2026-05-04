package chain

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/daemonapi"
	"routerd/pkg/render"
)

type PathMTUPolicyController struct {
	Router *api.Router
	Bus    interface {
		Publish(context.Context, daemonapi.DaemonEvent) error
	}
	Store      Store
	DryRun     bool
	NftCommand string
	Probe      func(context.Context, string, api.PathMTUPolicyMTUProbeSpec) (int, error)
	Path       string
}

func (c PathMTUPolicyController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	router, observed, err := c.renderRouter(ctx)
	if err != nil {
		return err
	}
	data, err := render.NftablesIPv4SourceNAT(router)
	if err != nil {
		return err
	}
	path := firstNonEmpty(c.Path, "/run/routerd/mss.nft")
	nft := firstNonEmpty(c.NftCommand, "nft")
	changed, err := c.applyTable(ctx, nft, path, data)
	if err != nil {
		return err
	}
	linkMTUChanged, err := c.applyInterfaceMTUs(ctx, router)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "PathMTUPolicy" {
			continue
		}
		current := c.Store.ObjectStatus(api.NetAPIVersion, "PathMTUPolicy", resource.Metadata.Name)
		policyStatus := observed[resource.Metadata.Name]
		if policyStatus == nil {
			policyStatus = map[string]any{}
		}
		policyStatus["phase"] = "Applied"
		policyStatus["nftTable"] = "routerd_mss"
		policyStatus["nftPath"] = path
		policyStatus["changed"] = changed
		policyStatus["interfaceMTUChanged"] = linkMTUChanged[resource.Metadata.Name]
		policyStatus["dryRun"] = c.DryRun
		if changed || current["updatedAt"] == nil {
			policyStatus["updatedAt"] = now.Format(time.RFC3339Nano)
		} else {
			policyStatus["updatedAt"] = current["updatedAt"]
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "PathMTUPolicy", resource.Metadata.Name, map[string]any{
			"phase":               policyStatus["phase"],
			"nftTable":            policyStatus["nftTable"],
			"nftPath":             policyStatus["nftPath"],
			"changed":             policyStatus["changed"],
			"interfaceMTUChanged": policyStatus["interfaceMTUChanged"],
			"dryRun":              policyStatus["dryRun"],
			"updatedAt":           policyStatus["updatedAt"],
			"mtu":                 policyStatus["mtu"],
			"mtuSource":           policyStatus["mtuSource"],
			"mtuObservedAt":       policyStatus["mtuObservedAt"],
			"mtuProbeErrors":      policyStatus["mtuProbeErrors"],
			"mtuProbeTargets":     policyStatus["mtuProbeTargets"],
		}); err != nil {
			return err
		}
	}
	if changed && c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.net.path_mtu.applied", daemonapi.SeverityInfo)
		event.Attributes = map[string]string{"path": path, "table": "routerd_mss"}
		if err := c.Bus.Publish(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (c PathMTUPolicyController) applyInterfaceMTUs(ctx context.Context, router *api.Router) (map[string]bool, error) {
	changed := map[string]bool{}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "PathMTUPolicy" {
			continue
		}
		spec, err := resource.PathMTUPolicySpec()
		if err != nil {
			return changed, err
		}
		if !spec.InterfaceMTU.Enabled {
			continue
		}
		mtu := spec.MTU.Value
		if mtu == 0 {
			continue
		}
		for _, to := range spec.ToInterfaces {
			ifname := c.resourceIfName(to)
			if ifname == "" {
				continue
			}
			updated, err := c.ensureInterfaceMTU(ctx, ifname, mtu)
			if err != nil {
				return changed, err
			}
			if updated {
				changed[resource.Metadata.Name] = true
			}
		}
	}
	return changed, nil
}

func (c PathMTUPolicyController) ensureInterfaceMTU(ctx context.Context, ifname string, mtu int) (bool, error) {
	if c.DryRun {
		return false, nil
	}
	current, err := currentInterfaceMTU(ctx, ifname)
	if err == nil && current == mtu {
		return false, nil
	}
	if out, err := exec.CommandContext(ctx, "ip", "link", "set", "dev", ifname, "mtu", strconv.Itoa(mtu)).CombinedOutput(); err != nil {
		return false, fmt.Errorf("ip link set dev %s mtu %d: %w: %s", ifname, mtu, err, strings.TrimSpace(string(out)))
	}
	return true, nil
}

func currentInterfaceMTU(ctx context.Context, ifname string) (int, error) {
	out, err := exec.CommandContext(ctx, "ip", "-o", "link", "show", "dev", ifname).CombinedOutput()
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(out))
	for i, field := range fields {
		if field == "mtu" && i+1 < len(fields) {
			return strconv.Atoi(fields[i+1])
		}
	}
	return 0, fmt.Errorf("mtu not found for %s", ifname)
}

func (c PathMTUPolicyController) renderRouter(ctx context.Context) (*api.Router, map[string]map[string]any, error) {
	out := &api.Router{TypeMeta: c.Router.TypeMeta, Metadata: c.Router.Metadata}
	observed := map[string]map[string]any{}
	for _, resource := range c.Router.Spec.Resources {
		switch resource.Kind {
		case "Interface", "PPPoEInterface", "DSLiteTunnel", "PathMTUPolicy":
			if resource.Kind == "PathMTUPolicy" {
				updated, status, err := c.resolvePolicyMTU(ctx, resource)
				if err != nil {
					return nil, nil, err
				}
				resource = updated
				observed[resource.Metadata.Name] = status
			}
			out.Spec.Resources = append(out.Spec.Resources, resource)
		}
	}
	return out, observed, nil
}

func (c PathMTUPolicyController) resolvePolicyMTU(ctx context.Context, resource api.Resource) (api.Resource, map[string]any, error) {
	status := map[string]any{}
	spec, err := resource.PathMTUPolicySpec()
	if err != nil {
		return resource, status, err
	}
	switch firstNonEmpty(spec.MTU.Source, "minInterface") {
	case "probe":
		mtu, probeStatus := c.probedMTU(ctx, resource.Metadata.Name, spec)
		for key, value := range probeStatus {
			status[key] = value
		}
		spec.MTU.Source = "static"
		spec.MTU.Value = mtu
		resource.Spec = spec
		status["mtu"] = mtu
		status["mtuSource"] = "probe"
	case "static":
		status["mtu"] = spec.MTU.Value
		status["mtuSource"] = "static"
	default:
		status["mtuSource"] = firstNonEmpty(spec.MTU.Source, "minInterface")
	}
	return resource, status, nil
}

func (c PathMTUPolicyController) probedMTU(ctx context.Context, name string, spec api.PathMTUPolicySpec) (int, map[string]any) {
	probe := normalizeMTUProbe(spec.MTU)
	status := map[string]any{
		"mtuProbeTargets": strings.Join(probe.Targets, ","),
	}
	interval, _ := time.ParseDuration(probe.Interval)
	if cached, ok := c.cachedProbeMTU(name, interval); ok {
		status["mtuObservedAt"] = cached.observedAt.Format(time.RFC3339Nano)
		return cached.mtu, status
	}
	prober := c.Probe
	if prober == nil {
		prober = defaultPathMTUProbe
	}
	best := 0
	var failures []string
	for _, to := range spec.ToInterfaces {
		ifname := c.resourceIfName(to)
		if ifname == "" {
			failures = append(failures, to+": interface not found")
			continue
		}
		for _, target := range probe.Targets {
			candidate := probe
			candidate.Targets = []string{target}
			mtu, err := prober(ctx, ifname, candidate)
			if err != nil {
				failures = append(failures, ifname+" "+target+": "+err.Error())
				continue
			}
			if best == 0 || mtu < best {
				best = mtu
			}
			break
		}
	}
	now := time.Now().UTC()
	if best == 0 {
		best = probe.Fallback
		status["mtuProbeErrors"] = strings.Join(failures, "; ")
	} else {
		status["mtuObservedAt"] = now.Format(time.RFC3339Nano)
	}
	return best, status
}

type cachedPathMTU struct {
	mtu        int
	observedAt time.Time
}

func (c PathMTUPolicyController) cachedProbeMTU(name string, interval time.Duration) (cachedPathMTU, bool) {
	if c.Store == nil || interval <= 0 {
		return cachedPathMTU{}, false
	}
	status := c.Store.ObjectStatus(api.NetAPIVersion, "PathMTUPolicy", name)
	mtu, ok := intFromStatus(status["mtu"])
	if !ok || mtu == 0 {
		return cachedPathMTU{}, false
	}
	raw, ok := status["mtuObservedAt"].(string)
	if !ok || raw == "" {
		return cachedPathMTU{}, false
	}
	observedAt, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || time.Since(observedAt) > interval {
		return cachedPathMTU{}, false
	}
	return cachedPathMTU{mtu: mtu, observedAt: observedAt}, true
}

func (c PathMTUPolicyController) resourceIfName(name string) string {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Metadata.Name != name {
			continue
		}
		switch resource.Kind {
		case "Interface":
			spec, err := resource.InterfaceSpec()
			if err == nil {
				return spec.IfName
			}
		case "PPPoEInterface":
			spec, err := resource.PPPoEInterfaceSpec()
			if err == nil {
				return firstNonEmpty(spec.IfName, resource.Metadata.Name)
			}
		case "DSLiteTunnel":
			spec, err := resource.DSLiteTunnelSpec()
			if err == nil {
				return firstNonEmpty(spec.TunnelName, resource.Metadata.Name)
			}
		}
	}
	return ""
}

func normalizeMTUProbe(mtu api.PathMTUPolicyMTUSpec) api.PathMTUPolicyMTUProbeSpec {
	probe := mtu.Probe
	if probe.Family == "" {
		probe.Family = "ipv4"
	}
	if len(probe.Targets) == 0 {
		if probe.Family == "ipv6" {
			probe.Targets = []string{"2606:4700:4700::1111", "2001:4860:4860::8888"}
		} else {
			probe.Targets = []string{"1.1.1.1", "8.8.8.8"}
		}
	}
	if probe.Min == 0 {
		probe.Min = 1280
	}
	if probe.Max == 0 {
		probe.Max = mtu.Value
	}
	if probe.Max == 0 {
		probe.Max = 1500
	}
	if probe.Fallback == 0 {
		probe.Fallback = mtu.Value
	}
	if probe.Fallback == 0 {
		probe.Fallback = 1454
	}
	if probe.Interval == "" {
		probe.Interval = "10m"
	}
	if probe.Timeout == "" {
		probe.Timeout = "1s"
	}
	return probe
}

func defaultPathMTUProbe(ctx context.Context, ifname string, probe api.PathMTUPolicyMTUProbeSpec) (int, error) {
	timeout, err := time.ParseDuration(firstNonEmpty(probe.Timeout, "1s"))
	if err != nil {
		return 0, err
	}
	deadline, cancel := context.WithTimeout(ctx, timeout*time.Duration(maxInt(1, len(probe.Targets)))+time.Second)
	defer cancel()
	target := probe.Targets[0]
	low := probe.Min
	high := probe.Max
	best := 0
	for low <= high {
		mid := (low + high) / 2
		ok := pingMTU(deadline, ifname, target, probe.Family, mid, timeout)
		if ok {
			best = mid
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	if best == 0 {
		return 0, fmt.Errorf("no working MTU for %s via %s", target, ifname)
	}
	return best, nil
}

func pingMTU(ctx context.Context, ifname, target, family string, mtu int, timeout time.Duration) bool {
	overhead := 28
	bin := "ping"
	args := []string{"-4", "-M", "do"}
	if family == "ipv6" {
		overhead = 48
		bin = "ping"
		args = []string{"-6", "-M", "do"}
	}
	payload := mtu - overhead
	if payload <= 0 {
		return false
	}
	wait := maxInt(1, int(timeout.Seconds()))
	args = append(args, "-c", "1", "-W", strconv.Itoa(wait), "-I", ifname, "-s", strconv.Itoa(payload), target)
	return exec.CommandContext(ctx, bin, args...).Run() == nil
}

func intFromStatus(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(typed)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (c PathMTUPolicyController) applyTable(ctx context.Context, nft, path string, data []byte) (bool, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		if !c.DryRun {
			_ = exec.CommandContext(ctx, nft, "delete", "table", "inet", "routerd_mss").Run()
		}
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	changed, err := writeFileIfChanged(path, data, 0644, false)
	if err != nil {
		return false, err
	}
	if c.DryRun {
		return changed, nil
	}
	if changed {
		if out, err := exec.CommandContext(ctx, nft, "-c", "-f", path).CombinedOutput(); err != nil {
			return changed, fmt.Errorf("%s -c -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
		}
	}
	if !changed && exec.CommandContext(ctx, nft, "list", "table", "inet", "routerd_mss").Run() == nil {
		return false, nil
	}
	_ = exec.CommandContext(ctx, nft, "delete", "table", "inet", "routerd_mss").Run()
	if out, err := exec.CommandContext(ctx, nft, "-f", path).CombinedOutput(); err != nil {
		return changed, fmt.Errorf("%s -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	return true, nil
}
