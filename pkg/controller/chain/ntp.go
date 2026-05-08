package chain

import (
	"bytes"
	"context"
	"strings"

	"routerd/pkg/api"
	"routerd/pkg/daemonapi"
	"routerd/pkg/platform"
	"routerd/pkg/resourcequery"
)

type NTPClientController struct {
	Router *api.Router
	Bus    interface {
		Publish(context.Context, daemonapi.DaemonEvent) error
	}
	Store      Store
	Command    outputCommandFunc
	ConfigPath string
	DryRun     bool
}

func (c NTPClientController) Reconcile(ctx context.Context) error {
	defaults, features := platform.Current()
	command := c.Command
	if command == nil {
		command = runOutputCommandContext
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "NTPClient" {
			continue
		}
		spec, err := resource.NTPClientSpec()
		if err != nil {
			return err
		}
		provider := firstNonEmpty(spec.Provider, defaultNTPProvider())
		if !spec.Managed {
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "NTPClient", resource.Metadata.Name, map[string]any{
				"phase":    "Observed",
				"provider": provider,
				"managed":  false,
			}); err != nil {
				return err
			}
			continue
		}
		if provider != "systemd-timesyncd" && provider != "ntpd" {
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "NTPClient", resource.Metadata.Name, map[string]any{
				"phase":    "Pending",
				"reason":   "UnsupportedProvider",
				"provider": provider,
			}); err != nil {
				return err
			}
			continue
		}
		if provider == "systemd-timesyncd" && !features.HasSystemdTimesyncd {
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "NTPClient", resource.Metadata.Name, map[string]any{
				"phase":    "Pending",
				"reason":   "SystemdTimesyncdUnsupported",
				"provider": provider,
			}); err != nil {
				return err
			}
			continue
		}
		if provider == "ntpd" && !features.HasRCD {
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "NTPClient", resource.Metadata.Name, map[string]any{
				"phase":    "Pending",
				"reason":   "NTPDUnsupported",
				"provider": provider,
			}); err != nil {
				return err
			}
			continue
		}
		servers, source := c.resolveServers(spec)
		if len(servers) == 0 {
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "NTPClient", resource.Metadata.Name, map[string]any{
				"phase":    "Pending",
				"reason":   "NoServers",
				"provider": provider,
				"source":   firstNonEmpty(spec.Source, "static"),
			}); err != nil {
				return err
			}
			continue
		}
		configPath := c.ntpConfigPath(provider, defaults)
		data := renderNTPConfig(provider, servers, compactNTPList(spec.FallbackServers))
		changed, err := writeFileIfChanged(configPath, data, 0o644, c.DryRun)
		if err != nil {
			if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "NTPClient", resource.Metadata.Name, map[string]any{
				"phase":    "Error",
				"reason":   "WriteFailed",
				"error":    err.Error(),
				"provider": provider,
				"servers":  servers,
				"dryRun":   c.DryRun,
			}); saveErr != nil {
				return saveErr
			}
			return err
		}
		if !c.DryRun {
			if err := c.applyNTPService(ctx, provider, configPath, changed, command); err != nil {
				return c.saveNTPCommandError(resource.Metadata.Name, provider, servers, "ServiceApplyFailed", err)
			}
		}
		status := map[string]any{
			"phase":      "Applied",
			"provider":   provider,
			"source":     source,
			"servers":    servers,
			"configPath": configPath,
			"changed":    changed,
			"dryRun":     c.DryRun,
		}
		if len(spec.FallbackServers) > 0 {
			status["fallbackServers"] = compactNTPList(spec.FallbackServers)
		}
		if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "NTPClient", resource.Metadata.Name, status); err != nil {
			return err
		}
	}
	return nil
}

func defaultNTPProvider() string {
	if platform.CurrentOS() == platform.OSFreeBSD {
		return "ntpd"
	}
	return "systemd-timesyncd"
}

func (c NTPClientController) ntpConfigPath(provider string, defaults platform.Defaults) string {
	if c.ConfigPath != "" {
		return c.ConfigPath
	}
	if provider == "ntpd" {
		return "/usr/local/etc/routerd/ntp.conf"
	}
	if platform.IsNixOSHost() {
		return "/run/systemd/timesyncd.conf.d/routerd.conf"
	}
	return firstNonEmpty(defaults.TimesyncdDropinFile, "/etc/systemd/timesyncd.conf.d/routerd.conf")
}

func (c NTPClientController) applyNTPService(ctx context.Context, provider, configPath string, changed bool, command outputCommandFunc) error {
	switch provider {
	case "systemd-timesyncd":
		if _, err := command(ctx, "timedatectl", "set-ntp", "true"); err != nil {
			return err
		}
		if changed {
			_, err := command(ctx, "systemctl", "restart", "systemd-timesyncd.service")
			return err
		}
		if _, err := command(ctx, "systemctl", "is-active", "--quiet", "systemd-timesyncd.service"); err != nil {
			_, err := command(ctx, "systemctl", "enable", "--now", "systemd-timesyncd.service")
			return err
		}
	case "ntpd":
		for _, args := range [][]string{
			{"ntpd_enable=YES"},
			{"ntpd_sync_on_start=YES"},
			{"ntpd_config=" + configPath},
		} {
			if _, err := command(ctx, "sysrc", args...); err != nil {
				return err
			}
		}
		if changed {
			_, err := command(ctx, "service", "ntpd", "restart")
			return err
		}
		if _, err := command(ctx, "service", "ntpd", "status"); err != nil {
			_, err := command(ctx, "service", "ntpd", "onestart")
			return err
		}
	}
	return nil
}

func (c NTPClientController) saveNTPCommandError(name, provider string, servers []string, reason string, err error) error {
	if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "NTPClient", name, map[string]any{
		"phase":    "Error",
		"reason":   reason,
		"error":    err.Error(),
		"provider": provider,
		"servers":  servers,
		"dryRun":   c.DryRun,
	}); saveErr != nil {
		return saveErr
	}
	return err
}

func (c NTPClientController) resolveServers(spec api.NTPClientSpec) ([]string, string) {
	source := firstNonEmpty(spec.Source, "static")
	switch source {
	case "static":
		return compactNTPList(spec.Servers), "static"
	case "dhcp", "dhcpv6", "auto":
		dynamic := ntpServersFromSources(c.Store, spec.ServerFrom)
		if len(dynamic) > 0 {
			return dynamic, source
		}
		if fallback := compactNTPList(spec.FallbackServers); len(fallback) > 0 {
			return fallback, "fallback"
		}
		if static := compactNTPList(spec.Servers); len(static) > 0 {
			return static, "static"
		}
	}
	return nil, source
}

func ntpServersFromSources(store Store, sources []api.StatusValueSourceSpec) []string {
	var out []string
	for _, source := range sources {
		for _, value := range resourcequery.Values(store, source) {
			for _, server := range splitNTPServerValue(value) {
				out = append(out, server)
			}
		}
	}
	return compactNTPList(out)
}

func splitNTPServerValue(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, "[] ")
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

func compactNTPList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		for _, server := range splitNTPServerValue(value) {
			if seen[server] {
				continue
			}
			seen[server] = true
			out = append(out, server)
		}
	}
	return out
}

func renderTimesyncdDropin(servers, fallbackServers []string) []byte {
	var buf bytes.Buffer
	buf.WriteString("# Generated by routerd. Do not edit by hand.\n")
	buf.WriteString("[Time]\n")
	buf.WriteString("NTP=" + strings.Join(servers, " ") + "\n")
	if len(fallbackServers) > 0 {
		buf.WriteString("FallbackNTP=" + strings.Join(fallbackServers, " ") + "\n")
	}
	return buf.Bytes()
}

func renderNTPConfig(provider string, servers, fallbackServers []string) []byte {
	if provider == "ntpd" {
		return renderNTPDConfig(servers)
	}
	return renderTimesyncdDropin(servers, fallbackServers)
}

func renderNTPDConfig(servers []string) []byte {
	var buf bytes.Buffer
	buf.WriteString("# Generated by routerd. Do not edit by hand.\n")
	buf.WriteString("driftfile /var/db/ntpd.drift\n")
	for _, server := range servers {
		buf.WriteString("server " + server + " iburst\n")
	}
	return buf.Bytes()
}
