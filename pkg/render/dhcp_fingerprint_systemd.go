// SPDX-License-Identifier: BSD-3-Clause

package render

import "path/filepath"

func DHCPFingerprintWatcherSystemdUnit(stateDir string) []byte {
	return []byte(`# routerd-managed-service: v1
[Unit]
Description=routerd DHCP fingerprint watcher
BindsTo=routerd-dnsmasq.service
PartOf=routerd-dnsmasq.service
After=routerd-dnsmasq.service

[Service]
Slice=routerd.slice
Type=simple
ExecStart=` + quoteCommand([]string{"/usr/local/sbin/routerd-dhcp-fingerprint-watcher", "--db", filepath.Join(stateDir, "dhcp-fingerprints.db"), "--journal-unit", "routerd-dnsmasq.service"}) + `
Restart=on-failure
RestartSec=5s
StateDirectory=routerd
NoNewPrivileges=yes
`)
}
