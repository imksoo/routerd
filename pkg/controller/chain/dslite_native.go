// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

const dsliteNativeStatusName = "dslite-native"

var errDSLiteInputsChanged = errors.New("DS-Lite source or dependency changed")

func (c DSLiteTunnelController) checkTunnelInputs(ctx context.Context, name string, spec api.DSLiteTunnelSpec, plan dsliteTunnelPlan) error {
	ready, err := resourcequery.ReadDependenciesReady(c.Store, spec.DependsOn)
	if err != nil {
		return err
	}
	if !ready {
		return errDSLiteInputsChanged
	}
	if spec.LocalAddressSource != "delegatedAddress" {
		return nil
	}
	source, active, err := c.delegatedEndpointSource(spec)
	if err != nil {
		return err
	}
	if !active || source.Address != plan.Local {
		return errDSLiteInputsChanged
	}
	if _, _, err := currentPDAddressLifetimes(source.Lease, c.now(), true); err != nil {
		return errDSLiteInputsChanged
	}
	if source.SharedLAN {
		ready, err := c.sharedEndpointReady(ctx, source)
		if err != nil {
			return err
		}
		if !ready {
			return errDSLiteInputsChanged
		}
		return c.recheckTunnelLease(spec, source)
	}
	receipts, err := c.endpointReceipts()
	if err != nil {
		return err
	}
	for _, receipt := range receipts {
		if receipt.Name != name {
			continue
		}
		if receipt.Pending || receipt.Interface != source.Interface || receipt.Address != source.Address+"/128" || receipt.PDName != source.PDName || receipt.PDPrefix != source.PDPrefix || receipt.ValidUntil != pdLifetimeDeadline(source.Lease.LastReplyAt, source.Lease.ValidSeconds) {
			return errDSLiteInputsChanged
		}
		observed, err := c.observeEndpoint(ctx, source.Interface, receipt.Address)
		if err != nil {
			return err
		}
		same, err := c.endpointReceiptIdentity(receipt, observed)
		if err != nil {
			return err
		}
		if !same || !observed.Present || !observed.Ready || observed.OtherPrefix {
			return errDSLiteInputsChanged
		}
		return c.recheckTunnelLease(spec, source)
	}
	return errDSLiteInputsChanged
}

func (c DSLiteTunnelController) recheckTunnelLease(spec api.DSLiteTunnelSpec, prior dsliteEndpointSource) error {
	fresh, active, err := c.delegatedEndpointSource(spec)
	if err != nil {
		return err
	}
	if !active || fresh.Interface != prior.Interface || fresh.Address != prior.Address || fresh.PDName != prior.PDName || fresh.PDPrefix != prior.PDPrefix || !fresh.Lease.LastReplyAt.Equal(prior.Lease.LastReplyAt) || fresh.Lease.PreferredSeconds != prior.Lease.PreferredSeconds || fresh.Lease.ValidSeconds != prior.Lease.ValidSeconds {
		return errDSLiteInputsChanged
	}
	if _, _, err := currentPDAddressLifetimes(fresh.Lease, c.now(), true); err != nil {
		return errDSLiteInputsChanged
	}
	return nil
}

func (c DSLiteTunnelController) requireTunnelInputs(ctx context.Context, name string, spec api.DSLiteTunnelSpec, plan dsliteTunnelPlan) error {
	err := c.checkTunnelInputs(ctx, name, spec, plan)
	if errors.Is(err, errDSLiteInputsChanged) {
		if withdrawErr := c.withdrawNativeTunnelResource(ctx, name); withdrawErr != nil {
			return fmt.Errorf("withdraw DS-Lite after source change: %w", withdrawErr)
		}
		return err
	}
	return err
}

// The tunnel owns its inner addresses. The source /128 has its separate finite
// PD owner, or belongs to LAN. Display status and artifact GC grant no authority.
type dsliteTunnelPlan struct {
	Name, Underlay, Local, Remote, InnerLocal, EncapLimit string
	MTU, RouteMetric                                      int
	DefaultRoute                                          bool
}

type dsliteTunnelReceipt struct {
	Resource   string           `json:"resource"`
	OS         platform.OS      `json:"os"`
	Scope      string           `json:"scope"`
	Token      string           `json:"token"`
	IfIndex    int              `json:"ifIndex"`
	Pending    bool             `json:"pending"`
	OwnDefault bool             `json:"ownDefault"`
	Plan       dsliteTunnelPlan `json:"plan"`
}

type dsliteTunnelObservation struct {
	Exists, Up                                                          bool
	Index, MTU                                                          int
	Token, Kind, Proto, Local, Remote, Underlay, EncapLimit, InnerLocal string
}

func (c DSLiteTunnelController) currentOS() platform.OS {
	if c.OS != "" {
		return c.OS
	}
	return platform.CurrentOS()
}

func dsliteKernelScope(scope string, osName platform.OS) (string, bool) {
	parts := strings.Split(scope, ":")
	count := 3
	if osName == platform.OSFreeBSD {
		count = 4
	}
	if len(parts) != count || parts[0] != string(osName) {
		return "", false
	}
	for _, part := range parts {
		if part == "" {
			return "", false
		}
	}
	return strings.Join(parts[:len(parts)-1], ":"), true
}

func (c DSLiteTunnelController) tunnelScope(ctx context.Context) (string, error) {
	var scope string
	var err error
	if c.NativeScope != nil {
		scope, err = c.NativeScope(ctx)
	} else {
		scope, err = platform.KernelNetworkScope(ctx)
	}
	if err != nil {
		return "", err
	}
	if _, valid := dsliteKernelScope(scope, c.currentOS()); !valid {
		return "", errors.New("invalid DS-Lite native kernel scope")
	}
	return scope, nil
}

func (c DSLiteTunnelController) interfaceGlobalIPv6(ctx context.Context, ifname string) (string, string, error) {
	if c.currentOS() == platform.OSFreeBSD {
		list, err := c.endpointCommand(ctx, "ifconfig", "-l")
		if err != nil {
			return "", "", err
		}
		if !fieldsContain(strings.Fields(string(list)), ifname) {
			return "", "", nil
		}
		out, err := c.endpointCommand(ctx, "ifconfig", ifname)
		if err != nil {
			return "", "", err
		}
		if !strings.HasPrefix(string(out), ifname+":") {
			return "", "", errors.New("invalid DS-Lite source interface observation")
		}
		return firstUsableIfconfigGlobalIPv6(out), "", nil
	}
	out, err := c.endpointCommand(ctx, "ip", "-j", "-6", "addr", "show")
	if err != nil {
		return "", "", err
	}
	var links []struct {
		Name      string          `json:"ifname"`
		Addresses []ipJSONAddress `json:"addr_info"`
	}
	if err := json.Unmarshal(out, &links); err != nil || links == nil {
		return "", "", errors.New("invalid DS-Lite source interface observation")
	}
	for _, link := range links {
		if link.Name == "" {
			return "", "", errors.New("DS-Lite source observation has no interface identity")
		}
		if link.Name != ifname {
			continue
		}
		if link.Addresses == nil {
			return "", "", errors.New("DS-Lite source interface observation has no address array")
		}
		for _, info := range link.Addresses {
			if info.Family != "inet6" {
				continue
			}
			address, err := netip.ParseAddr(info.Local)
			if err != nil || !address.Is6() || info.PrefixLen < 0 || info.PrefixLen > 128 {
				return "", "", errors.New("invalid DS-Lite IPv6 source identity")
			}
		}
		return firstUsableGlobalIPv6Links([]ipJSONLink{{AddrInfo: link.Addresses}}), "", nil
	}
	return "", "", nil
}

func (c DSLiteTunnelController) nativeTunnelReceipts() ([]dsliteTunnelReceipt, error) {
	status, err := c.endpointStatus(api.RouterAPIVersion, "Router", dsliteNativeStatusName)
	if err != nil {
		return nil, err
	}
	value, present := status["tunnels"]
	if !present {
		return nil, nil
	}
	var records []dsliteTunnelReceipt
	if !decodeAppliedDataplaneStatus(value, &records) || records == nil {
		return nil, errors.New("invalid DS-Lite native journal")
	}
	names, resources := map[string]bool{}, map[string]bool{}
	for _, record := range records {
		_, scopeOK := dsliteKernelScope(record.Scope, record.OS)
		token, err := hex.DecodeString(record.Token)
		local, localErr := netip.ParseAddr(record.Plan.Local)
		remote, remoteErr := netip.ParseAddr(record.Plan.Remote)
		inner, innerErr := netip.ParseAddr(record.Plan.InnerLocal)
		if err != nil || len(token) != 24 || record.Resource == "" || record.Plan.Name == "" || !scopeOK || record.OS != platform.OSLinux && record.OS != platform.OSFreeBSD || record.IfIndex < 0 || !record.Pending && record.IfIndex == 0 || localErr != nil || !local.Is6() || remoteErr != nil || !remote.Is6() || innerErr != nil || !inner.Is4() || record.Plan.MTU < 1280 || record.Plan.RouteMetric < 0 || names[record.Plan.Name] || resources[record.Resource] || record.OwnDefault && !record.Plan.DefaultRoute {
			return nil, fmt.Errorf("invalid DS-Lite native receipt for %q", record.Resource)
		}
		names[record.Plan.Name], resources[record.Resource] = true, true
	}
	return records, nil
}

func (c DSLiteTunnelController) saveNativeTunnel(record dsliteTunnelReceipt, remove bool) error {
	records, err := c.nativeTunnelReceipts()
	if err != nil {
		return err
	}
	next := make([]dsliteTunnelReceipt, 0, len(records)+1)
	for _, existing := range records {
		if existing.Resource != record.Resource {
			next = append(next, existing)
		}
	}
	if !remove {
		next = append(next, record)
	}
	sort.Slice(next, func(i, j int) bool { return next[i].Resource < next[j].Resource })
	return c.Store.SaveObjectStatus(api.RouterAPIVersion, "Router", dsliteNativeStatusName, map[string]any{"tunnels": next})
}

func dsliteNativeToken(alias string) string {
	token, recognized := strings.CutPrefix(alias, "routerd-dslite:")
	if !recognized {
		return ""
	}
	return token
}

func (c DSLiteTunnelController) observeNativeTunnel(ctx context.Context, name string) (dsliteTunnelObservation, error) {
	var observed dsliteTunnelObservation
	if c.currentOS() == platform.OSFreeBSD {
		list, err := c.endpointCommand(ctx, "ifconfig", "-l")
		if err != nil {
			return observed, err
		}
		if !fieldsContain(strings.Fields(string(list)), name) {
			return observed, nil
		}
		out, err := c.endpointCommand(ctx, "ifconfig", name)
		if err != nil {
			return observed, err
		}
		observed.Exists = true
		if c.NativeInterfaceIndex != nil {
			observed.Index, err = c.NativeInterfaceIndex(name)
		} else {
			var iface *net.Interface
			iface, err = net.InterfaceByName(name)
			if err == nil {
				observed.Index = iface.Index
			}
		}
		if err != nil || observed.Index < 1 {
			return observed, errors.New("DS-Lite native interface index unavailable")
		}
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			switch fields[0] {
			case name + ":":
				if len(fields) < 2 {
					return observed, errors.New("truncated FreeBSD DS-Lite interface header")
				}
				observed.Up = strings.Contains(fields[1], "<UP,") || strings.Contains(fields[1], ",UP,") || strings.Contains(fields[1], ",UP>")
				for i, field := range fields {
					if field == "mtu" && i+1 < len(fields) {
						observed.MTU, err = strconv.Atoi(fields[i+1])
					}
				}
			case "description:":
				observed.Token = dsliteNativeToken(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "description:")))
			case "groups:":
				if fieldsContain(fields[1:], "gif") {
					observed.Kind = "gif"
				}
			case "tunnel":
				if len(fields) == 5 && fields[1] == "inet6" && fields[3] == "-->" {
					observed.Local, observed.Remote = canonicalIPAddress(fields[2]), canonicalIPAddress(fields[4])
				}
			case "inet":
				if len(fields) >= 4 && fields[2] == "-->" && fields[3] == dsliteInnerRemoteIPv4 {
					observed.InnerLocal = canonicalIPAddress(fields[1])
				}
			}
		}
		if err != nil || observed.MTU < 1 {
			return observed, errors.New("incomplete FreeBSD DS-Lite native observation")
		}
		return observed, nil
	}
	if c.currentOS() != platform.OSLinux {
		return observed, errors.New("DS-Lite requires Linux or FreeBSD")
	}
	out, err := c.endpointCommand(ctx, "ip", "-j", "-d", "link", "show")
	if err != nil {
		return observed, err
	}
	var links []struct {
		Name  string   `json:"ifname"`
		Index int      `json:"ifindex"`
		Alias string   `json:"ifalias"`
		MTU   int      `json:"mtu"`
		Flags []string `json:"flags"`
		Info  struct {
			Kind string `json:"info_kind"`
			Data struct {
				Proto       string `json:"proto"`
				Local       string `json:"local"`
				Remote      string `json:"remote"`
				Link        string `json:"link"`
				IgnoreLimit bool   `json:"ip6_tnl_f_ign_encap_limit"`
				Limit       *int   `json:"encap_limit"`
			} `json:"info_data"`
		} `json:"linkinfo"`
	}
	if err := json.Unmarshal(out, &links); err != nil || links == nil {
		return observed, errors.New("invalid DS-Lite link observation")
	}
	for _, link := range links {
		if link.Name != name {
			continue
		}
		if observed.Exists || link.Index < 1 || link.MTU < 1 || link.Flags == nil {
			return observed, errors.New("incomplete DS-Lite link identity")
		}
		observed = dsliteTunnelObservation{Exists: true, Index: link.Index, MTU: link.MTU, Up: fieldsContain(link.Flags, "UP"), Token: dsliteNativeToken(link.Alias), Kind: link.Info.Kind, Proto: link.Info.Data.Proto, Local: canonicalIPAddress(link.Info.Data.Local), Remote: canonicalIPAddress(link.Info.Data.Remote), Underlay: link.Info.Data.Link}
		if link.Info.Data.IgnoreLimit {
			observed.EncapLimit = "none"
		} else if link.Info.Data.Limit != nil {
			observed.EncapLimit = strconv.Itoa(*link.Info.Data.Limit)
		}
	}
	if !observed.Exists {
		return observed, nil
	}
	out, err = c.endpointCommand(ctx, "ip", "-j", "-4", "addr", "show", "dev", name)
	if err != nil {
		return observed, err
	}
	var addresses []struct {
		Addresses []struct {
			Local, Peer string
			PrefixLen   *int `json:"prefixlen"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal(out, &addresses); err != nil || len(addresses) != 1 || addresses[0].Addresses == nil {
		return observed, errors.New("invalid DS-Lite inner address observation")
	}
	for _, address := range addresses[0].Addresses {
		peer := strings.TrimSuffix(address.Peer, "/32")
		if peer == dsliteInnerRemoteIPv4 && address.PrefixLen != nil && *address.PrefixLen == 32 {
			observed.InnerLocal = canonicalIPAddress(address.Local)
		}
	}
	return observed, nil
}

func (c DSLiteTunnelController) verifyNativeTunnel(ctx context.Context, record dsliteTunnelReceipt, observed dsliteTunnelObservation) error {
	scope, err := c.tunnelScope(ctx)
	if err != nil {
		return err
	}
	if record.OS != c.currentOS() || record.Scope != scope {
		return errors.New("DS-Lite native receipt belongs to another kernel scope")
	}
	if !observed.Exists {
		return nil
	}
	kind := "ip6tnl"
	if record.OS == platform.OSFreeBSD {
		kind = "gif"
	}
	if observed.Kind != kind || observed.Token != record.Token || record.IfIndex != 0 && record.IfIndex != observed.Index {
		return fmt.Errorf("DS-Lite interface %s is foreign or has been replaced", record.Plan.Name)
	}
	return nil
}

func (c DSLiteTunnelController) withdrawNativeTunnel(ctx context.Context, record dsliteTunnelReceipt) error {
	if c.DryRun {
		return nil
	}
	observed, err := c.observeNativeTunnel(ctx, record.Plan.Name)
	if err != nil {
		return err
	}
	if err := c.verifyNativeTunnel(ctx, record, observed); err != nil {
		return err
	}
	if observed.Exists || record.OwnDefault {
		record.Pending = true
		if err := c.saveNativeTunnel(record, false); err != nil {
			return err
		}
		observed, err = c.observeNativeTunnel(ctx, record.Plan.Name)
		if err != nil {
			return err
		}
		if err := c.verifyNativeTunnel(ctx, record, observed); err != nil {
			return err
		}
	}
	if record.OwnDefault {
		if err := c.withdrawTunnelDefault(ctx, record); err != nil {
			return err
		}
	}

	if observed.Exists {
		record.Pending = true
		if err := c.saveNativeTunnel(record, false); err != nil {
			return err
		}
		observed, err = c.observeNativeTunnel(ctx, record.Plan.Name)
		if err != nil {
			return err
		}
		if err := c.verifyNativeTunnel(ctx, record, observed); err != nil {
			return err
		}
		if observed.Exists {
			command, args := "ip", []string{"link", "delete", "dev", record.Plan.Name}
			if record.OS == platform.OSFreeBSD {
				command, args = "ifconfig", []string{record.Plan.Name, "destroy"}
			}
			if _, err := c.endpointCommand(ctx, command, args...); err != nil {
				return err
			}
		}
		observed, err = c.observeNativeTunnel(ctx, record.Plan.Name)
		if err != nil {
			return err
		}
		if observed.Exists {
			return errors.New("DS-Lite interface remains after withdrawal")
		}
	}
	if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", record.Resource, map[string]any{"phase": PhaseDisabled, "reason": "Withdrawn", "interface": record.Plan.Name, "dryRun": false}); err != nil {
		return err
	}
	return c.saveNativeTunnel(record, true)
}

func (c DSLiteTunnelController) withdrawNativeTunnelResource(ctx context.Context, name string) error {
	if c.DryRun {
		return nil
	}
	records, err := c.nativeTunnelReceipts()
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Resource == name {
			return c.withdrawNativeTunnel(ctx, record)
		}
	}
	return nil
}

// Retire tunnel effects before their source owner removes an expired /128.
func (c DSLiteTunnelController) cleanupNativeTunnels(ctx context.Context) error {
	if c.DryRun {
		return nil
	}
	records, err := c.nativeTunnelReceipts()
	if err != nil {
		return err
	}
	active := map[string]api.DSLiteTunnelSpec{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DSLiteTunnel" {
			continue
		}
		spec, err := resource.DSLiteTunnelSpec()
		if err != nil {
			return err
		}
		ready, err := resourcequery.ReadDependenciesReady(c.Store, spec.DependsOn)
		if err != nil {
			return err
		}
		if api.BoolDefault(spec.Enabled, true) && ready {
			active[resource.Metadata.Name] = spec
		}
	}
	for _, record := range records {
		scope, err := c.tunnelScope(ctx)
		if err != nil {
			return err
		}
		if record.OS != c.currentOS() {
			return errors.New("DS-Lite receipt belongs to another platform")
		}
		if scope != record.Scope {
			// Reboot destroys the old native effects. Changing only namespace or
			// jail does not: leave that receipt intact and require its old owner.
			oldKernel, oldOK := dsliteKernelScope(record.Scope, record.OS)
			newKernel, newOK := dsliteKernelScope(scope, c.currentOS())
			if !oldOK || !newOK || oldKernel == newKernel {
				return errors.New("DS-Lite receipt belongs to another namespace or jail")
			}
			if err := c.saveNativeTunnel(record, true); err != nil {
				return err
			}
			continue
		}
		spec, keep := active[record.Resource]
		if keep {
			name := firstNonEmpty(spec.TunnelName, spec.Interface, record.Resource)
			if c.currentOS() == platform.OSFreeBSD {
				name = freeBSDDSLiteRuntimeIfName(name)
			}
			keep = name == record.Plan.Name
			if spec.LocalAddressSource == "delegatedAddress" {
				source, found, err := c.delegatedEndpointSource(spec)
				if err != nil {
					return err
				}
				keep = keep && found && source.Address == record.Plan.Local
			}
		}
		if !keep {
			if err := c.withdrawNativeTunnel(ctx, record); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c DSLiteTunnelController) ensureNativeTunnel(ctx context.Context, resource string, spec api.DSLiteTunnelSpec, plan dsliteTunnelPlan) error {
	records, err := c.nativeTunnelReceipts()
	if err != nil {
		return err
	}
	var record dsliteTunnelReceipt
	for _, previous := range records {
		if previous.Resource == resource {
			if previous.Plan != plan {
				if err := c.withdrawNativeTunnel(ctx, previous); err != nil {
					return err
				}
			} else {
				record = previous
			}
		} else if previous.Plan.Name == plan.Name {
			return errors.New("DS-Lite native name belongs to another resource")
		}
	}
	observed, err := c.observeNativeTunnel(ctx, plan.Name)
	if err != nil {
		return err
	}
	if observed.Exists && record.Resource != "" {
		if err := c.verifyNativeTunnel(ctx, record, observed); err != nil {
			return err
		}
		drift := observed.Local != plan.Local || observed.Remote != plan.Remote || observed.InnerLocal != plan.InnerLocal
		if record.OS == platform.OSLinux {
			drift = observed.Local != plan.Local || observed.Remote != plan.Remote || observed.Proto != "ipip6" || observed.Underlay != plan.Underlay || observed.EncapLimit != plan.EncapLimit
		}
		if drift && (record.OS == platform.OSLinux || !record.Pending) {
			if err := c.withdrawNativeTunnel(ctx, record); err != nil {
				return err
			}
			record = dsliteTunnelReceipt{}
			observed, err = c.observeNativeTunnel(ctx, plan.Name)
			if err != nil {
				return err
			}
		}
	}
	if observed.Exists && record.Resource == "" {
		return fmt.Errorf("DS-Lite interface %s exists without an ownership receipt", plan.Name)
	}
	if record.Resource == "" {
		var token [24]byte
		if _, err := rand.Read(token[:]); err != nil {
			return err
		}
		scope, err := c.tunnelScope(ctx)
		if err != nil {
			return err
		}
		record = dsliteTunnelReceipt{Resource: resource, OS: c.currentOS(), Scope: scope, Token: hex.EncodeToString(token[:]), Pending: true, Plan: plan}
	}
	if err := c.verifyNativeTunnel(ctx, record, observed); err != nil {
		return err
	}
	record.Pending = true
	if !observed.Exists {
		record.IfIndex = 0
	}
	if err := c.saveNativeTunnel(record, false); err != nil {
		return err
	}
	// SQLite writes may wait: recheck native identity after the pending fence.
	current, err := c.observeNativeTunnel(ctx, plan.Name)
	if err != nil {
		return err
	}
	if err := c.verifyNativeTunnel(ctx, record, current); err != nil {
		return err
	}
	if !observed.Exists && current.Exists {
		return errors.New("DS-Lite creation premise changed after pending write")
	}
	observed = current
	if err := c.requireTunnelInputs(ctx, resource, spec, plan); err != nil {
		return err
	}
	if !observed.Exists {
		var createErr error
		if record.OS == platform.OSLinux {
			args := []string{"link", "add", "name", plan.Name, "alias", "routerd-dslite:" + record.Token, "type", "ip6tnl", "mode", "ipip6", "local", plan.Local, "remote", plan.Remote}
			if plan.Underlay != "" {
				args = append(args, "dev", plan.Underlay)
			}
			args = append(args, "encaplimit", plan.EncapLimit)
			_, createErr = c.endpointCommand(ctx, "ip", args...)
		} else {
			_, createErr = c.endpointCommand(ctx, "ifconfig", "gif", "create", "name", plan.Name, "description", "routerd-dslite:"+record.Token)
		}
		observed, err = c.observeNativeTunnel(ctx, plan.Name)
		if err != nil {
			return errors.Join(createErr, err)
		}
		if err := c.verifyNativeTunnel(ctx, record, observed); err != nil {
			return errors.Join(createErr, err)
		}
		if !observed.Exists {
			return errors.Join(createErr, errors.New("DS-Lite interface absent after creation"))
		}
		record.IfIndex = observed.Index
		if err := c.saveNativeTunnel(record, false); err != nil {
			return errors.Join(createErr, err)
		}
		if createErr != nil {
			return createErr
		}
	}
	if record.IfIndex == 0 {
		record.IfIndex = observed.Index
		if err := c.saveNativeTunnel(record, false); err != nil {
			return err
		}
	}
	if err := c.requireTunnelInputs(ctx, resource, spec, plan); err != nil {
		return err
	}
	if record.OS == platform.OSFreeBSD {
		if observed.Local != plan.Local || observed.Remote != plan.Remote {
			if _, err := c.endpointCommand(ctx, "ifconfig", plan.Name, "inet6", "tunnel", plan.Local, plan.Remote); err != nil {
				return err
			}
		}
		if observed.InnerLocal != plan.InnerLocal {
			if _, err := c.endpointCommand(ctx, "ifconfig", plan.Name, "inet", plan.InnerLocal, dsliteInnerRemoteIPv4, "netmask", "255.255.255.255"); err != nil {
				return err
			}
		}
		if _, err := c.endpointCommand(ctx, "ifconfig", plan.Name, "mtu", strconv.Itoa(plan.MTU), "up"); err != nil {
			return err
		}
	} else {
		if observed.Local != plan.Local || observed.Remote != plan.Remote || observed.Underlay != plan.Underlay || observed.EncapLimit != plan.EncapLimit {
			return errors.New("owned DS-Lite tunnel configuration changed externally")
		}
		if observed.InnerLocal != plan.InnerLocal {
			if _, err := c.endpointCommand(ctx, "ip", "-4", "addr", "replace", plan.InnerLocal+"/32", "peer", dsliteInnerRemoteIPv4+"/32", "dev", plan.Name); err != nil {
				return err
			}
		}
		if _, err := c.endpointCommand(ctx, "ip", "link", "set", "dev", plan.Name, "mtu", strconv.Itoa(plan.MTU), "up"); err != nil {
			return err
		}
	}
	observed, err = c.observeNativeTunnel(ctx, plan.Name)
	if err != nil {
		return err
	}
	if err := c.verifyNativeTunnel(ctx, record, observed); err != nil {
		return err
	}
	if record.OS == platform.OSLinux && (observed.Proto != "ipip6" || observed.Underlay != plan.Underlay || observed.EncapLimit != plan.EncapLimit) {
		return errors.New("DS-Lite encapsulation readback mismatch")
	}
	if !observed.Exists || !observed.Up || observed.MTU != plan.MTU || observed.Local != plan.Local || observed.Remote != plan.Remote || observed.InnerLocal != plan.InnerLocal {
		return errors.New("DS-Lite tunnel readback did not confirm applied configuration")
	}
	if plan.DefaultRoute {
		if err := c.ensureTunnelDefault(ctx, &record); err != nil {
			return err
		}
	}
	if err := c.requireTunnelInputs(ctx, resource, spec, plan); err != nil {
		return err
	}
	record.Pending = false
	return c.saveNativeTunnel(record, false)
}

func (c DSLiteTunnelController) observeTunnelDefault(ctx context.Context, record dsliteTunnelReceipt) (bool, bool, error) {
	if record.OS == platform.OSFreeBSD {
		out, err := c.endpointCommand(ctx, "route", "-n", "get", "-inet", "default")
		if err != nil {
			if freeBSDRouteNeedsAdd(out) || missingIPv4RouteDelete(err, out) {
				return false, false, nil
			}
			return false, false, err
		}
		values := map[string]string{}
		for _, line := range strings.Split(string(out), "\n") {
			key, value, found := strings.Cut(strings.TrimSpace(line), ":")
			if found {
				values[key] = strings.TrimSpace(value)
			}
		}
		if values["destination"] != "default" || values["flags"] == "" || values["interface"] == "" {
			return false, false, errors.New("invalid FreeBSD DS-Lite default route observation")
		}
		return values["gateway"] == dsliteInnerRemoteIPv4 && values["interface"] == record.Plan.Name && strings.Contains(values["flags"], "STATIC") && !strings.Contains(values["flags"], "PROTO1") && !strings.Contains(values["flags"], "PROTO2") && !strings.Contains(values["flags"], "BLACKHOLE") && !strings.Contains(values["flags"], "REJECT"), true, nil
	}
	out, err := c.endpointCommand(ctx, "ip", "-j", "-d", "-4", "route", "show", "default")
	if err != nil {
		return false, false, err
	}
	var routes []ordinaryIPv4RouteObservation
	if err := json.Unmarshal(out, &routes); err != nil || routes == nil {
		return false, false, errors.New("invalid DS-Lite default route observation")
	}
	collision := false
	for _, route := range routes {
		destination, err := ordinaryRouteDestination(route.Destination)
		if err != nil {
			return false, false, err
		}
		if destination != "0.0.0.0/0" || route.Metric != record.Plan.RouteMetric {
			continue
		}
		collision = true
		if (route.Type == "" || route.Type == "unicast") && route.PreferredSource == "" && route.Device == record.Plan.Name && route.Gateway == dsliteInnerRemoteIPv4 && (route.Protocol == "boot" || route.Protocol == "3") {
			return true, true, nil
		}
	}
	return false, collision, nil
}

func (c DSLiteTunnelController) ensureTunnelDefault(ctx context.Context, record *dsliteTunnelReceipt) error {
	matching, collision, err := c.observeTunnelDefault(ctx, *record)
	if err != nil {
		return err
	}
	if collision && (!record.OwnDefault || !matching) {
		return errors.New("foreign default route occupies DS-Lite route key")
	}
	if matching {
		return nil
	}
	record.OwnDefault = true
	if err := c.saveNativeTunnel(*record, false); err != nil {
		return err
	}
	matching, collision, err = c.observeTunnelDefault(ctx, *record)
	if err != nil {
		return err
	}
	if collision {
		return errors.New("DS-Lite default route premise changed after pending write")
	}
	var applyErr error
	if record.OS == platform.OSFreeBSD {
		_, applyErr = c.endpointCommand(ctx, "route", "-n", "add", "-inet", "default", dsliteInnerRemoteIPv4, "-ifp", record.Plan.Name)
	} else {
		_, applyErr = c.endpointCommand(ctx, "ip", "-4", "route", "add", "default", "via", dsliteInnerRemoteIPv4, "dev", record.Plan.Name, "metric", strconv.Itoa(record.Plan.RouteMetric), "proto", "boot")
	}
	matching, _, err = c.observeTunnelDefault(ctx, *record)
	if err != nil || !matching {
		return errors.Join(applyErr, err, errors.New("DS-Lite default route readback failed"))
	}
	return applyErr
}

func (c DSLiteTunnelController) withdrawTunnelDefault(ctx context.Context, record dsliteTunnelReceipt) error {
	matching, _, err := c.observeTunnelDefault(ctx, record)
	if err != nil || !matching {
		return err
	}
	if record.OS == platform.OSFreeBSD {
		_, err = c.endpointCommand(ctx, "route", "-n", "delete", "-inet", "default", dsliteInnerRemoteIPv4, "-ifp", record.Plan.Name)
	} else {
		_, err = c.endpointCommand(ctx, "ip", "-4", "route", "del", "default", "via", dsliteInnerRemoteIPv4, "dev", record.Plan.Name, "metric", strconv.Itoa(record.Plan.RouteMetric), "proto", "boot")
	}
	if err != nil {
		return err
	}
	matching, _, err = c.observeTunnelDefault(ctx, record)
	if err != nil {
		return err
	}
	if matching {
		return errors.New("DS-Lite default route remains after withdrawal")
	}
	return nil
}
