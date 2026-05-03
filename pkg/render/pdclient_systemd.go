package render

import (
	"fmt"
	"strconv"
)

type DHCPv6ClientSystemdOptions struct {
	BinaryPath string
	Resource   string
	Interface  string
	SocketPath string
	LeaseFile  string
	EventFile  string
	IAID       uint32
}

func DHCPv6ClientSystemdUnit(options DHCPv6ClientSystemdOptions) []byte {
	binaryPath := options.BinaryPath
	if binaryPath == "" {
		binaryPath = "/usr/local/sbin/routerd-dhcpv6-client"
	}
	resource := options.Resource
	if resource == "" {
		resource = "%i"
	}
	args := "--resource " + strconv.Quote(resource)
	if options.Interface != "" {
		args += " --interface " + strconv.Quote(options.Interface)
	}
	if options.IAID != 0 {
		args += fmt.Sprintf(" --iaid %d", options.IAID)
	}
	if options.SocketPath != "" {
		args += " --socket " + strconv.Quote(options.SocketPath)
	}
	if options.LeaseFile != "" {
		args += " --lease-file " + strconv.Quote(options.LeaseFile)
	}
	if options.EventFile != "" {
		args += " --event-file " + strconv.Quote(options.EventFile)
	}

	return []byte(fmt.Sprintf(`[Unit]
Description=routerd DHCPv6 client %s
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s %s
Restart=always
RestartSec=5s
RuntimeDirectory=routerd/dhcpv6-client
StateDirectory=routerd/dhcpv6-client
NoNewPrivileges=yes
PrivateTmp=yes
ProtectHome=yes
ProtectSystem=strict
ReadWritePaths=/run/routerd /var/lib/routerd
RestrictAddressFamilies=AF_UNIX AF_INET6 AF_NETLINK
CapabilityBoundingSet=CAP_NET_RAW CAP_NET_ADMIN CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_RAW CAP_NET_ADMIN CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
`, resource, binaryPath, args))
}
