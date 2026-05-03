package dhcpv4server

import (
	"fmt"
	"strings"

	"routerd/pkg/api"
)

type Config struct {
	Name         string
	IfName       string
	AddressPool  api.DHCPAddressPoolSpec
	Gateway      string
	DNSServers   []string
	NTPServers   []string
	Domain       string
	Options      []api.DHCPv4OptionSpec
	Reservations []api.DHCPv4ReservationSpec
}

func RenderDnsmasqLines(cfg Config) []string {
	tag := sanitizeTag(cfg.Name)
	leaseTime := cfg.AddressPool.LeaseTime
	if leaseTime == "" {
		leaseTime = "12h"
	}
	lines := []string{
		"interface=" + cfg.IfName,
		fmt.Sprintf("dhcp-range=set:%s,%s,%s,%s", tag, cfg.AddressPool.Start, cfg.AddressPool.End, leaseTime),
	}
	if cfg.Gateway != "" {
		lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:router,%s", tag, cfg.Gateway))
	}
	if len(cfg.DNSServers) > 0 {
		lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:dns-server,%s", tag, strings.Join(cfg.DNSServers, ",")))
	}
	if len(cfg.NTPServers) > 0 {
		lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:ntp-server,%s", tag, strings.Join(cfg.NTPServers, ",")))
	}
	if cfg.Domain != "" {
		lines = append(lines, fmt.Sprintf("dhcp-option=tag:%s,option:domain-name,%s", tag, cfg.Domain))
	}
	for _, option := range cfg.Options {
		lines = append(lines, "dhcp-option=tag:"+tag+","+optionKey(option)+","+option.Value)
	}
	for _, reservation := range cfg.Reservations {
		lines = append(lines, "dhcp-host="+reservationLine(reservation))
	}
	return lines
}

func optionKey(option api.DHCPv4OptionSpec) string {
	if option.Name != "" {
		return "option:" + option.Name
	}
	return fmt.Sprintf("%d", option.Code)
}

func reservationLine(spec api.DHCPv4ReservationSpec) string {
	parts := []string{strings.ToLower(spec.MACAddress)}
	if spec.Hostname != "" {
		parts = append(parts, spec.Hostname)
	}
	parts = append(parts, spec.IPAddress)
	return strings.Join(parts, ",")
}

func sanitizeTag(value string) string {
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, ".", "-")
	return value
}
