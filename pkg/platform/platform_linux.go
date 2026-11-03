// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package platform

func currentDefaults() Defaults {
	return Defaults{
		OS:                       OSLinux,
		PrefixDir:                "/usr",
		BinDir:                   linuxBinaryDir,
		SysconfDir:               "/etc/routerd",
		PluginDir:                "/usr/libexec/routerd/plugins",
		RuntimeDir:               "/run/routerd",
		StateDir:                 "/var/lib/routerd",
		SystemdUnitDir:           "/usr/lib/systemd/system",
		NetplanFile:              "/etc/netplan/90-routerd.yaml",
		NetworkdDropinDir:        "/etc/systemd/network",
		SystemdSystemDir:         "/etc/systemd/system",
		TimesyncdDropinFile:      "/etc/systemd/timesyncd.conf.d/routerd.conf",
		DnsmasqConfigFile:        "/etc/routerd/dnsmasq.conf",
		DnsmasqServiceFile:       "/etc/systemd/system/routerd-dnsmasq.service",
		NftablesFile:             "/etc/routerd/nftables.nft",
		DefaultRouteNftablesFile: "/etc/routerd/default-route.nft",
		PPPoEChapSecretsFile:     "/etc/ppp/chap-secrets",
		PPPoEPapSecretsFile:      "/etc/ppp/pap-secrets",
	}
}

func currentFeatures() Features {
	return Features{
		HasSystemd:          true,
		HasNetplan:          true,
		HasSystemdNetworkd:  true,
		HasSystemdTimesyncd: true,
		HasNftables:         true,
		HasIproute2:         true,
		HasResolvectl:       true,
	}
}
