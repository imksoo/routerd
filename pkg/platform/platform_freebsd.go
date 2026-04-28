//go:build freebsd

package platform

// FreeBSD support is in-progress. These defaults describe the install
// layout so packaging, rc.d scripts, renderers, and the limited FreeBSD
// applier can target stable paths. See docs/platforms.md.

func currentDefaults() Defaults {
	return Defaults{
		OS:                        OSFreeBSD,
		PrefixDir:                 "/usr/local",
		BinDir:                    "/usr/local/sbin",
		SysconfDir:                "/usr/local/etc/routerd",
		PluginDir:                 "/usr/local/libexec/routerd/plugins",
		RuntimeDir:                "/var/run/routerd",
		StateDir:                  "/var/db/routerd",
		RCScriptDir:               "/usr/local/etc/rc.d",
		DnsmasqConfigFile:         "/usr/local/etc/routerd/dnsmasq.conf",
		DnsmasqServiceFile:        "/usr/local/etc/rc.d/routerd_dnsmasq",
		FreeBSDDHClientConfigFile: "/etc/dhclient.conf",
		FreeBSDDHCP6CConfigFile:   "/usr/local/etc/dhcp6c.conf",
		PPPoEChapSecretsFile:      "/etc/ppp/chap-secrets",
		PPPoEPapSecretsFile:       "/etc/ppp/pap-secrets",
		FreeBSDMPD5ConfigFile:     "/usr/local/etc/mpd5/mpd.conf",
	}
}

func currentFeatures() Features {
	return Features{
		HasPF:       true,
		HasIproute2: false,
		HasRCD:      true,
	}
}
