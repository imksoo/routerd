// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"github.com/imksoo/routerd/pkg/subprocessio"
)

const dsliteEndpointStatusName = "dslite-endpoints"

type dsliteEndpointReceipt struct {
	OS         platform.OS `json:"os"`
	Scope      string      `json:"scope"`
	IfIndex    int         `json:"ifIndex"`
	Name       string      `json:"name"`
	Interface  string      `json:"interface"`
	Address    string      `json:"address"`
	PDName     string      `json:"pdName"`
	PDPrefix   string      `json:"pdPrefix"`
	ValidUntil string      `json:"validUntil"`
	Pending    bool        `json:"pending"`
}

type dsliteEndpointSource struct {
	Interface, Address, PDName, PDPrefix, LANName string
	SharedLAN                                     bool
	Lease                                         routerstate.PDLease
}

func (c DSLiteTunnelController) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c DSLiteTunnelController) endpointCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	if c.CommandOutput != nil {
		return c.CommandOutput(ctx, name, args...)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, stderr := subprocessio.NewCapture(subprocessio.StdoutLimit), subprocessio.NewCapture(subprocessio.StderrLimit)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := subprocessio.Run(cmd)
	if limitErr := subprocessio.LimitError(stdout, stderr); limitErr != nil {
		return nil, errors.Join(err, limitErr)
	}
	if err != nil {
		return stderr.Bytes(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func (c DSLiteTunnelController) endpointStatus(version, kind, name string) (map[string]any, error) {
	reader, ok := c.Store.(ipv4RouteStatusReader)
	if !ok {
		return nil, fmt.Errorf("DS-Lite endpoint requires an error-aware status reader")
	}
	return reader.ReadObjectStatus(version, kind, name)
}

func (c DSLiteTunnelController) delegatedEndpointSource(spec api.DSLiteTunnelSpec) (dsliteEndpointSource, bool, error) {
	var source dsliteEndpointSource
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "IPv6DelegatedAddress" || resource.Metadata.Name != spec.LocalDelegatedAddress {
			continue
		}
		delegated, err := resource.IPv6DelegatedAddressSpec()
		if err != nil {
			return source, false, err
		}
		lease, found, err := livePDLease(c.Store, delegated.PrefixDelegation, c.now())
		if err != nil {
			return source, false, err
		}
		if !found || lease.Phase != daemonapi.ResourcePhaseBound || !lease.HasFreshTransactionEvidence(c.now()) {
			return source, false, nil
		}
		address, err := deriveIPv6AddressFromPrefix(lease.CurrentPrefix, delegated.SubnetID, firstNonEmpty(spec.LocalAddressSuffix, delegated.AddressSuffix, "::1"))
		if err != nil {
			return source, false, err
		}
		prefix, _ := netip.ParsePrefix(lease.CurrentPrefix)
		local, _ := netip.ParseAddr(address)
		if !prefix.Contains(local) {
			return source, false, fmt.Errorf("DS-Lite source %s is outside delegated prefix %s", address, lease.CurrentPrefix)
		}
		lanAddress, err := deriveIPv6AddressFromPrefix(lease.CurrentPrefix, delegated.SubnetID, firstNonEmpty(delegated.AddressSuffix, "::1"))
		if err != nil {
			return source, false, err
		}
		source = dsliteEndpointSource{Interface: interfaceName(c.Router, delegated.Interface), Address: address, PDName: delegated.PrefixDelegation, PDPrefix: lease.CurrentPrefix, LANName: resource.Metadata.Name, SharedLAN: delegated.PrefixLength == 128 && address == lanAddress, Lease: lease}
		if source.Interface == "" {
			return source, false, fmt.Errorf("DS-Lite delegated source interface is unavailable")
		}
		return source, true, nil
	}
	return source, false, nil
}

// This single owner ledger survives ordinary tunnel display/status updates and
// resource GC. Pending intent precedes a native write; exact cleanup precedes
// replacing its identity or dropping it from the ledger.
func (c DSLiteTunnelController) endpointReceipts() ([]dsliteEndpointReceipt, error) {
	status, err := c.endpointStatus(api.RouterAPIVersion, "Router", dsliteEndpointStatusName)
	if err != nil {
		return nil, err
	}
	value, present := status["endpoints"]
	if !present {
		return nil, nil
	}
	var receipts []dsliteEndpointReceipt
	if !decodeAppliedDataplaneStatus(value, &receipts) || receipts == nil {
		return nil, fmt.Errorf("invalid DS-Lite endpoint ownership array")
	}
	seen := map[string]bool{}
	for _, receipt := range receipts {
		_, scopeOK := dsliteKernelScope(receipt.Scope, receipt.OS)
		address, err := netip.ParsePrefix(receipt.Address)
		prefix, prefixErr := netip.ParsePrefix(receipt.PDPrefix)
		_, deadlineErr := time.Parse(time.RFC3339Nano, receipt.ValidUntil)
		if err != nil || !address.Addr().Is6() || address.Bits() != 128 || address.String() != receipt.Address || prefixErr != nil || !prefix.Addr().Is6() || prefix != prefix.Masked() || !prefix.Contains(address.Addr()) || receipt.Name == "" || !scopeOK || receipt.IfIndex < 1 || receipt.OS != platform.OSLinux && receipt.OS != platform.OSFreeBSD || receipt.Interface == "" || receipt.PDName == "" || (deadlineErr != nil && receipt.ValidUntil != "infinite") || seen[receipt.Name] {
			return nil, fmt.Errorf("invalid DS-Lite endpoint receipt for %q", receipt.Name)
		}
		seen[receipt.Name] = true
	}
	return receipts, nil
}

func (c DSLiteTunnelController) saveEndpointReceipts(receipts []dsliteEndpointReceipt) error {
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].Name < receipts[j].Name })
	if receipts == nil {
		receipts = []dsliteEndpointReceipt{}
	}
	return c.Store.SaveObjectStatus(api.RouterAPIVersion, "Router", dsliteEndpointStatusName, map[string]any{"endpoints": receipts})
}

func (c DSLiteTunnelController) setEndpointReceipt(receipt dsliteEndpointReceipt, remove bool) error {
	receipts, err := c.endpointReceipts()
	if err != nil {
		return err
	}
	next := make([]dsliteEndpointReceipt, 0, len(receipts)+1)
	found := false
	for _, previous := range receipts {
		if previous.Name != receipt.Name {
			next = append(next, previous)
		} else {
			found = true
		}
	}
	if remove && !found {
		return nil
	}
	if !remove {
		next = append(next, receipt)
	}
	return c.saveEndpointReceipts(next)
}

func (c DSLiteTunnelController) cleanupDelegatedEndpoints(ctx context.Context) error {
	if c.DryRun {
		return nil
	}
	receipts, err := c.endpointReceipts()
	if err != nil || len(receipts) == 0 {
		return err
	}
	wanted := map[string]dsliteEndpointSource{}
	reader, ok := c.Store.(ipv4RouteStatusReader)
	if !ok {
		return fmt.Errorf("DS-Lite dependency reader unavailable")
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DSLiteTunnel" {
			continue
		}
		spec, err := resource.DSLiteTunnelSpec()
		if err != nil {
			return err
		}
		ready, err := resourcequery.ReadDependenciesReady(reader, spec.DependsOn)
		if err != nil {
			return err
		}
		if !api.BoolDefault(spec.Enabled, true) || !ready || spec.LocalAddressSource != "delegatedAddress" {
			continue
		}
		source, active, err := c.delegatedEndpointSource(spec)
		if err != nil {
			return err
		}
		if active {
			wanted[resource.Metadata.Name] = source
		}
	}
	for _, receipt := range receipts {
		if source, exists := wanted[receipt.Name]; exists && !source.SharedLAN && source.Interface == receipt.Interface && source.Address+"/128" == receipt.Address && source.PDName == receipt.PDName && source.PDPrefix == receipt.PDPrefix {
			observed, err := c.observeEndpoint(ctx, receipt.Interface, receipt.Address)
			if err != nil {
				return err
			}
			same, err := c.endpointReceiptIdentity(receipt, observed)
			if err != nil {
				return err
			}
			if same {
				continue
			}
		}
		if err := c.withdrawEndpoint(ctx, receipt); err != nil {
			return err
		}
	}
	return nil
}

func (c DSLiteTunnelController) endpointReceiptIdentity(receipt dsliteEndpointReceipt, observed dsliteEndpointObservation) (bool, error) {
	if receipt.OS != c.currentOS() {
		return false, fmt.Errorf("DS-Lite source belongs to another platform")
	}
	if receipt.Scope != observed.Scope {
		oldKernel, oldOK := dsliteKernelScope(receipt.Scope, receipt.OS)
		newKernel, newOK := dsliteKernelScope(observed.Scope, c.currentOS())
		if oldOK && newOK && oldKernel != newKernel {
			return false, nil
		}
		return false, fmt.Errorf("DS-Lite source belongs to another namespace or jail")
	}
	if observed.IfIndex == 0 {
		return false, nil
	}
	if receipt.IfIndex != observed.IfIndex {
		return false, fmt.Errorf("DS-Lite source interface has been replaced")
	}
	return true, nil
}

func (c DSLiteTunnelController) withdrawEndpoint(ctx context.Context, receipt dsliteEndpointReceipt) error {
	if c.DryRun {
		return nil
	}
	observed, err := c.observeEndpoint(ctx, receipt.Interface, receipt.Address)
	if err != nil {
		return err
	}
	same, err := c.endpointReceiptIdentity(receipt, observed)
	if err != nil {
		return err
	}
	// A reboot destroyed the old source; its receipt grants no authority over
	// a same-address replacement in the current kernel.
	if receipt.Scope != observed.Scope {
		return c.setEndpointReceipt(receipt, true)
	}
	if err := c.withdrawNativeTunnelResource(ctx, receipt.Name); err != nil {
		return err
	}
	if same && observed.Present {
		if observed.OtherPrefix {
			return fmt.Errorf("DS-Lite owned source overlaps another native prefix")
		}
		receipt.Pending = true
		if err := c.setEndpointReceipt(receipt, false); err != nil {
			return err
		}
		current, err := c.observeEndpoint(ctx, receipt.Interface, receipt.Address)
		if err != nil {
			return err
		}
		matches, err := c.endpointReceiptIdentity(receipt, current)
		if err != nil {
			return err
		}
		if !matches && current.IfIndex != 0 {
			return fmt.Errorf("DS-Lite source identity changed during withdrawal")
		}
		if current.Present {
			if current.OtherPrefix {
				return fmt.Errorf("DS-Lite source prefix changed during withdrawal")
			}
			name, args := ipv6StaticAddressDeleteCommand(c.currentOS(), receipt.Interface, receipt.Address)
			if output, err := c.endpointCommand(ctx, name, args...); err != nil {
				return fmt.Errorf("withdraw DS-Lite endpoint %s: %w: %s", receipt.Address, err, strings.TrimSpace(string(output)))
			}
		}
		current, err = c.observeEndpoint(ctx, receipt.Interface, receipt.Address)
		if err != nil {
			return err
		}
		matches, err = c.endpointReceiptIdentity(receipt, current)
		if err != nil {
			return err
		}
		if current.Present || !matches && current.IfIndex != 0 {
			return fmt.Errorf("DS-Lite source withdrawal was not confirmed")
		}
	}
	return c.setEndpointReceipt(receipt, true)
}

type dsliteEndpointObservation struct {
	Scope                       string
	IfIndex                     int
	Present, Ready, OtherPrefix bool
}

func (c DSLiteTunnelController) observeEndpoint(ctx context.Context, ifname, address string) (dsliteEndpointObservation, error) {
	var observed dsliteEndpointObservation
	scope, err := c.tunnelScope(ctx)
	if err != nil {
		return observed, err
	}
	observed.Scope = scope
	wanted, err := netip.ParsePrefix(address)
	if err != nil {
		return observed, err
	}
	if c.currentOS() == platform.OSFreeBSD {
		output, err := c.endpointCommand(ctx, "ifconfig", ifname, "inet6")
		if err != nil {
			if strings.Contains(string(output), "does not exist") {
				return observed, nil
			}
			return observed, err
		}
		if !strings.HasPrefix(string(output), ifname+":") {
			return observed, fmt.Errorf("invalid DS-Lite source interface identity")
		}
		if c.NativeInterfaceIndex != nil {
			observed.IfIndex, err = c.NativeInterfaceIndex(ifname)
		} else {
			var iface *net.Interface
			iface, err = net.InterfaceByName(ifname)
			if err == nil {
				observed.IfIndex = iface.Index
			}
		}
		if err != nil || observed.IfIndex < 1 {
			return observed, fmt.Errorf("DS-Lite source interface index unavailable")
		}
		for _, line := range strings.Split(string(output), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 || fields[0] != "inet6" {
				continue
			}
			if len(fields) < 4 {
				return observed, fmt.Errorf("invalid FreeBSD DS-Lite address observation")
			}
			actual, err := netip.ParseAddr(strings.Split(fields[1], "%")[0])
			bits, prefixErr := strconv.Atoi(fields[3])
			if err != nil || prefixErr != nil || bits < 0 || bits > 128 || fields[2] != "prefixlen" {
				return observed, fmt.Errorf("invalid FreeBSD DS-Lite address observation")
			}
			if actual == wanted.Addr() {
				if bits == wanted.Bits() {
					observed.Present, observed.Ready = true, !fieldsContain(fields, "tentative") && !fieldsContain(fields, "duplicated")
				} else {
					observed.OtherPrefix = true
				}
			}
		}
		return observed, nil
	}
	output, err := c.endpointCommand(ctx, "ip", "-j", "-6", "addr", "show", "dev", ifname)
	if err != nil {
		if strings.Contains(string(output), "Cannot find device") || strings.Contains(string(output), "does not exist") {
			return observed, nil
		}
		return observed, fmt.Errorf("observe DS-Lite source address: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var links []struct {
		Name      string `json:"ifname"`
		Index     int    `json:"ifindex"`
		Addresses []struct {
			Family    string `json:"family"`
			Local     string `json:"local"`
			PrefixLen *int   `json:"prefixlen"`
			Tentative bool   `json:"tentative"`
			DADFailed bool   `json:"dadfailed"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal(output, &links); err != nil || links == nil {
		return observed, fmt.Errorf("invalid DS-Lite source address observation")
	}
	if len(links) > 1 {
		return observed, fmt.Errorf("DS-Lite source observation is not a single interface")
	}
	for _, link := range links {
		if link.Name != ifname || link.Index < 1 {
			return observed, fmt.Errorf("invalid DS-Lite source interface identity")
		}
		observed.IfIndex = link.Index
		if link.Addresses == nil {
			return observed, fmt.Errorf("DS-Lite source observation has no address array")
		}
		for _, actual := range link.Addresses {
			if actual.Family != "inet6" {
				continue
			}
			address, err := netip.ParseAddr(actual.Local)
			if err != nil || !address.Is6() || actual.PrefixLen == nil || *actual.PrefixLen < 0 || *actual.PrefixLen > 128 {
				return observed, fmt.Errorf("invalid DS-Lite source address identity")
			}
			if address == wanted.Addr() {
				if *actual.PrefixLen == wanted.Bits() {
					observed.Present, observed.Ready = true, !actual.Tentative && !actual.DADFailed
				} else {
					observed.OtherPrefix = true
				}
			}
		}
	}
	return observed, nil
}

func (c DSLiteTunnelController) recheckDelegatedEndpoint(ctx context.Context, spec api.DSLiteTunnelSpec, ifname, local string) (string, error) {
	source, active, err := c.delegatedEndpointSource(spec)
	if err != nil {
		return "", err
	}
	if active && source.Interface == ifname && source.Address == local {
		if _, _, err := currentPDAddressLifetimes(source.Lease, c.now(), true); err == nil {
			if source.SharedLAN {
				ready, err := c.sharedEndpointReady(ctx, source)
				if err != nil || !ready {
					return "LocalIPv6NotReady", err
				}
			} else {
				receipts, err := c.endpointReceipts()
				if err != nil {
					return "", err
				}
				for _, receipt := range receipts {
					if receipt.Interface == ifname && receipt.Address == local+"/128" && (receipt.PDName != source.PDName || receipt.PDPrefix != source.PDPrefix || receipt.ValidUntil != pdLifetimeDeadline(source.Lease.LastReplyAt, source.Lease.ValidSeconds)) {
						return "PDLeaseChanged", c.withdrawEndpoint(ctx, receipt)
					}
				}
			}
			return "", nil
		}
	}
	return "PDLeaseChanged", c.cleanupDelegatedEndpoints(ctx)
}

func (c DSLiteTunnelController) sharedEndpointReady(ctx context.Context, source dsliteEndpointSource) (bool, error) {
	status, err := c.endpointStatus(api.NetAPIVersion, "IPv6DelegatedAddress", source.LANName)
	if err != nil {
		return false, err
	}
	dryRun, known := status["dryRun"].(bool)
	if status["phase"] != "Applied" || !known || dryRun || status["ifname"] != source.Interface || status["address"] != source.Address+"/128" || status["prefixSource"] != source.PDName || status["validUntil"] != pdLifetimeDeadline(source.Lease.LastReplyAt, source.Lease.ValidSeconds) {
		return false, nil
	}
	receipt, err := delegatedReceipt(status)
	if err != nil {
		return false, err
	}
	if receipt == nil || receipt.IfName != source.Interface || receipt.PrefixDelegation != source.PDName {
		return false, nil
	}
	preferred, valid := nativePDAddressDeadlines(source.Lease, c.currentOS() == platform.OSLinux)
	if !receipt.matches(source.Address+"/128", preferred, valid) {
		return false, nil
	}
	observed, err := c.observeEndpoint(ctx, source.Interface, source.Address+"/128")
	if err != nil {
		return false, err
	}
	return receipt.Scope == observed.Scope && receipt.IfIndex == observed.IfIndex && observed.Present && observed.Ready && !observed.OtherPrefix, nil
}

func (c DSLiteTunnelController) ensureDelegatedEndpoint(ctx context.Context, name string, spec api.DSLiteTunnelSpec, ifname, local string) (string, error) {
	source, active, err := c.delegatedEndpointSource(spec)
	if err != nil || !active || source.Interface != ifname || source.Address != local {
		if err != nil {
			return "", err
		}
		return "PDLeaseChanged", c.cleanupDelegatedEndpoints(ctx)
	}
	receipt := dsliteEndpointReceipt{Name: name, Interface: ifname, Address: local + "/128", PDName: source.PDName, PDPrefix: source.PDPrefix, ValidUntil: pdLifetimeDeadline(source.Lease.LastReplyAt, source.Lease.ValidSeconds), Pending: true}
	if !source.SharedLAN {
		receipts, err := c.endpointReceipts()
		if err != nil {
			return "", err
		}
		observed, err := c.observeEndpoint(ctx, ifname, receipt.Address)
		if err != nil {
			return "", err
		}
		if observed.IfIndex == 0 {
			return "LocalIPv6NotReady", nil
		}
		receipt.OS, receipt.Scope, receipt.IfIndex = c.currentOS(), observed.Scope, observed.IfIndex
		owned := false
		for _, previous := range receipts {
			if previous.Name == name {
				owned = previous.Interface == ifname && previous.Address == receipt.Address && previous.PDName == source.PDName && previous.PDPrefix == source.PDPrefix
				if !owned {
					return "", fmt.Errorf("previous DS-Lite source has not been withdrawn")
				}
				same, err := c.endpointReceiptIdentity(previous, observed)
				if err != nil {
					return "", err
				}
				if !same {
					if err := c.withdrawEndpoint(ctx, previous); err != nil {
						return "", err
					}
					owned = false
				}

			}
		}
		if observed.OtherPrefix || (observed.Present && !owned) {
			return "", fmt.Errorf("foreign IPv6 address occupies DS-Lite source %s", receipt.Address)
		}
		if err := c.setEndpointReceipt(receipt, false); err != nil {
			return "", err
		}
		fresh, active, err := c.delegatedEndpointSource(spec)
		if err != nil {
			return "", err
		}
		if !active || fresh.Interface != ifname || fresh.Address != local || fresh.PDName != receipt.PDName || fresh.PDPrefix != receipt.PDPrefix {
			return "PDLeaseChanged", c.cleanupDelegatedEndpoints(ctx)
		}

		current, err := c.observeEndpoint(ctx, ifname, receipt.Address)
		if err != nil {
			return "", err
		}
		same, err := c.endpointReceiptIdentity(receipt, current)
		if err != nil || !same || current.OtherPrefix || !observed.Present && current.Present {
			if !owned {
				if saveErr := c.setEndpointReceipt(receipt, true); saveErr != nil {
					return "", errors.Join(err, saveErr)
				}
			}
			return "", errors.Join(err, fmt.Errorf("DS-Lite source acquisition premise changed after pending write"))
		}
		// Native observation may wait. Resolve the canonical lease and its
		// remaining lifetimes after that wait, immediately before the write.
		fresh, active, err = c.delegatedEndpointSource(spec)
		if err != nil {
			return "", err
		}
		if !active || fresh.Interface != ifname || fresh.Address != local || fresh.PDName != receipt.PDName || fresh.PDPrefix != receipt.PDPrefix || pdLifetimeDeadline(fresh.Lease.LastReplyAt, fresh.Lease.ValidSeconds) != receipt.ValidUntil {
			return "PDLeaseChanged", c.withdrawEndpoint(ctx, receipt)
		}
		preferred, valid, err := currentPDAddressLifetimes(fresh.Lease, c.now(), true)
		if err != nil {
			return "PDLeaseStale", c.withdrawEndpoint(ctx, receipt)
		}
		command, args := ipv6DelegatedAddressApplyCommand(c.currentOS(), ifname, receipt.Address, preferred, valid)
		if output, err := c.endpointCommand(ctx, command, args...); err != nil {
			return "", fmt.Errorf("apply DS-Lite source: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	var readErr error
	ready := waitForIPv6AddressReady(ctx, ifname, receipt.Address, 3*time.Second, func(ctx context.Context, ifname, address string) bool {
		if source.SharedLAN {
			ready, err := c.sharedEndpointReady(ctx, source)
			if err != nil {
				readErr = err
				return true // Stop waiting and report the failed observation.
			}
			if !ready {
				return false
			}
		}
		observed, err := c.observeEndpoint(ctx, ifname, address)
		readErr = err
		return (observed.Ready && !observed.OtherPrefix) || err != nil
	})
	if readErr != nil {
		return "", readErr
	}
	if pending, err := c.recheckDelegatedEndpoint(ctx, spec, ifname, local); pending != "" || err != nil {
		return pending, err
	}
	if !ready {
		return "LocalIPv6NotReady", nil
	}
	if source.SharedLAN {
		// LAN is now the observed owner; do not duplicate its address write or
		// carry a second withdrawal authority for the same native /128.
		return "", c.setEndpointReceipt(receipt, true)
	}
	final, err := c.observeEndpoint(ctx, ifname, receipt.Address)
	if err != nil {
		return "", err
	}
	same, err := c.endpointReceiptIdentity(receipt, final)
	if err != nil {
		return "", err
	}
	if !same || !final.Present || !final.Ready || final.OtherPrefix {
		return "", fmt.Errorf("DS-Lite source identity changed while waiting for readiness")
	}
	receipt.Pending = false
	return "", c.setEndpointReceipt(receipt, false)
}
