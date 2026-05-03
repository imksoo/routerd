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
ProtectSystem=strict
ReadWritePaths=/run/routerd /var/lib/routerd /var/run/routerd /var/db/routerd
PrivateTmp=yes
NoNewPrivileges=yes

[Install]
WantedBy=multi-user.target
`, resource, binaryPath, resource, ifname, username, password))
}
