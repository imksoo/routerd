// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dnsresolver"
	"github.com/imksoo/routerd/pkg/healthcheck"
	"github.com/imksoo/routerd/pkg/platform"
)

func validIAID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		_, err := strconv.ParseUint(value[2:], 16, 32)
		return err == nil
	}
	if len(value) == 8 && validHex(value) {
		_, err := strconv.ParseUint(value, 16, 32)
		return err == nil
	}
	_, err := strconv.ParseUint(value, 10, 32)
	return err == nil
}

func validHex(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range strings.ToLower(value) {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func validEventRuleCorrelation(value string) bool {
	switch value {
	case "resource.name", "resource.kind", "resource.apiVersion", "daemon.instance", "daemon.kind":
		return true
	default:
		return strings.HasPrefix(value, "attributes.") && strings.TrimPrefix(value, "attributes.") != ""
	}
}

func validateIPv4AddressPair(start, end string) error {
	startAddr, err := netip.ParseAddr(start)
	if err != nil || !startAddr.Is4() {
		return fmt.Errorf("start must be an IPv4 address")
	}
	endAddr, err := netip.ParseAddr(end)
	if err != nil || !endAddr.Is4() {
		return fmt.Errorf("end must be an IPv4 address")
	}
	if startAddr.Compare(endAddr) > 0 {
		return fmt.Errorf("start must be less than or equal to end")
	}
	return nil
}

func validateIPv6AddressPair(start, end string) error {
	startAddr, err := netip.ParseAddr(start)
	if err != nil || !startAddr.Is6() {
		return fmt.Errorf("start must be an IPv6 address")
	}
	endAddr, err := netip.ParseAddr(end)
	if err != nil || !endAddr.Is6() {
		return fmt.Errorf("end must be an IPv6 address")
	}
	if startAddr.Compare(endAddr) > 0 {
		return fmt.Errorf("start must be less than or equal to end")
	}
	return nil
}

func validateDHCPv4Option(option api.DHCPv4OptionSpec) error {
	if option.Code == 0 && option.Name == "" {
		return fmt.Errorf("code or name is required")
	}
	if option.Code != 0 && option.Name != "" {
		return fmt.Errorf("code and name are mutually exclusive")
	}
	if option.Value == "" {
		return fmt.Errorf("value is required")
	}
	if strings.ContainsAny(option.Name, " \t\n,") || strings.ContainsAny(option.Value, "\n\r") {
		return fmt.Errorf("contains invalid whitespace or newline")
	}
	return nil
}

func validateDomainValue(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("must not be empty")
	}
	if strings.ContainsAny(value, " \t\n\r/,") {
		return fmt.Errorf("must be a single DNS domain")
	}
	return nil
}

func validateFQDN(value string) error {
	value = strings.Trim(strings.TrimSpace(value), ".")
	if value == "" {
		return fmt.Errorf("must not be empty")
	}
	if strings.ContainsAny(value, " \t\n\r/,") {
		return fmt.Errorf("must be a single DNS name")
	}
	if !strings.Contains(value, ".") {
		return fmt.Errorf("must be a fully qualified DNS name")
	}
	if len(value) > 253 {
		return fmt.Errorf("must be 253 bytes or shorter")
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 {
			return fmt.Errorf("must contain non-empty labels of 63 bytes or less")
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("labels must not start or end with '-'")
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return fmt.Errorf("labels must contain only letters, digits, or '-'")
		}
	}
	return nil
}

func dnsHostnameCovered(hostname string, zones map[string]string) (string, bool) {
	hostname = strings.Trim(strings.ToLower(strings.TrimSpace(hostname)), ".")
	bestName := ""
	bestLen := -1
	for name, zone := range zones {
		zone = strings.Trim(strings.ToLower(strings.TrimSpace(zone)), ".")
		if zone == "" {
			continue
		}
		if hostname == zone || strings.HasSuffix(hostname, "."+zone) {
			if len(zone) > bestLen {
				bestName = name
				bestLen = len(zone)
			}
		}
	}
	return bestName, bestName != ""
}

func validateDNSZoneDomainSource(source api.StatusValueSourceSpec) error {
	resource := strings.TrimSpace(source.Resource)
	field := strings.TrimSpace(source.Field)
	if resource == "" || field == "" {
		return fmt.Errorf("resource and field are required")
	}
	kind, name, ok := strings.Cut(resource, "/")
	if !ok || kind != "DNSZone" || name == "" {
		return fmt.Errorf("resource must reference DNSZone/<name>")
	}
	if field != "zone" {
		return fmt.Errorf("field must be zone")
	}
	return nil
}

func interfaceRef(res api.Resource) (string, error) {
	switch res.Kind {
	case "IPv4StaticAddress":
		spec, err := res.IPv4StaticAddressSpec()
		return spec.Interface, err
	case "VirtualAddress":
		spec, err := res.VirtualAddressSpec()
		return spec.Interface, err
	case "DHCPv4Client":
		spec, err := res.DHCPv4ClientSpec()
		return spec.Interface, err
	case "IPv4StaticRoute":
		spec, err := res.IPv4StaticRouteSpec()
		return spec.Interface, err
	case "IPv6StaticRoute":
		spec, err := res.IPv6StaticRouteSpec()
		return spec.Interface, err
	case "DHCPv4Server":
		spec, err := res.DHCPv4ServerSpec()
		return spec.Interface, err
	case "DHCPv6Server":
		return "", nil
	case "DHCPv6Address":
		spec, err := res.DHCPv6AddressSpec()
		return spec.Interface, err
	case "IPv6RAAddress":
		spec, err := res.IPv6RAAddressSpec()
		return spec.Interface, err
	case "DHCPv6PrefixDelegation":
		spec, err := res.DHCPv6PrefixDelegationSpec()
		return spec.Interface, err
	case "IPv6DelegatedAddress":
		spec, err := res.IPv6DelegatedAddressSpec()
		return spec.Interface, err
	case "DSLiteTunnel":
		spec, err := res.DSLiteTunnelSpec()
		return spec.Interface, err
	case "PPPoESession":
		spec, err := res.PPPoESessionSpec()
		return spec.Interface, err
	default:
		return "", fmt.Errorf("%s has no interface reference", res.ID())
	}
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func validateIngressListen(resourceID, path string, listen api.IngressListenSpec) error {
	if strings.TrimSpace(listen.Interface) == "" {
		return fmt.Errorf("%s %s.interface is required", resourceID, path)
	}
	if strings.TrimSpace(listen.Address) != "" && strings.TrimSpace(listen.AddressFrom.Resource) != "" {
		return fmt.Errorf("%s %s.address and %s.addressFrom are mutually exclusive", resourceID, path, path)
	}
	if strings.TrimSpace(listen.Address) != "" {
		addr, err := netip.ParseAddr(listen.Address)
		if err != nil || !addr.Is4() {
			return fmt.Errorf("%s %s.address must be an IPv4 address", resourceID, path)
		}
	}
	if err := validateIngressAddressSource(resourceID, path+".addressFrom", listen.AddressFrom); err != nil {
		return err
	}
	switch listen.Protocol {
	case "tcp", "udp":
	default:
		return fmt.Errorf("%s %s.protocol must be tcp or udp", resourceID, path)
	}
	if listen.Port < 1 || listen.Port > 65535 {
		return fmt.Errorf("%s %s.port must be within 1-65535", resourceID, path)
	}
	return nil
}

func validateVirtualAddressResource(res api.Resource, targetOS platform.OS) error {
	if res.APIVersion != api.NetAPIVersion {
		return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
	}
	spec, err := res.VirtualAddressSpec()
	if err != nil {
		return err
	}
	switch spec.Family {
	case "ipv4", "ipv6":
	default:
		return fmt.Errorf("%s spec.family must be ipv4 or ipv6", res.ID())
	}
	if strings.TrimSpace(spec.Interface) == "" {
		return fmt.Errorf("%s spec.interface is required", res.ID())
	}
	if strings.TrimSpace(spec.Address) == "" && strings.TrimSpace(spec.AddressFrom.Resource) == "" {
		return fmt.Errorf("%s spec.address or spec.addressFrom is required", res.ID())
	}
	if strings.TrimSpace(spec.Address) != "" && strings.TrimSpace(spec.AddressFrom.Resource) != "" {
		return fmt.Errorf("%s spec.address and spec.addressFrom are mutually exclusive", res.ID())
	}
	if strings.TrimSpace(spec.Address) != "" {
		prefix, err := netip.ParsePrefix(spec.Address)
		if err != nil {
			return fmt.Errorf("%s spec.address must be an %s prefix", res.ID(), spec.Family)
		}
		if spec.Family == "ipv4" && !prefix.Addr().Is4() {
			return fmt.Errorf("%s spec.address must be an IPv4 prefix", res.ID())
		}
		if spec.Family == "ipv6" && !prefix.Addr().Is6() {
			return fmt.Errorf("%s spec.address must be an IPv6 prefix", res.ID())
		}
		wantBits := 32
		familyLabel := "IPv4"
		if spec.Family == "ipv6" {
			wantBits = 128
			familyLabel = "IPv6"
		}
		if prefix.Bits() != wantBits {
			return fmt.Errorf("%s spec.address must be an %s /%d prefix", res.ID(), familyLabel, wantBits)
		}
	}
	if err := validateIngressAddressSource(res.ID(), "spec.addressFrom", spec.AddressFrom); err != nil {
		return err
	}
	if strings.TrimSpace(spec.Hostname) != "" {
		if err := validateFQDN(spec.Hostname); err != nil {
			return fmt.Errorf("%s spec.hostname is invalid: %w", res.ID(), err)
		}
	}
	switch defaultString(spec.Mode, "static") {
	case "static":
	case "vrrp":
		if spec.VRRP.VirtualRouterID < 1 || spec.VRRP.VirtualRouterID > 255 {
			return fmt.Errorf("%s spec.vrrp.virtualRouterID must be within 1-255", res.ID())
		}
		if targetOS != platform.OSFreeBSD && len(spec.VRRP.Peers) == 0 {
			return fmt.Errorf("%s spec.vrrp.peers is required for unicast VRRP", res.ID())
		}
		if spec.VRRP.Priority != 0 && (spec.VRRP.Priority < 1 || spec.VRRP.Priority > 254) {
			return fmt.Errorf("%s spec.vrrp.priority must be within 1-254", res.ID())
		}
		if spec.VRRP.AdvertInterval != "" || spec.VRRP.PreemptDelay != "" {
			return fmt.Errorf("%s spec.vrrp.advertInterval and spec.vrrp.preemptDelay are not supported; routerd derives VRRP/CARP timing from profile defaults", res.ID())
		}
		for i, peer := range spec.VRRP.Peers {
			if err := validateAddressOrHostname(peer); err != nil {
				return fmt.Errorf("%s spec.vrrp.peers[%d] must be a single peer address or hostname", res.ID(), i)
			}
		}
		if err := validateSecretValueSource(res.ID(), "spec.vrrp.authentication", spec.VRRP.Authentication, "spec.vrrp.authenticationFrom", spec.VRRP.AuthenticationFrom); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%s spec.mode must be static or vrrp", res.ID())
	}
	for i, track := range spec.Track {
		if err := validateSourceResourceRef(track.Resource); err != nil {
			return fmt.Errorf("%s spec.track[%d].resource %w", res.ID(), i, err)
		}
		if track.UnhealthyPenalty < 0 || track.UnhealthyPenalty > 254 {
			return fmt.Errorf("%s spec.track[%d].unhealthyPenalty must be within 0-254", res.ID(), i)
		}
		if track.ConfirmConsecutiveUnhealthy < 0 || track.ConfirmConsecutiveUnhealthy > 255 {
			return fmt.Errorf("%s spec.track[%d].confirmConsecutiveUnhealthy must be non-negative and within 1-255 when set", res.ID(), i)
		}
		if track.ConfirmConsecutiveHealthy < 0 || track.ConfirmConsecutiveHealthy > 255 {
			return fmt.Errorf("%s spec.track[%d].confirmConsecutiveHealthy must be non-negative and within 1-255 when set", res.ID(), i)
		}
	}
	return nil
}

func validateNAT44SourceNATShape(resourceID string, spec api.NAT44RuleSpec) error {
	if spec.Type != "" || spec.EgressInterface != "" || spec.EgressPolicyRef != "" || len(spec.SourceRanges) > 0 || len(spec.DestinationCIDRs) > 0 || len(spec.DestinationSetRefs) > 0 || len(spec.ExcludeDestinationCIDRs) > 0 || len(spec.ExcludeDestinationSetRefs) > 0 || spec.SNATAddress != "" || spec.SNATAddressFrom.Resource != "" {
		return fmt.Errorf("%s NAT44Rule must not mix outboundInterface/sourceCIDRs/translation with type/egressInterface/sourceRanges fields", resourceID)
	}
	if spec.OutboundInterface == "" {
		return fmt.Errorf("%s spec.outboundInterface is required when using source NAT fields on NAT44Rule", resourceID)
	}
	if len(spec.SourceCIDRs) == 0 {
		return fmt.Errorf("%s spec.sourceCIDRs is required when using source NAT fields on NAT44Rule", resourceID)
	}
	for _, cidr := range spec.SourceCIDRs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil || !prefix.Addr().Is4() {
			return fmt.Errorf("%s spec.sourceCIDRs entries must be IPv4 prefixes", resourceID)
		}
	}
	switch spec.Translation.Type {
	case "interfaceAddress":
	case "address":
		addr, err := netip.ParseAddr(spec.Translation.Address)
		if err != nil || !addr.Is4() {
			return fmt.Errorf("%s spec.translation.address must be an IPv4 address", resourceID)
		}
	case "pool":
		if len(spec.Translation.Addresses) == 0 {
			return fmt.Errorf("%s spec.translation.addresses is required when translation.type is pool", resourceID)
		}
		for _, value := range spec.Translation.Addresses {
			addr, err := netip.ParseAddr(value)
			if err != nil || !addr.Is4() {
				return fmt.Errorf("%s spec.translation.addresses entries must be IPv4 addresses", resourceID)
			}
		}
	default:
		return fmt.Errorf("%s spec.translation.type must be interfaceAddress, address, or pool", resourceID)
	}
	portMappingType := defaultString(spec.Translation.PortMapping.Type, "auto")
	switch portMappingType {
	case "auto", "preserve":
		if spec.Translation.PortMapping.Start != 0 || spec.Translation.PortMapping.End != 0 {
			return fmt.Errorf("%s spec.translation.portMapping start/end are only valid when type is range", resourceID)
		}
	case "range":
		start := spec.Translation.PortMapping.Start
		end := spec.Translation.PortMapping.End
		if start < 1 || start > 65535 || end < 1 || end > 65535 || start > end {
			return fmt.Errorf("%s spec.translation.portMapping range must be within 1-65535 and start must be <= end", resourceID)
		}
	default:
		return fmt.Errorf("%s spec.translation.portMapping.type must be auto, preserve, or range", resourceID)
	}
	return nil
}

func validateIngressTarget(resourceID, path, address string, addressFrom api.StatusValueSourceSpec, port int, allowHostname bool) error {
	if strings.TrimSpace(address) == "" && strings.TrimSpace(addressFrom.Resource) == "" {
		return fmt.Errorf("%s %s.address or %s.addressFrom is required", resourceID, path, path)
	}
	if strings.TrimSpace(address) != "" && strings.TrimSpace(addressFrom.Resource) != "" {
		return fmt.Errorf("%s %s.address and %s.addressFrom are mutually exclusive", resourceID, path, path)
	}
	if strings.TrimSpace(address) != "" {
		addr, err := netip.ParseAddr(address)
		if err != nil || !addr.Is4() {
			if !allowHostname {
				return fmt.Errorf("%s %s.address must be an IPv4 address", resourceID, path)
			}
			if err := validateAddressOrHostname(address); err != nil {
				return fmt.Errorf("%s %s.address %w", resourceID, path, err)
			}
		}
	}
	if err := validateIngressAddressSource(resourceID, path+".addressFrom", addressFrom); err != nil {
		return err
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s %s.port must be within 1-65535", resourceID, path)
	}
	return nil
}

func validateIngressHairpin(resourceID, path string, listen api.IngressListenSpec, hairpin api.IngressHairpinSpec) error {
	mode := strings.TrimSpace(hairpin.Mode)
	switch mode {
	case "", "auto", "manual", "off":
	default:
		return fmt.Errorf("%s %s.mode must be auto, manual, or off", resourceID, path)
	}
	if mode == "off" {
		return nil
	}
	if !hairpin.Enabled && mode != "auto" {
		if len(hairpin.Interfaces) > 0 {
			return fmt.Errorf("%s %s.enabled must be true when interfaces are set", resourceID, path)
		}
		return nil
	}
	if strings.TrimSpace(listen.Address) == "" && strings.TrimSpace(listen.AddressFrom.Resource) == "" {
		return fmt.Errorf("%s %s requires spec.listen.address or spec.listen.addressFrom", resourceID, path)
	}
	if hairpin.Enabled && mode != "auto" && len(hairpin.Interfaces) == 0 {
		return fmt.Errorf("%s %s.interfaces is required when enabled is true", resourceID, path)
	}
	for i, name := range hairpin.Interfaces {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("%s %s.interfaces[%d] must not be empty", resourceID, path, i)
		}
	}
	return nil
}

func validateIngressInterfaceRefs(resourceID string, listen api.IngressListenSpec, hairpin api.IngressHairpinSpec, interfaces map[string]bool) error {
	if !interfaces[listen.Interface] {
		return fmt.Errorf("%s spec.listen.interface references missing Interface %q", resourceID, listen.Interface)
	}
	if !hairpin.Enabled && strings.TrimSpace(hairpin.Mode) != "auto" {
		return nil
	}
	seen := map[string]bool{}
	for i, name := range hairpin.Interfaces {
		if !interfaces[name] {
			return fmt.Errorf("%s spec.hairpin.interfaces[%d] references missing Interface %q", resourceID, i, name)
		}
		if seen[name] {
			return fmt.Errorf("%s spec.hairpin.interfaces[%d] duplicates Interface %q", resourceID, i, name)
		}
		seen[name] = true
	}
	return nil
}

func splitResourceRef(ref string) (string, string) {
	ref = strings.TrimSpace(ref)
	if kind, name, ok := strings.Cut(ref, "/"); ok {
		return kind, name
	}
	return "IPAddressSet", ref
}

func splitKindNameRef(ref, defaultKind string) (string, string) {
	ref = strings.TrimSpace(ref)
	if kind, name, ok := strings.Cut(ref, "/"); ok {
		return kind, strings.TrimSpace(name)
	}
	return defaultKind, ref
}

func validateLogSinkSpec(resourceID string, spec api.LogSinkSpec) error {
	switch spec.Type {
	case "syslog":
		switch spec.Syslog.Network {
		case "", "unix", "unixgram", "tcp", "udp":
		default:
			return fmt.Errorf("%s spec.syslog.network must be unix, unixgram, tcp, or udp", resourceID)
		}
		switch defaultString(spec.Syslog.Facility, "local6") {
		case "kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7":
		default:
			return fmt.Errorf("%s spec.syslog.facility is invalid", resourceID)
		}
	case "otlp":
		hasTelemetry := strings.TrimSpace(spec.OTLP.TelemetryRef) != ""
		hasEndpoint := strings.TrimSpace(spec.OTLP.Endpoint) != ""
		if hasTelemetry == hasEndpoint {
			return fmt.Errorf("%s spec.otlp must set exactly one of telemetryRef or endpoint", resourceID)
		}
		if hasEndpoint {
			if _, err := url.ParseRequestURI(strings.TrimSpace(spec.OTLP.Endpoint)); err != nil {
				return fmt.Errorf("%s spec.otlp.endpoint is invalid: %w", resourceID, err)
			}
		}
	case "webhook":
		if strings.TrimSpace(spec.Webhook.URL) == "" {
			return fmt.Errorf("%s spec.webhook.url is required when type is webhook", resourceID)
		}
		if _, err := url.ParseRequestURI(strings.TrimSpace(spec.Webhook.URL)); err != nil {
			return fmt.Errorf("%s spec.webhook.url is invalid: %w", resourceID, err)
		}
		if strings.TrimSpace(spec.Webhook.Timeout) != "" {
			if _, err := time.ParseDuration(spec.Webhook.Timeout); err != nil {
				return fmt.Errorf("%s spec.webhook.timeout is invalid: %w", resourceID, err)
			}
		}
	case "file":
		name := strings.TrimSpace(spec.File.Name)
		if strings.ContainsAny(name, "/\\\x00\n\r") {
			return fmt.Errorf("%s spec.file.name must be a logical name without path separators", resourceID)
		}
	case "journald":
	default:
		return fmt.Errorf("%s spec.type must be syslog, otlp, webhook, file, or journald", resourceID)
	}
	switch defaultString(spec.MinLevel, "info") {
	case "debug", "info", "warning", "error":
	default:
		return fmt.Errorf("%s spec.minLevel must be debug, info, warning, or error", resourceID)
	}
	return nil
}

func validateIPAddressSetRefs(resourceID, path string, refs []string) error {
	seen := map[string]bool{}
	for i, ref := range refs {
		kind, name := splitResourceRef(ref)
		if kind != "IPAddressSet" || strings.TrimSpace(name) == "" {
			return fmt.Errorf("%s %s[%d] must reference IPAddressSet", resourceID, path, i)
		}
		key := kind + "/" + name
		if seen[key] {
			return fmt.Errorf("%s %s[%d] duplicates IPAddressSet reference %q", resourceID, path, i, ref)
		}
		seen[key] = true
	}
	return nil
}

func validateIPAddressSetRefsExist(resourceID, path string, refs []string, known map[string]bool) error {
	if err := validateIPAddressSetRefs(resourceID, path, refs); err != nil {
		return err
	}
	for i, ref := range refs {
		_, name := splitResourceRef(ref)
		if !known[name] {
			return fmt.Errorf("%s %s[%d] references missing IPAddressSet %q", resourceID, path, i, ref)
		}
	}
	return nil
}

func validateIngressAddressSource(resourceID, path string, source api.StatusValueSourceSpec) error {
	if strings.TrimSpace(source.Resource) == "" {
		if strings.TrimSpace(source.Field) != "" {
			return fmt.Errorf("%s %s.resource is required when field is set", resourceID, path)
		}
		return nil
	}
	if strings.TrimSpace(source.Field) == "" {
		return fmt.Errorf("%s %s.field is required", resourceID, path)
	}
	return nil
}

func validateBGPTimerProfile(resourceID, path string, spec api.BGPTimersSpec) error {
	switch strings.TrimSpace(spec.Profile) {
	case "", "default", "fast", "slow":
	default:
		return fmt.Errorf("%s %s.profile must be default, fast, or slow", resourceID, path)
	}
	if spec.Keepalive != "" || spec.HoldTime != "" || spec.ConnectRetry != "" {
		return fmt.Errorf("%s %s keepalive, holdTime, and connectRetry are not supported; use %s.profile", resourceID, path, path)
	}
	return nil
}

func validateBGPTimers(resourceID, path string, spec api.BGPTimersSpec) error {
	keepalive, err := validateOptionalDuration(resourceID, path+".keepalive", spec.Keepalive)
	if err != nil {
		return err
	}
	holdTime, err := validateOptionalDuration(resourceID, path+".holdTime", spec.HoldTime)
	if err != nil {
		return err
	}
	if _, err := validateOptionalDuration(resourceID, path+".connectRetry", spec.ConnectRetry); err != nil {
		return err
	}
	if keepalive > 0 && holdTime > 0 && holdTime <= keepalive {
		return fmt.Errorf("%s %s.holdTime must be greater than keepalive", resourceID, path)
	}
	return nil
}

func validateBGPGracefulRestart(resourceID string, spec api.BGPGracefulRestartSpec) error {
	if _, err := validateOptionalDuration(resourceID, "spec.gracefulRestart.restartTime", spec.RestartTime); err != nil {
		return err
	}
	if _, err := validateOptionalDuration(resourceID, "spec.gracefulRestart.stalePathTime", spec.StalePathTime); err != nil {
		return err
	}
	return nil
}

func validateOptionalDuration(resourceID, path, value string) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s %s is invalid: %w", resourceID, path, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("%s %s must be positive", resourceID, path)
	}
	return duration, nil
}

func validateDSLiteInnerLocalAddress(value string) error {
	addr, err := netip.ParseAddr(value)
	if err != nil || !addr.Is4() {
		return fmt.Errorf("must be an IPv4 address")
	}
	if addr.IsUnspecified() || addr.IsMulticast() || addr.IsLoopback() {
		return fmt.Errorf("must be a usable unicast IPv4 address")
	}
	return nil
}

func validateSourceResourceRef(value string) error {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return fmt.Errorf("must be Kind/name")
	}
	return nil
}

type statusReference struct {
	Path       string
	Resource   string
	Field      string
	Optional   bool
	Dependency bool
}

func validateStatusReferences(router *api.Router) error {
	known := map[string]string{}
	for _, resource := range router.Spec.Resources {
		known[resource.Kind+"/"+resource.Metadata.Name] = resource.ID()
	}
	for _, resource := range router.Spec.Resources {
		for _, ref := range collectStatusReferences(resource.Spec, "spec") {
			if err := validateStatusReference(resource.ID(), ref, known); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateStatusReference(owner string, ref statusReference, known map[string]string) error {
	resource := strings.TrimSpace(ref.Resource)
	field := strings.TrimSpace(ref.Field)
	if resource == "" {
		if field != "" {
			return fmt.Errorf("%s %s.resource is required when field is set", owner, ref.Path)
		}
		return nil
	}
	if err := validateSourceResourceRef(resource); err != nil {
		return fmt.Errorf("%s %s.resource: %w", owner, ref.Path, err)
	}
	if field == "" {
		if ref.Dependency {
			field = "phase"
		} else {
			return fmt.Errorf("%s %s.field is required", owner, ref.Path)
		}
	}
	kind, name, _ := strings.Cut(resource, "/")
	if known[resource] == "" {
		return fmt.Errorf("%s %s references missing %s %q", owner, ref.Path, kind, name)
	}
	if !api.ResourceProvidesField(kind, field) {
		return fmt.Errorf("%s %s references %s.%s, but %s does not provide field %q", owner, ref.Path, resource, field, kind, field)
	}
	return nil
}

func collectStatusReferences(value any, path string) []statusReference {
	if value == nil {
		return nil
	}
	var refs []statusReference
	collectStatusReferencesValue(reflect.ValueOf(value), path, &refs)
	return refs
}

func collectStatusReferencesValue(value reflect.Value, path string, refs *[]statusReference) {
	if !value.IsValid() {
		return
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}
	statusType := reflect.TypeOf(api.StatusValueSourceSpec{})
	dependencyType := reflect.TypeOf(api.ResourceDependencySpec{})
	if value.Type() == statusType {
		source := value.Interface().(api.StatusValueSourceSpec)
		if strings.TrimSpace(source.Resource) != "" || strings.TrimSpace(source.Field) != "" {
			*refs = append(*refs, statusReference{Path: path, Resource: source.Resource, Field: source.Field, Optional: source.Optional})
		}
		return
	}
	if value.Type() == dependencyType {
		dependency := value.Interface().(api.ResourceDependencySpec)
		if strings.TrimSpace(dependency.Resource) != "" || strings.TrimSpace(dependency.Field) != "" || strings.TrimSpace(dependency.Phase) != "" || strings.TrimSpace(dependency.Equals) != "" || dependency.NotEmpty {
			field := dependency.Field
			if strings.TrimSpace(dependency.Phase) != "" {
				field = "phase"
			}
			*refs = append(*refs, statusReference{Path: path, Resource: dependency.Resource, Field: field, Optional: dependency.Optional, Dependency: true})
		}
		return
	}
	switch value.Kind() {
	case reflect.Struct:
		valueType := value.Type()
		for i := 0; i < value.NumField(); i++ {
			field := valueType.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name := statusReferenceFieldName(field)
			if name == "-" {
				continue
			}
			childPath := path
			if name != "" {
				childPath += "." + name
			}
			collectStatusReferencesValue(value.Field(i), childPath, refs)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			collectStatusReferencesValue(value.Index(i), fmt.Sprintf("%s[%d]", path, i), refs)
		}
	case reflect.Map:
		return
	}
}

func statusReferenceFieldName(field reflect.StructField) string {
	tag := field.Tag.Get("yaml")
	if tag == "" {
		return field.Name
	}
	name := strings.Split(tag, ",")[0]
	if name == "" {
		return field.Name
	}
	return name
}

func validateAddressOrHostname(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, " \t\n\r") {
		return fmt.Errorf("must be a single address or hostname")
	}
	if _, err := netip.ParseAddr(value); err == nil {
		return nil
	}
	hostname := strings.TrimSuffix(value, ".")
	if hostname == "" || len(hostname) > 253 || strings.Contains(hostname, "..") {
		return fmt.Errorf("must be a single address or hostname")
	}
	for _, label := range strings.Split(hostname, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("must be a single address or hostname")
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z':
			case r >= 'A' && r <= 'Z':
			case r >= '0' && r <= '9':
			case r == '-':
			default:
				return fmt.Errorf("must be a single address or hostname")
			}
		}
	}
	return nil
}

func defaultPackageManager(osName string) string {
	switch osName {
	case "ubuntu", "debian":
		return "apt"
	case "fedora", "rhel", "rocky", "almalinux":
		return "dnf"
	case "freebsd":
		return "pkg"
	default:
		return ""
	}
}

func refName(ref string) string {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

func validateDNSForwarderGraph(router *api.Router, resolvers map[string]api.DNSResolverSpec, forwarders map[string]api.DNSForwarderSpec, upstreams map[string]api.DNSUpstreamSpec, zones map[string]bool, interfaces map[string]bool, wireGuardInterfaces map[string]bool) error {
	resolverForwarders := map[string]map[string]bool{}
	for name, spec := range forwarders {
		resolver := refName(spec.Resolver)
		if _, ok := resolvers[resolver]; !ok {
			return fmt.Errorf("DNSForwarder/%s references missing DNSResolver %q", name, spec.Resolver)
		}
		if resolverForwarders[resolver] == nil {
			resolverForwarders[resolver] = map[string]bool{}
		}
		resolverForwarders[resolver][name] = true
		for _, ref := range spec.ZoneRefs {
			if !zones[refName(ref)] {
				return fmt.Errorf("DNSForwarder/%s spec.zoneRefs references missing DNSZone %q", name, ref)
			}
		}
		for _, ref := range spec.Upstreams {
			if _, ok := upstreams[refName(ref)]; !ok {
				return fmt.Errorf("DNSForwarder/%s spec.upstreams references missing DNSUpstream %q", name, ref)
			}
		}
	}
	for name, spec := range upstreams {
		if spec.SourceInterface != "" && !interfaces[refName(spec.SourceInterface)] && !wireGuardInterfaces[refName(spec.SourceInterface)] {
			return fmt.Errorf("DNSUpstream/%s spec.sourceInterface references missing Interface or WireGuardInterface %q", name, spec.SourceInterface)
		}
	}
	for name, spec := range resolvers {
		if len(resolverForwarders[name]) == 0 {
			if len(spec.Sources) > 0 {
				continue
			}
			return fmt.Errorf("DNSResolver/%s requires at least one DNSForwarder", name)
		}
		for i, listen := range spec.Listen {
			for _, sourceName := range listen.Sources {
				if !resolverForwarders[name][refName(sourceName)] {
					return fmt.Errorf("DNSResolver/%s spec.listen[%d].sources references missing DNSForwarder %q for this resolver", name, i, sourceName)
				}
			}
		}
	}
	return nil
}

func validateDNSResolverCore(spec api.DNSResolverSpec) error {
	check := dnsresolver.NormalizeSpec(spec)
	check.Sources = []api.DNSResolverSourceSpec{{
		Name:      "validation-placeholder",
		Kind:      "upstream",
		Match:     []string{"."},
		Upstreams: []string{"udp://127.0.0.1:53"},
	}}
	return dnsresolver.Validate(check)
}

func validateDNSResolverHealthcheck(resourceID string, spec api.DNSResolverHealthcheckSpec) error {
	if strings.TrimSpace(spec.Interval) != "" {
		if _, err := time.ParseDuration(spec.Interval); err != nil {
			return fmt.Errorf("%s spec.healthcheck.interval must be a duration", resourceID)
		}
	}
	if strings.TrimSpace(spec.Timeout) != "" {
		if _, err := time.ParseDuration(spec.Timeout); err != nil {
			return fmt.Errorf("%s spec.healthcheck.timeout must be a duration", resourceID)
		}
	}
	return nil
}

func validateDNSUpstream(resourceID string, spec api.DNSUpstreamSpec) error {
	switch strings.ToLower(strings.TrimSpace(spec.Protocol)) {
	case "udp", "tcp", "dot", "doh":
	default:
		return fmt.Errorf("%s spec.protocol must be udp, tcp, dot, or doh", resourceID)
	}
	if strings.TrimSpace(spec.Address) == "" && len(spec.AddressFrom) == 0 {
		return fmt.Errorf("%s requires spec.address or spec.addressFrom", resourceID)
	}
	if spec.Port != 0 && (spec.Port < 1 || spec.Port > 65535) {
		return fmt.Errorf("%s spec.port must be between 1 and 65535", resourceID)
	}
	if len(spec.AddressFrom) > 0 && strings.ToLower(strings.TrimSpace(spec.Protocol)) != "udp" {
		return fmt.Errorf("%s spec.addressFrom currently supports protocol udp only", resourceID)
	}
	if strings.EqualFold(spec.Protocol, "doh") && strings.TrimSpace(spec.TLSName) != "" {
		return fmt.Errorf("%s spec.tlsName is only supported with protocol dot", resourceID)
	}
	for i, source := range spec.AddressFrom {
		if strings.TrimSpace(source.Resource) == "" {
			return fmt.Errorf("%s spec.addressFrom[%d].resource is required", resourceID, i)
		}
		if strings.TrimSpace(source.Field) == "" {
			return fmt.Errorf("%s spec.addressFrom[%d].field is required", resourceID, i)
		}
	}
	for i, source := range spec.BootstrapFrom {
		if strings.TrimSpace(source.Resource) == "" {
			return fmt.Errorf("%s spec.bootstrapFrom[%d].resource is required", resourceID, i)
		}
		if strings.TrimSpace(source.Field) == "" {
			return fmt.Errorf("%s spec.bootstrapFrom[%d].field is required", resourceID, i)
		}
	}
	if strings.EqualFold(spec.Protocol, "doh") && strings.TrimSpace(spec.Path) != "" && !strings.HasPrefix(strings.TrimSpace(spec.Path), "/") {
		return fmt.Errorf("%s spec.path must start with /", resourceID)
	}
	return nil
}

func validateHealthCheckDerivedFwMark(router *api.Router, res api.Resource, spec api.HealthCheckSpec) error {
	refs := healthcheck.DerivedFwMarkRefs(router, res.Metadata.Name)
	if len(refs) == 0 {
		return nil
	}
	var mark int
	var first healthcheck.FwMarkRef
	for _, ref := range refs {
		if ref.Mark == 0 {
			continue
		}
		if mark == 0 {
			mark = ref.Mark
			first = ref
			continue
		}
		if mark != ref.Mark {
			return fmt.Errorf("%s is referenced by routing targets with conflicting marks: %s/%s=0x%x and %s/%s=0x%x", res.ID(), first.Resource, first.Name, mark, ref.Resource, ref.Name, ref.Mark)
		}
	}
	if mark == 0 {
		return nil
	}
	if spec.FwMark != 0 && spec.FwMark != mark {
		return fmt.Errorf("%s spec.fwmark 0x%x conflicts with routing target mark 0x%x; omit spec.fwmark to derive it from the referenced route target", res.ID(), spec.FwMark, mark)
	}
	return nil
}

func isStatusExpression(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") && strings.Contains(value, ".status.")
}

func parseRetentionDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(value)
}

func validEnvironmentName(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '_' {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func validateSystemdEnvironmentFilePath(value string) error {
	path := strings.TrimPrefix(strings.TrimSpace(value), "-")
	if path == "" {
		return fmt.Errorf("must not be empty")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("must be an absolute path")
	}
	if strings.ContainsAny(path, "\x00\n\r") {
		return fmt.Errorf("contains invalid characters")
	}
	return nil
}

func validateFirewallRulePorts(resourceID string, spec api.FirewallRuleSpec) error {
	srcPorts := spec.SourcePorts
	dstPorts := spec.DestinationPorts
	if spec.Port != 0 {
		dstPorts = []api.FirewallPort{api.FirewallPort(strconv.Itoa(spec.Port))}
	}
	if len(srcPorts) == 0 && len(dstPorts) == 0 {
		return nil
	}
	if spec.Protocol != "tcp" && spec.Protocol != "udp" {
		return fmt.Errorf("%s sourcePorts, destinationPorts, or port require protocol tcp or udp", resourceID)
	}
	if spec.Port < 0 || spec.Port > 65535 {
		return fmt.Errorf("%s spec.port must be between 1 and 65535", resourceID)
	}
	if err := validateFirewallPortList(resourceID, "spec.sourcePorts", srcPorts); err != nil {
		return err
	}
	return validateFirewallPortList(resourceID, "spec.destinationPorts", dstPorts)
}

func validateFirewallPortList(resourceID, field string, ports []api.FirewallPort) error {
	rangeCount := 0
	for i, port := range ports {
		value := strings.TrimSpace(string(port))
		if value == "" {
			return fmt.Errorf("%s %s[%d] must not be empty", resourceID, field, i)
		}
		if strings.Contains(value, "-") {
			rangeCount++
			parts := strings.Split(value, "-")
			if len(parts) != 2 {
				return fmt.Errorf("%s %s[%d] must be a port or start-end range", resourceID, field, i)
			}
			start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil {
				return fmt.Errorf("%s %s[%d] range start is invalid: %w", resourceID, field, i, err)
			}
			end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return fmt.Errorf("%s %s[%d] range end is invalid: %w", resourceID, field, i, err)
			}
			if start < 1 || start > 65535 || end < 1 || end > 65535 || start > end {
				return fmt.Errorf("%s %s[%d] range must be within 1-65535 and start <= end", resourceID, field, i)
			}
			continue
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s %s[%d] must be a port number or start-end range: %w", resourceID, field, i, err)
		}
		if n < 1 || n > 65535 {
			return fmt.Errorf("%s %s[%d] must be between 1 and 65535", resourceID, field, i)
		}
	}
	if rangeCount > 0 && len(ports) > 1 {
		return fmt.Errorf("%s %s cannot mix a port range with multiple port entries", resourceID, field)
	}
	return nil
}

func validateFirewallRuleICMP(resourceID string, spec api.FirewallRuleSpec) error {
	icmpType := firewallRuleICMPType(spec)
	icmpv6Type := firewallRuleICMPv6Type(spec)
	if icmpType != "" {
		if spec.Protocol != "icmp" {
			return fmt.Errorf("%s spec.icmpType requires protocol icmp", resourceID)
		}
		if _, ok := firewallICMPTypes[strings.TrimSpace(icmpType)]; !ok {
			return fmt.Errorf("%s spec.icmpType %q is not supported", resourceID, icmpType)
		}
	}
	if icmpv6Type != "" {
		if spec.Protocol != "icmpv6" && spec.Protocol != "ipv6-icmp" {
			return fmt.Errorf("%s spec.icmpv6Type requires protocol icmpv6 or ipv6-icmp", resourceID)
		}
		if _, ok := firewallICMPv6Types[strings.TrimSpace(icmpv6Type)]; !ok {
			return fmt.Errorf("%s spec.icmpv6Type %q is not supported", resourceID, icmpv6Type)
		}
	}
	return nil
}

func firewallRuleICMPType(spec api.FirewallRuleSpec) string {
	if strings.TrimSpace(spec.ICMPType) != "" {
		return strings.TrimSpace(spec.ICMPType)
	}
	return strings.TrimSpace(spec.ICMPTypeKebab)
}

func firewallRuleICMPv6Type(spec api.FirewallRuleSpec) string {
	if strings.TrimSpace(spec.ICMPv6Type) != "" {
		return strings.TrimSpace(spec.ICMPv6Type)
	}
	return strings.TrimSpace(spec.ICMPv6TypeKebab)
}

func validateFirewallRateLimit(resourceID string, limit api.FirewallRateLimitSpec) error {
	rate := limit.Rate
	if rate == 0 {
		rate = limit.PacketsPerSecond
	}
	if rate < 0 {
		return fmt.Errorf("%s spec.rateLimit.rate must be greater than or equal to 0", resourceID)
	}
	if rate == 0 {
		if limit.Unit != "" || limit.Per != "" || limit.Burst != 0 || limit.Log {
			return fmt.Errorf("%s spec.rateLimit.rate is required when rateLimit is configured", resourceID)
		}
		return nil
	}
	switch limit.Unit {
	case "", "packet", "byte", "kilobyte", "megabyte":
	default:
		return fmt.Errorf("%s spec.rateLimit.unit must be packet, byte, kilobyte, or megabyte", resourceID)
	}
	switch limit.Per {
	case "", "second", "minute":
	default:
		return fmt.Errorf("%s spec.rateLimit.per must be second or minute", resourceID)
	}
	if limit.Burst < 0 {
		return fmt.Errorf("%s spec.rateLimit.burst must be greater than or equal to 0", resourceID)
	}
	return nil
}

func validateObservabilitySink(resourceID string, index int, sink api.ObservabilityPipelineLogSink) error {
	path := fmt.Sprintf("spec.logs.sinks[%d]", index)
	switch sink.Type {
	case "stdout", "syslog", "loki", "kafka":
	default:
		return fmt.Errorf("%s %s.type must be stdout, syslog, loki, or kafka", resourceID, path)
	}
	switch defaultString(sink.MinLevel, "info") {
	case "debug", "info", "warning", "error":
	default:
		return fmt.Errorf("%s %s.minLevel must be debug, info, warning, or error", resourceID, path)
	}
	switch sink.Type {
	case "loki":
		if strings.TrimSpace(sink.Loki.URL) == "" {
			return fmt.Errorf("%s %s.loki.url is required when type is loki", resourceID, path)
		}
		if _, err := url.ParseRequestURI(strings.TrimSpace(sink.Loki.URL)); err != nil {
			return fmt.Errorf("%s %s.loki.url is invalid: %w", resourceID, path, err)
		}
	case "syslog":
		switch sink.Syslog.Network {
		case "", "unix", "unixgram", "tcp", "udp":
		default:
			return fmt.Errorf("%s %s.syslog.network must be unix, unixgram, tcp, or udp", resourceID, path)
		}
	case "kafka":
		if len(compactStrings(sink.Kafka.Brokers)) == 0 || strings.TrimSpace(sink.Kafka.Topic) == "" {
			return fmt.Errorf("%s %s.kafka.brokers and kafka.topic document the intended sink and must be set", resourceID, path)
		}
	}
	return nil
}

func compactStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

var firewallICMPTypes = map[string]string{
	"echo-reply":              "echo-reply",
	"destination-unreachable": "destination-unreachable",
	"source-quench":           "source-quench",
	"redirect":                "redirect",
	"echo-request":            "echo-request",
	"router-advertisement":    "router-advertisement",
	"router-solicitation":     "router-solicitation",
	"time-exceeded":           "time-exceeded",
	"parameter-problem":       "parameter-problem",
	"timestamp-request":       "timestamp-request",
	"timestamp-reply":         "timestamp-reply",
}

var firewallICMPv6Types = map[string]string{
	"destination-unreachable": "destination-unreachable",
	"packet-too-big":          "packet-too-big",
	"time-exceeded":           "time-exceeded",
	"parameter-problem":       "parameter-problem",
	"echo-request":            "echo-request",
	"echo-reply":              "echo-reply",
	"router-solicit":          "nd-router-solicit",
	"router-advert":           "nd-router-advert",
	"neighbor-solicit":        "nd-neighbor-solicit",
	"neighbor-advert":         "nd-neighbor-advert",
	"nd-router-solicit":       "nd-router-solicit",
	"nd-router-advert":        "nd-router-advert",
	"nd-neighbor-solicit":     "nd-neighbor-solicit",
	"nd-neighbor-advert":      "nd-neighbor-advert",
}

func stringInSlice(value string, values []string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
