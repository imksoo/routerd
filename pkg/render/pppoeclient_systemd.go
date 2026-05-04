package render

import "fmt"

func PPPoEClientSystemdUnit(binaryPath, resource, ifname, username, password string) []byte {
	if binaryPath == "" {
		binaryPath = "/usr/local/sbin/routerd-pppoe-client"
	}
	return []byte(fmt.Sprintf(`[Unit]
Description=routerd PPPoE client %s
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s daemon --resource %s --interface %s --username %q --password %q
Restart=always
RestartSec=5s
RuntimeDirectory=routerd/pppoe-client
StateDirectory=routerd/pppoe-client
LogsDirectory=routerd
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/run/routerd /var/lib/routerd /var/log/routerd /var/run/routerd /var/db/routerd /etc/ppp
PrivateTmp=yes
NoNewPrivileges=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_SETUID CAP_SETGID CAP_CHOWN
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_SETUID CAP_SETGID CAP_CHOWN

[Install]
WantedBy=multi-user.target
`, resource, binaryPath, resource, ifname, username, password))
}
