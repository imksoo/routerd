package render

import (
	"strconv"
	"strings"

	"routerd/pkg/api"
	"routerd/pkg/dnsresolver"
)

func DNSResolverSystemdUnit(name string, spec api.DNSResolverSpec, binaryPath, configPath string) []byte {
	_ = dnsresolver.NormalizeSpec(spec)
	if strings.TrimSpace(binaryPath) == "" {
		binaryPath = "/usr/local/sbin/routerd-dns-resolver"
	}
	if strings.TrimSpace(configPath) == "" {
		configPath = "/var/lib/routerd/dns-resolver/" + name + "/config.json"
	}
	var args []string
	args = append(args,
		"daemon",
		"--resource", strconv.Quote(name),
		"--config-file", strconv.Quote(configPath),
	)
	return []byte(`[Unit]
Description=routerd DNS resolver ` + name + `
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + binaryPath + ` ` + strings.Join(args, " ") + `
Restart=always
RestartSec=5s
RuntimeDirectory=routerd/dns-resolver
StateDirectory=routerd/dns-resolver
LogsDirectory=routerd
NoNewPrivileges=yes
ProtectHome=yes
ProtectSystem=strict
PrivateTmp=yes
ReadWritePaths=/run/routerd /var/lib/routerd /var/log/routerd
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
`)
}
