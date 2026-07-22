// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

var freeBSDLoaderModuleIdentifier = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]*$`)

func validateSystemResource(res api.Resource, targetOS platform.OS) (bool, error) {
	switch res.Kind {
	case "LogSink":
		if res.APIVersion != api.SystemAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.LogSinkSpec()
		if err != nil {
			return true, err
		}
		if err := validateLogSinkSpec(res.ID(), spec); err != nil {
			return true, err
		}
	case "Telemetry":
		if res.APIVersion != api.ObservabilityAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.ObservabilityAPIVersion)
		}
		spec, err := res.TelemetrySpec()
		if err != nil {
			return true, err
		}
		if strings.TrimSpace(spec.OTLP.Endpoint) == "" {
			return true, fmt.Errorf("%s spec.otlp.endpoint is required", res.ID())
		}
		for _, signal := range spec.Signals {
			switch signal {
			case "logs", "metrics", "traces":
			default:
				return true, fmt.Errorf("%s spec.signals must contain only logs, metrics, or traces", res.ID())
			}
		}
	case "ObservabilityPipeline":
		if res.APIVersion != api.SystemAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.ObservabilityPipelineSpec()
		if err != nil {
			return true, err
		}
		if strings.TrimSpace(spec.OTLP.Endpoint) != "" {
			if _, err := url.ParseRequestURI(strings.TrimSpace(spec.OTLP.Endpoint)); err != nil {
				return true, fmt.Errorf("%s spec.otlp.endpoint is invalid: %w", res.ID(), err)
			}
		}
		for _, signal := range spec.Signals {
			switch signal {
			case "logs", "metrics", "traces":
			default:
				return true, fmt.Errorf("%s spec.signals must contain only logs, metrics, or traces", res.ID())
			}
		}
		if spec.Sampling.Rate < 0 || spec.Sampling.Rate > 1 {
			return true, fmt.Errorf("%s spec.sampling.rate must be between 0 and 1", res.ID())
		}
		for i, sink := range spec.Logs.Sinks {
			if err := validateObservabilitySink(res.ID(), i, sink); err != nil {
				return true, err
			}
		}
	case "RouterdCluster":
		if res.APIVersion != api.SystemAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.RouterdClusterSpec()
		if err != nil {
			return true, err
		}
		if len(compactStrings(spec.Peers)) < 2 {
			return true, fmt.Errorf("%s spec.peers must contain at least 2 peers", res.ID())
		}
		ttl := 30 * time.Second
		if strings.TrimSpace(spec.LeaseTTL) != "" {
			var err error
			ttl, err = time.ParseDuration(spec.LeaseTTL)
			if err != nil {
				return true, fmt.Errorf("%s spec.leaseTTL is invalid: %w", res.ID(), err)
			}
		}
		if ttl < 5*time.Second || ttl > 10*time.Minute {
			return true, fmt.Errorf("%s spec.leaseTTL must be between 5s and 10m", res.ID())
		}
		if strings.TrimSpace(spec.LeasePath) == "" {
			return true, fmt.Errorf("%s spec.leasePath is required", res.ID())
		}
		if strings.ContainsAny(spec.LeasePath, "\n\r") || !strings.HasPrefix(strings.TrimSpace(spec.LeasePath), "/") {
			return true, fmt.Errorf("%s spec.leasePath must be an absolute path", res.ID())
		}
	case "Sysctl":
		if res.APIVersion != api.SystemAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.SysctlSpec()
		if err != nil {
			return true, err
		}
		key := spec.Key
		if key == "" {
			return true, fmt.Errorf("%s spec.key is required", res.ID())
		}
		if strings.ContainsAny(key, " \t\n/") {
			return true, fmt.Errorf("%s spec.key contains invalid whitespace or slash", res.ID())
		}
		if spec.Value == "" {
			return true, fmt.Errorf("%s spec.value is required", res.ID())
		}
		if strings.TrimSpace(spec.ExpectedValue) == "" && spec.ExpectedValue != "" {
			return true, fmt.Errorf("%s spec.expectedValue must not be blank", res.ID())
		}
		switch defaultString(spec.Compare, "exact") {
		case "exact", "atLeast":
		default:
			return true, fmt.Errorf("%s spec.compare must be exact or atLeast", res.ID())
		}
	case "SysctlProfile":
		if res.APIVersion != api.SystemAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.SysctlProfileSpec()
		if err != nil {
			return true, err
		}
		switch spec.Profile {
		case "router-linux", "router-freebsd":
		default:
			return true, fmt.Errorf("%s spec.profile must be router-linux or router-freebsd", res.ID())
		}
		for key, value := range spec.Overrides {
			if strings.TrimSpace(key) == "" || strings.ContainsAny(key, " \t\n/") {
				return true, fmt.Errorf("%s spec.overrides contains invalid sysctl key %q", res.ID(), key)
			}
			if strings.TrimSpace(value) == "" {
				return true, fmt.Errorf("%s spec.overrides[%s] must not be empty", res.ID(), key)
			}
		}
	case "KernelModule":
		if res.APIVersion != api.SystemAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.KernelModuleSpec()
		if err != nil {
			return true, err
		}
		switch defaultString(spec.State, "present") {
		case "present":
		default:
			return true, fmt.Errorf("%s spec.state must be present", res.ID())
		}
		if len(spec.Modules) == 0 {
			return true, fmt.Errorf("%s spec.modules is required", res.ID())
		}
		seen := map[string]bool{}
		for i, module := range spec.Modules {
			module = strings.TrimSpace(module)
			if module == "" {
				return true, fmt.Errorf("%s spec.modules[%d] is required", res.ID(), i)
			}
			if strings.ContainsAny(module, "/ \t\n") {
				return true, fmt.Errorf("%s spec.modules[%d] must be a module name, not a path or command", res.ID(), i)
			}
			if targetOS == platform.OSFreeBSD && !freeBSDLoaderModuleIdentifier.MatchString(module) {
				return true, fmt.Errorf("%s spec.modules[%d] must be a FreeBSD loader variable identifier", res.ID(), i)
			}
			if seen[module] {
				return true, fmt.Errorf("%s spec.modules[%d] duplicates %q", res.ID(), i, module)
			}
			seen[module] = true
		}
	case "Package":
		if res.APIVersion != api.SystemAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.PackageSpec()
		if err != nil {
			return true, err
		}
		switch defaultString(spec.State, "present") {
		case "present":
		default:
			return true, fmt.Errorf("%s spec.state must be present", res.ID())
		}
		if len(spec.Packages) == 0 {
			return true, fmt.Errorf("%s spec.packages is required", res.ID())
		}
		for i, set := range spec.Packages {
			switch set.OS {
			case "ubuntu", "debian", "fedora", "rhel", "rocky", "almalinux", "freebsd":
			default:
				return true, fmt.Errorf("%s spec.packages[%d].os must be ubuntu, debian, fedora, rhel, rocky, almalinux, or freebsd", res.ID(), i)
			}
			manager := defaultString(set.Manager, defaultPackageManager(set.OS))
			switch manager {
			case "apt", "dnf", "pkg":
			default:
				return true, fmt.Errorf("%s spec.packages[%d].manager must be apt, dnf, or pkg", res.ID(), i)
			}
			if expected := defaultPackageManager(set.OS); expected != "" && manager != expected {
				return true, fmt.Errorf("%s spec.packages[%d].manager must be %s for os %s", res.ID(), i, expected, set.OS)
			}
			if len(set.Names) == 0 {
				return true, fmt.Errorf("%s spec.packages[%d].names is required", res.ID(), i)
			}
			for j, name := range set.Names {
				if strings.TrimSpace(name) == "" {
					return true, fmt.Errorf("%s spec.packages[%d].names[%d] must not be empty", res.ID(), i, j)
				}
				if strings.ContainsAny(name, " \t\n\r/") {
					return true, fmt.Errorf("%s spec.packages[%d].names[%d] must be a package name", res.ID(), i, j)
				}
			}
		}
	case "NetworkAdoption":
		if res.APIVersion != api.SystemAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.NetworkAdoptionSpec()
		if err != nil {
			return true, err
		}
		switch defaultString(spec.State, "present") {
		case "present", "absent":
		default:
			return true, fmt.Errorf("%s spec.state must be present or absent", res.ID())
		}
		if spec.Interface == "" && spec.IfName == "" && !spec.SystemdResolved.DisableDNSStubListener {
			return true, fmt.Errorf("%s must set spec.interface, spec.ifname, or spec.systemdResolved.disableDNSStubListener", res.ID())
		}
		for field, value := range map[string]string{"interface": spec.Interface, "ifname": spec.IfName, "systemdNetworkd.dropinName": spec.SystemdNetworkd.DropinName, "systemdResolved.dropinName": spec.SystemdResolved.DropinName} {
			if strings.ContainsAny(value, "\x00\n\r") {
				return true, fmt.Errorf("%s spec.%s contains invalid characters", res.ID(), field)
			}
		}
	case "LogRetention":
		if res.APIVersion != api.SystemAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.LogRetentionSpec()
		if err != nil {
			return true, err
		}
		switch spec.Schedule {
		case "", "daily":
		default:
			return true, fmt.Errorf("%s spec.schedule must be daily", res.ID())
		}
		if strings.TrimSpace(spec.Retention) == "" {
			return true, fmt.Errorf("%s spec.retention is required", res.ID())
		}
		if _, err := parseRetentionDuration(spec.Retention); err != nil {
			return true, fmt.Errorf("%s spec.retention must be a duration: %w", res.ID(), err)
		}
		for i, signal := range spec.Signals {
			switch signal {
			case "events", "dnsQueries", "trafficFlows", "firewallEvents":
			default:
				return true, fmt.Errorf("%s spec.signals[%d] must be events, dnsQueries, trafficFlows, or firewallEvents", res.ID(), i)
			}
		}
	case "NTPClient":
		if res.APIVersion != api.SystemAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.NTPClientSpec()
		if err != nil {
			return true, err
		}
		switch defaultString(spec.Provider, "systemd-timesyncd") {
		case "systemd-timesyncd", "chrony", "ntpd":
		default:
			return true, fmt.Errorf("%s spec.provider must be systemd-timesyncd, chrony, or ntpd", res.ID())
		}
		switch defaultString(spec.Source, "static") {
		case "static", "auto", "dhcp", "dhcpv6":
		default:
			return true, fmt.Errorf("%s spec.source must be static, auto, dhcp, or dhcpv6", res.ID())
		}
		if defaultString(spec.Source, "static") == "static" && spec.Managed && len(spec.Servers) == 0 {
			return true, fmt.Errorf("%s spec.servers is required when spec.source is static", res.ID())
		}
		if spec.Managed && len(spec.Servers) == 0 && len(spec.ServerFrom) == 0 && len(spec.FallbackServers) == 0 {
			return true, fmt.Errorf("%s spec.servers, spec.serverFrom, or spec.fallbackServers is required when managed is true", res.ID())
		}
		for i, server := range append(append([]string{}, spec.Servers...), spec.FallbackServers...) {
			if strings.TrimSpace(server) == "" {
				return true, fmt.Errorf("%s NTP server entry %d must not be empty", res.ID(), i)
			}
			if strings.ContainsAny(server, " \t\n\r") {
				return true, fmt.Errorf("%s NTP server entry %d must be a single hostname or IP address", res.ID(), i)
			}
		}
		for i, source := range spec.ServerFrom {
			if strings.TrimSpace(source.Resource) == "" {
				return true, fmt.Errorf("%s spec.serverFrom[%d].resource is required", res.ID(), i)
			}
			if strings.TrimSpace(source.Field) == "" {
				return true, fmt.Errorf("%s spec.serverFrom[%d].field is required", res.ID(), i)
			}
		}
	case "NTPServer":
		if res.APIVersion != api.SystemAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.NTPServerSpec()
		if err != nil {
			return true, err
		}
		switch defaultString(spec.Provider, "chrony") {
		case "chrony", "ntpd":
		default:
			return true, fmt.Errorf("%s spec.provider must be chrony or ntpd", res.ID())
		}
		switch defaultString(spec.Source, "static") {
		case "static", "auto", "dhcp", "dhcpv6":
		default:
			return true, fmt.Errorf("%s spec.source must be static, auto, dhcp, or dhcpv6", res.ID())
		}
		if defaultString(spec.Source, "static") == "static" && spec.Managed && len(spec.Servers) == 0 {
			return true, fmt.Errorf("%s spec.servers is required when spec.source is static", res.ID())
		}
		if spec.Managed && len(spec.Servers) == 0 && len(spec.ServerFrom) == 0 && len(spec.FallbackServers) == 0 {
			return true, fmt.Errorf("%s spec.servers, spec.serverFrom, or spec.fallbackServers is required when managed is true", res.ID())
		}
		for i, server := range append(append([]string{}, spec.Servers...), spec.FallbackServers...) {
			if strings.TrimSpace(server) == "" {
				return true, fmt.Errorf("%s NTP server entry %d must not be empty", res.ID(), i)
			}
			if strings.ContainsAny(server, " \t\n\r") {
				return true, fmt.Errorf("%s NTP server entry %d must be a single hostname or IP address", res.ID(), i)
			}
		}
		for i, cidr := range spec.AllowCIDRs {
			if strings.TrimSpace(cidr) == "" {
				return true, fmt.Errorf("%s spec.allowCIDRs[%d] must not be empty", res.ID(), i)
			}
		}
		sources := append([]api.StatusValueSourceSpec{}, spec.ServerFrom...)
		sources = append(sources, spec.ListenAddressFrom...)
		sources = append(sources, spec.AllowCIDRFrom...)
		for i, source := range sources {
			if strings.TrimSpace(source.Resource) == "" {
				return true, fmt.Errorf("%s source reference %d resource is required", res.ID(), i)
			}
			if strings.TrimSpace(source.Field) == "" {
				return true, fmt.Errorf("%s source reference %d field is required", res.ID(), i)
			}
		}
	case "WebConsole":
		if res.APIVersion != api.SystemAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.WebConsoleSpec()
		if err != nil {
			return true, err
		}
		if strings.TrimSpace(spec.ListenAddress) != "" {
			if _, err := netip.ParseAddr(spec.ListenAddress); err != nil {
				return true, fmt.Errorf("%s spec.listenAddress must be an IP address", res.ID())
			}
		}
		if spec.ListenAddressFrom.Resource != "" && spec.ListenAddressFrom.Field == "" {
			return true, fmt.Errorf("%s spec.listenAddressFrom.field is required", res.ID())
		}
		if spec.Port < 0 || spec.Port > 65535 {
			return true, fmt.Errorf("%s spec.port must be omitted or between 1 and 65535", res.ID())
		}
		if spec.BasePath != "" {
			if !strings.HasPrefix(spec.BasePath, "/") || strings.ContainsAny(spec.BasePath, "\x00\r\n") {
				return true, fmt.Errorf("%s spec.basePath must be an absolute HTTP path", res.ID())
			}
		}
	case "ControlAPI":
		if res.APIVersion != api.SystemAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.ControlAPISpec()
		if err != nil {
			return true, err
		}
		if strings.TrimSpace(spec.ListenAddress) != "" {
			if _, err := netip.ParseAddr(spec.ListenAddress); err != nil {
				return true, fmt.Errorf("%s spec.listenAddress must be an IP address", res.ID())
			}
		}
		if spec.Port < 0 || spec.Port > 65535 {
			return true, fmt.Errorf("%s spec.port must be omitted or between 1 and 65535", res.ID())
		}
		if err := validateControlAPIAllowCIDRs(res.ID(), spec.AllowCIDRs); err != nil {
			return true, err
		}
		if err := validateSecretValueSource(res.ID(), "", "", "spec.tokenFrom", spec.TokenFrom); err != nil {
			return true, err
		}
		if err := validateControlAPITLS(res.ID(), spec.TLS); err != nil {
			return true, err
		}
	case "Inventory":
		if res.APIVersion != api.RouterAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.RouterAPIVersion)
		}
		if res.Metadata.Name != "host" {
			return true, fmt.Errorf("%s metadata.name must be host", res.ID())
		}
	case "Hostname":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.HostnameSpec()
		if err != nil {
			return true, err
		}
		hostname := spec.Hostname
		if hostname == "" {
			return true, fmt.Errorf("%s spec.hostname is required", res.ID())
		}
		if strings.ContainsAny(hostname, " \t\n/") {
			return true, fmt.Errorf("%s spec.hostname contains invalid whitespace or slash", res.ID())
		}
	default:
		return false, nil
	}
	return true, nil
}

func validateControlAPITLS(resourceID string, spec api.ControlAPITLSSpec) error {
	cert := strings.TrimSpace(spec.CertFile)
	key := strings.TrimSpace(spec.KeyFile)
	clientCA := strings.TrimSpace(spec.ClientCAFile)
	for path, value := range map[string]string{
		"spec.tls.certFile":     cert,
		"spec.tls.keyFile":      key,
		"spec.tls.clientCAFile": clientCA,
	} {
		if strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("%s %s is invalid", resourceID, path)
		}
	}
	if (cert == "") != (key == "") {
		return fmt.Errorf("%s spec.tls.certFile and spec.tls.keyFile must be set together", resourceID)
	}
	if clientCA != "" && cert == "" {
		return fmt.Errorf("%s spec.tls.clientCAFile requires spec.tls.certFile and spec.tls.keyFile", resourceID)
	}
	return nil
}

func validateControlAPIAllowCIDRs(resourceID string, cidrs []string) error {
	for i, cidr := range cidrs {
		text := strings.TrimSpace(cidr)
		if text == "" {
			return fmt.Errorf("%s spec.allowCIDRs[%d] must not be empty", resourceID, i)
		}
		prefix, err := netip.ParsePrefix(text)
		if err != nil {
			return fmt.Errorf("%s spec.allowCIDRs[%d] must be a valid CIDR prefix: %w", resourceID, i, err)
		}
		if prefix.Bits() == 0 {
			addr := prefix.Addr().Unmap()
			if addr.Is4() && addr == netip.IPv4Unspecified() {
				return fmt.Errorf("%s spec.allowCIDRs[%d] must not be 0.0.0.0/0", resourceID, i)
			}
			if addr.Is6() && addr == netip.IPv6Unspecified() {
				return fmt.Errorf("%s spec.allowCIDRs[%d] must not be ::/0", resourceID, i)
			}
		}
	}
	return nil
}
