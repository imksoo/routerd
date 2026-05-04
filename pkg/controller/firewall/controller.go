package firewall

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/render"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type Controller struct {
	Router       *api.Router
	Bus          *bus.Bus
	Store        Store
	DryRun       bool
	NftablesPath string
	NftCommand   string
	Interval     time.Duration
	Logger       *slog.Logger
}

func (c Controller) Start(ctx context.Context) {
	if c.Router == nil || c.Bus == nil || c.Store == nil {
		return
	}
	interval := c.Interval
	if interval == 0 {
		interval = 5 * time.Second
	}
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.*"}}, 32)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		_ = c.reconcileLogged(ctx)
		for {
			select {
			case event, ok := <-ch:
				if !ok {
					return
				}
				if strings.HasPrefix(event.Type, "routerd.firewall.") {
					continue
				}
				_ = c.reconcileLogged(ctx)
			case <-ticker.C:
				_ = c.reconcileLogged(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c Controller) reconcileLogged(ctx context.Context) error {
	if err := c.Reconcile(ctx); err != nil {
		if c.Logger != nil {
			c.Logger.Warn("firewall reconcile failed", "error", err)
		}
		return err
	}
	return nil
}

func (c Controller) Reconcile(ctx context.Context) error {
	if !hasFirewall(c.Router) {
		return nil
	}
	holes := deriveHoles(c.Router)
	data, err := render.NftablesFirewall(c.Router, holes)
	if err != nil {
		return err
	}
	path := firstNonEmpty(c.NftablesPath, "/run/routerd/firewall.nft")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	previous, readErr := os.ReadFile(path)
	changed := readErr != nil || !bytes.Equal(previous, data)
	if changed {
		if err := os.WriteFile(path, data, 0644); err != nil {
			return err
		}
	}
	nft := firstNonEmpty(c.NftCommand, "nft")
	if changed {
		if err := checkNftablesRuleset(ctx, nft, path); err != nil {
			return err
		}
	}
	if changed && !c.DryRun {
		_ = exec.CommandContext(ctx, nft, "delete", "table", "inet", "routerd_filter").Run()
		out, err := exec.CommandContext(ctx, nft, "-f", path).CombinedOutput()
		if err != nil {
			return fmt.Errorf("nft -f %s: %w: %s", path, err, strings.TrimSpace(string(out)))
		}
	}
	status := map[string]any{
		"phase":         "Applied",
		"dryRun":        c.DryRun,
		"changed":       changed,
		"rules":         firewallRuleCount(c.Router),
		"internalHoles": len(holes),
		"nftablesPath":  path,
		"conditions":    []map[string]any{{"type": "Applied", "status": "True", "reason": "Rendered"}},
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.FirewallAPIVersion {
			continue
		}
		if err := c.Store.SaveObjectStatus(api.FirewallAPIVersion, resource.Kind, resource.Metadata.Name, status); err != nil {
			return err
		}
	}
	if changed && c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.firewall.rules.applied", daemonapi.SeverityInfo)
		event.Attributes = map[string]string{"nftablesPath": path, "dryRun": fmt.Sprintf("%t", c.DryRun), "internalHoles": fmt.Sprintf("%d", len(holes))}
		_ = c.Bus.Publish(ctx, event)
	}
	return nil
}

func checkNftablesRuleset(ctx context.Context, nft, path string) error {
	out, err := exec.CommandContext(ctx, nft, "-c", "-f", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s -c -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func hasFirewall(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.FirewallAPIVersion && (resource.Kind == "FirewallZone" || resource.Kind == "FirewallPolicy" || resource.Kind == "FirewallRule") {
			return true
		}
	}
	return false
}

func firewallRuleCount(router *api.Router) int {
	n := 0
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.FirewallAPIVersion && resource.Kind == "FirewallRule" {
			n++
		}
	}
	return n
}

func deriveHoles(router *api.Router) []render.FirewallHole {
	zones := zoneIndex(router)
	var holes []render.FirewallHole
	add := func(name, from, to, proto string, port int, comment string) {
		if from == "" || to == "" {
			return
		}
		holes = append(holes, render.FirewallHole{Name: name, FromZone: from, ToZone: to, Protocol: proto, Port: port, Action: "accept", Comment: comment})
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "DHCPv6PrefixDelegation":
			spec, _ := resource.DHCPv6PrefixDelegationSpec()
			add(resource.Metadata.Name+"-dhcpv6-client", zones.byResource(spec.Interface), "self", "udp", 546, resource.ID())
		case "DHCPv6Information":
			spec, _ := resource.DHCPv6InformationSpec()
			add(resource.Metadata.Name+"-dhcpv6-info", zones.byResource(spec.Interface), "self", "udp", 546, resource.ID())
		case "DHCPv4Lease":
			spec, _ := resource.DHCPv4LeaseSpec()
			add(resource.Metadata.Name+"-dhcpv4-client", zones.byResource(spec.Interface), "self", "udp", 68, resource.ID())
		case "DSLiteTunnel":
			spec, _ := resource.DSLiteTunnelSpec()
			add(resource.Metadata.Name+"-dslite-ipip", "self", zones.byResource(spec.Interface), "ipip", 0, resource.ID())
		case "DHCPv4Server":
			spec, _ := resource.DHCPv4ServerSpec()
			if spec.Interface != "" {
				add(resource.Metadata.Name+"-dhcpv4-server", zones.byResource(spec.Interface), "self", "udp", 67, resource.ID())
			}
		case "DHCPv6Server":
			spec, _ := resource.DHCPv6ServerSpec()
			if spec.Interface != "" {
				add(resource.Metadata.Name+"-dhcpv6-server", zones.byResource(spec.Interface), "self", "udp", 547, resource.ID())
			}
		case "DNSResolver":
			spec, _ := resource.DNSResolverSpec()
			for _, listen := range spec.Listen {
				for _, zone := range zones.byListenAddress(listen.Addresses) {
					add(resource.Metadata.Name+"-dns-udp-"+zone, zone, "self", "udp", listen.Port, resource.ID())
					add(resource.Metadata.Name+"-dns-tcp-"+zone, zone, "self", "tcp", listen.Port, resource.ID())
				}
			}
		case "IPv6RouterAdvertisement":
			spec, _ := resource.IPv6RouterAdvertisementSpec()
			add(resource.Metadata.Name+"-ra", "self", zones.byResource(spec.Interface), "icmpv6", 0, resource.ID())
		case "WireGuardInterface":
			spec, _ := resource.WireGuardInterfaceSpec()
			if spec.ListenPort != 0 {
				add(resource.Metadata.Name+"-wireguard", zones.firstUntrust(), "self", "udp", spec.ListenPort, resource.ID())
			}
		case "HealthCheck":
			spec, _ := resource.HealthCheckSpec()
			if spec.Protocol == "tcp" || spec.Protocol == "dns" || spec.Protocol == "http" {
				proto := "tcp"
				if spec.Protocol == "dns" {
					proto = "udp"
				}
				add(resource.Metadata.Name+"-healthcheck", "self", zones.byResource(spec.Interface), proto, spec.Port, resource.ID())
			}
		}
	}
	sort.Slice(holes, func(i, j int) bool { return holes[i].Name < holes[j].Name })
	return holes
}

type zonesByRef struct {
	resource map[string]string
	role     map[string]string
}

func zoneIndex(router *api.Router) zonesByRef {
	out := zonesByRef{resource: map[string]string{}, role: map[string]string{}}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.FirewallAPIVersion || resource.Kind != "FirewallZone" {
			continue
		}
		spec, err := resource.FirewallZoneSpec()
		if err != nil {
			continue
		}
		out.role[resource.Metadata.Name] = spec.Role
		for _, ref := range spec.Interfaces {
			kind, name := splitRef(ref)
			out.resource[name] = resource.Metadata.Name
			out.resource[kind+"/"+name] = resource.Metadata.Name
		}
	}
	return out
}

func (z zonesByRef) byResource(name string) string {
	if zone := z.resource[name]; zone != "" {
		return zone
	}
	if _, short, ok := strings.Cut(name, "/"); ok {
		return z.resource[short]
	}
	return ""
}

func (z zonesByRef) firstUntrust() string {
	var names []string
	for name, role := range z.role {
		if role == "untrust" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func (z zonesByRef) byListenAddress(addresses []string) []string {
	var out []string
	for zone, role := range z.role {
		if role == "untrust" {
			continue
		}
		for _, address := range addresses {
			if address == "127.0.0.1" || address == "::1" {
				continue
			}
			if zone != "" && !contains(out, zone) {
				out = append(out, zone)
			}
		}
	}
	sort.Strings(out)
	return out
}

func splitRef(ref string) (string, string) {
	if kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/"); ok {
		return kind, name
	}
	return "Interface", strings.TrimSpace(ref)
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
