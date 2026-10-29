// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

type LinkController struct {
	Router        *api.Router
	Store         Store
	Command       func(context.Context, string, ...string) error
	Lookup        func(string) (*net.Interface, error)
	ReadAddresses func(*net.Interface) ([]net.Addr, error)
}

func (c LinkController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return errors.New("link reconciliation requires current Router and Store")
	}
	for _, resource := range c.Router.Spec.Resources {
		if err := ctx.Err(); err != nil {
			return err
		}
		if resource.Kind != "Interface" {
			continue
		}
		spec, err := resource.InterfaceSpec()
		if err != nil {
			return err
		}
		status := map[string]any{"phase": "Down", "ifname": spec.IfName, "managed": spec.Managed}
		if spec.IfName == "" {
			status["reason"] = "IfNameMissing"
		} else {
			ifi, err := c.lookupInterface(spec.IfName)
			if err != nil {
				return c.failLink(resource.Metadata.Name, "ObservationFailed", err)
			}
			if ifi == nil {
				status["reason"] = "InterfaceNotFound"
			} else {
				if shouldEnsureInterfaceAdminUp(spec, ifi) {
					if err := c.ensureAdminUp(ctx, spec.IfName); err != nil {
						return c.failLink(resource.Metadata.Name, "AdminUpFailed", err)
					}
					ifi, err = c.lookupInterface(spec.IfName)
					if err != nil {
						return c.failLink(resource.Metadata.Name, "ObservationFailed", err)
					}
					if ifi == nil || ifi.Flags&net.FlagUp == 0 {
						return c.failLink(resource.Metadata.Name, "AdminUpUnconfirmed", errors.New("native interface is not up after admin-up command"))
					}
				}
				read := c.ReadAddresses
				if read == nil {
					read = (*net.Interface).Addrs
				}
				addresses, err := read(ifi)
				if err != nil {
					return c.failLink(resource.Metadata.Name, "AddressObservationFailed", err)
				}
				all, ipv4, ipv6, err := interfaceStatusAddresses(addresses)
				if err != nil {
					return c.failLink(resource.Metadata.Name, "AddressObservationFailed", err)
				}
				status["index"], status["flags"] = ifi.Index, ifi.Flags.String()
				status["addresses"], status["ipv4Addresses"], status["ipv6Addresses"] = all, ipv4, ipv6
				if len(ipv4) > 0 {
					status["primaryIPv4"] = ipv4[0]
				}
				if len(ipv6) > 0 {
					status["primaryIPv6"] = ipv6[0]
				}
				if ifi.Flags&net.FlagUp != 0 {
					status["phase"] = "Up"
				}
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "Interface", resource.Metadata.Name, status); err != nil {
			return err
		}
	}
	return nil
}

func (c LinkController) failLink(name, reason string, cause error) error {
	previous, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "Interface", name)
	if err != nil {
		return errors.Join(cause, err)
	}
	status := copyStatusMap(previous)
	status["phase"], status["reason"], status["error"] = "Error", reason, cause.Error()
	return errors.Join(fmt.Errorf("Interface/%s: %w", name, cause), c.Store.SaveObjectStatus(api.NetAPIVersion, "Interface", name, status))
}

// A successful complete enumeration distinguishes missing from an unreadable
// native interface. Unknown observations never become InterfaceNotFound.
func (c LinkController) lookupInterface(ifname string) (*net.Interface, error) {
	if c.Lookup != nil {
		return c.Lookup(ifname)
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, iface := range interfaces {
		if iface.Name == ifname {
			return &iface, nil
		}
	}
	return nil, nil
}

func linkAdminUpCommand(osName platform.OS, ifname string) (string, []string, error) {
	switch osName {
	case platform.OSLinux:
		return "ip", []string{"link", "set", "dev", ifname, "up"}, nil
	case platform.OSFreeBSD:
		return "ifconfig", []string{ifname, "up"}, nil
	default:
		return "", nil, fmt.Errorf("admin-up is unsupported on %s", osName)
	}
}

func (c LinkController) ensureAdminUp(ctx context.Context, ifname string) error {
	name, args, err := linkAdminUpCommand(platform.CurrentOS(), ifname)
	if err != nil {
		return err
	}
	command := c.Command
	if command == nil {
		command = runCommandContext
	}
	return command(ctx, name, args...)
}

func shouldEnsureInterfaceAdminUp(spec api.InterfaceSpec, ifi *net.Interface) bool {
	return ifi != nil && ifi.Flags&net.FlagUp == 0 && spec.Managed && spec.Owner != "external" && spec.AdminUp
}

func interfaceStatusAddresses(addresses []net.Addr) ([]string, []string, []string, error) {
	all, ipv4, ipv6 := []string{}, []string{}, []string{}
	for _, address := range addresses {
		prefix, err := netip.ParsePrefix(address.String())
		if err != nil {
			return nil, nil, nil, fmt.Errorf("invalid native interface address %q: %w", address, err)
		}
		if prefix.Addr().IsLinkLocalUnicast() {
			continue
		}
		all = append(all, prefix.String())
		if prefix.Addr().Is4() {
			ipv4 = append(ipv4, prefix.String())
		} else {
			ipv6 = append(ipv6, prefix.String())
		}
	}
	sort.Strings(all)
	sort.Strings(ipv4)
	sort.Strings(ipv6)
	return all, ipv4, ipv6, nil
}
