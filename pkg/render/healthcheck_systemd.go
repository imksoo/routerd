package render

import (
	"fmt"
	"strconv"
)

type HealthCheckSystemdOptions struct {
	BinaryPath      string
	Resource        string
	Target          string
	Protocol        string
	Via             string
	SourceInterface string
	SourceAddress   string
	Port            int
	Interval        string
	Timeout         string
	SocketPath      string
	StateFile       string
	EventFile       string
}

func HealthCheckSystemdUnit(options HealthCheckSystemdOptions) []byte {
	binaryPath := options.BinaryPath
	if binaryPath == "" {
		binaryPath = "/usr/local/sbin/routerd-healthcheck"
	}
	resource := options.Resource
	if resource == "" {
		resource = "%i"
	}
	args := "--resource " + strconv.Quote(resource)
	if options.Target != "" {
		args += " --target " + strconv.Quote(options.Target)
	}
	if options.Protocol != "" {
		args += " --protocol " + strconv.Quote(options.Protocol)
	}
	if options.Via != "" {
		args += " --via " + strconv.Quote(options.Via)
	}
	if options.SourceInterface != "" {
		args += " --source-interface " + strconv.Quote(options.SourceInterface)
	}
	if options.SourceAddress != "" {
		args += " --source-address " + strconv.Quote(options.SourceAddress)
	}
	if options.Port != 0 {
		args += fmt.Sprintf(" --port %d", options.Port)
	}
	if options.Interval != "" {
		args += " --interval " + strconv.Quote(options.Interval)
	}
	if options.Timeout != "" {
		args += " --timeout " + strconv.Quote(options.Timeout)
	}
	if options.SocketPath != "" {
		args += " --socket " + strconv.Quote(options.SocketPath)
	}
	if options.StateFile != "" {
		args += " --state-file " + strconv.Quote(options.StateFile)
	}
	if options.EventFile != "" {
		args += " --event-file " + strconv.Quote(options.EventFile)
	}

	return []byte(fmt.Sprintf(`[Unit]
Description=routerd healthcheck %s
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s %s
Restart=always
RestartSec=5s
RuntimeDirectory=routerd/healthcheck
StateDirectory=routerd/healthcheck
NoNewPrivileges=yes
PrivateTmp=yes
ProtectHome=yes
ProtectSystem=strict
ReadWritePaths=/run/routerd /var/lib/routerd
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
CapabilityBoundingSet=CAP_NET_RAW
AmbientCapabilities=CAP_NET_RAW

[Install]
WantedBy=multi-user.target
`, resource, binaryPath, args))
}
