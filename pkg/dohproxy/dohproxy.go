package dohproxy

import (
	"fmt"
	"net"
	"strings"

	"routerd/pkg/api"
)

const (
	DaemonKind         = "routerd-doh-proxy"
	BackendCloudflared = "cloudflared"
	BackendDNSCrypt    = "dnscrypt"
)

func NormalizeSpec(spec api.DoHProxySpec) api.DoHProxySpec {
	if strings.TrimSpace(spec.Backend) == "" {
		spec.Backend = BackendCloudflared
	}
	if strings.TrimSpace(spec.ListenAddress) == "" {
		spec.ListenAddress = "127.0.0.1"
	}
	if spec.ListenPort == 0 {
		spec.ListenPort = 5053
	}
	return spec
}

func Validate(spec api.DoHProxySpec) error {
	spec = NormalizeSpec(spec)
	switch spec.Backend {
	case BackendCloudflared, BackendDNSCrypt:
	default:
		return fmt.Errorf("unsupported DoH backend %q", spec.Backend)
	}
	if net.ParseIP(spec.ListenAddress) == nil {
		return fmt.Errorf("listenAddress must be an IP address")
	}
	if spec.ListenPort < 1 || spec.ListenPort > 65535 {
		return fmt.Errorf("listenPort must be between 1 and 65535")
	}
	if len(spec.Upstreams) == 0 {
		return fmt.Errorf("at least one upstream is required")
	}
	for _, upstream := range spec.Upstreams {
		if !strings.HasPrefix(strings.TrimSpace(upstream), "https://") {
			return fmt.Errorf("DoH upstream %q must use https", upstream)
		}
	}
	return nil
}

func Command(spec api.DoHProxySpec) (string, []string, error) {
	spec = NormalizeSpec(spec)
	if err := Validate(spec); err != nil {
		return "", nil, err
	}
	switch spec.Backend {
	case BackendCloudflared:
		command := firstNonEmpty(spec.Command, "cloudflared")
		args := []string{"proxy-dns", "--address", spec.ListenAddress, "--port", fmt.Sprintf("%d", spec.ListenPort)}
		for _, upstream := range spec.Upstreams {
			args = append(args, "--upstream", upstream)
		}
		return command, args, nil
	case BackendDNSCrypt:
		command := firstNonEmpty(spec.Command, "dnscrypt-proxy")
		configPath := firstNonEmpty(spec.ConfigPath, "/var/lib/routerd/doh-proxy/dnscrypt-proxy.toml")
		return command, []string{"-config", configPath}, nil
	default:
		return "", nil, fmt.Errorf("unsupported DoH backend %q", spec.Backend)
	}
}

func DNSCryptConfig(spec api.DoHProxySpec) (string, error) {
	spec = NormalizeSpec(spec)
	if err := Validate(spec); err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "listen_addresses = ['%s:%d']\n", spec.ListenAddress, spec.ListenPort)
	b.WriteString("server_names = ['routerd-static-doh']\n")
	b.WriteString("[static.'routerd-static-doh']\n")
	b.WriteString("stamp = ''\n")
	if len(spec.Upstreams) > 0 {
		fmt.Fprintf(&b, "urls = [%s]\n", quotedList(spec.Upstreams))
	}
	return b.String(), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func quotedList(values []string) string {
	var out []string
	for _, value := range values {
		out = append(out, fmt.Sprintf("%q", strings.TrimSpace(value)))
	}
	return strings.Join(out, ", ")
}
