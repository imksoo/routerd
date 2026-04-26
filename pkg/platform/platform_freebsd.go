//go:build freebsd

package platform

// FreeBSD support is in-progress. These defaults describe the intended
// install layout so packaging, rc.d scripts, and renderers can target a
// stable set of paths. The renderers themselves are still being ported,
// so building routerd on FreeBSD currently produces a binary that can
// validate, plan, and dry-run but cannot apply host changes for every
// resource kind. See docs/platforms.md.

func currentDefaults() Defaults {
	return Defaults{
		OS:                       OSFreeBSD,
		PrefixDir:                "/usr/local",
		BinDir:                   "/usr/local/sbin",
		SysconfDir:               "/usr/local/etc/routerd",
		PluginDir:                "/usr/local/libexec/routerd/plugins",
		RuntimeDir:               "/var/run/routerd",
		StateDir:                 "/var/db/routerd",
		RCScriptDir:              "/usr/local/etc/rc.d",
		DnsmasqConfigFile:        "/usr/local/etc/routerd/dnsmasq.conf",
		DnsmasqServiceFile:       "/usr/local/etc/rc.d/routerd_dnsmasq",
		PPPoEChapSecretsFile:     "/etc/ppp/chap-secrets",
		PPPoEPapSecretsFile:      "/etc/ppp/pap-secrets",
	}
}

func currentFeatures() Features {
	return Features{
		HasPF:       true,
		HasIproute2: false,
		HasRCD:      true,
	}
}
