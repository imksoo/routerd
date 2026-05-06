//go:build freebsd

package healthcheck

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

func bindDialerToDevice(dialer *net.Dialer, ifname, network, address, addressFamily string, hasSourceAddress bool) error {
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return err
	}
	if hasSourceAddress {
		return nil
	}
	family := socketAddressFamily(network, address, addressFamily)
	source, err := interfaceSourceAddress(iface, family)
	if err != nil {
		return err
	}
	switch {
	case strings.HasPrefix(network, "udp"):
		dialer.LocalAddr = &net.UDPAddr{IP: source, Zone: zoneForIP(source, iface.Name)}
	case strings.HasPrefix(network, "ip"):
		dialer.LocalAddr = &net.IPAddr{IP: source, Zone: zoneForIP(source, iface.Name)}
	default:
		dialer.LocalAddr = &net.TCPAddr{IP: source, Zone: zoneForIP(source, iface.Name)}
	}
	return nil
}

func socketAddressFamily(network, address, addressFamily string) int {
	switch strings.ToLower(strings.TrimSpace(addressFamily)) {
	case "ipv6", "ip6", "6":
		return 6
	case "ipv4", "ip4", "4":
		return 4
	}
	network = strings.ToLower(network)
	if strings.Contains(network, "6") {
		return 6
	}
	if strings.Contains(network, "4") {
		return 4
	}
	host := address
	if splitHost, _, err := net.SplitHostPort(address); err == nil {
		host = splitHost
	}
	host = strings.Trim(host, "[]")
	if addr, err := netip.ParseAddr(host); err == nil && addr.Is6() {
		return 6
	}
	return 4
}

func interfaceSourceAddress(iface *net.Interface, family int) (net.IP, error) {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, err
	}
	var fallback net.IP
	for _, raw := range addrs {
		ip, ok := addressIP(raw)
		if !ok || !matchesFamily(ip, family) {
			continue
		}
		if fallback == nil {
			fallback = ip
		}
		if !ip.IsLinkLocalUnicast() && !ip.IsLoopback() {
			return ip, nil
		}
	}
	if fallback != nil {
		return fallback, nil
	}
	return nil, fmt.Errorf("interface %q has no IPv%d address for sourceInterface", iface.Name, family)
}

func addressIP(addr net.Addr) (net.IP, bool) {
	switch value := addr.(type) {
	case *net.IPNet:
		return value.IP, true
	case *net.IPAddr:
		return value.IP, true
	default:
		return nil, false
	}
}

func matchesFamily(ip net.IP, family int) bool {
	if family == 6 {
		return ip.To4() == nil
	}
	return ip.To4() != nil
}

func zoneForIP(ip net.IP, ifname string) string {
	if ip.To4() == nil && ip.IsLinkLocalUnicast() {
		return ifname
	}
	return ""
}
