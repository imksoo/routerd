//go:build !linux && !freebsd

package platform

// Fallback for unsupported build targets. routerd is not expected to
// run on these platforms, but the package must compile so that
// platform-agnostic tests and tooling (schema generation, validation)
// can still be exercised on developer machines.

func currentDefaults() Defaults {
	return Defaults{
		OS:         OSOther,
		PrefixDir:  "/usr/local",
		BinDir:     "/usr/local/sbin",
		SysconfDir: "/usr/local/etc/routerd",
		PluginDir:  "/usr/local/libexec/routerd/plugins",
		RuntimeDir: "/tmp/routerd",
		StateDir:   "/tmp/routerd/state",
	}
}

func currentFeatures() Features {
	return Features{}
}
