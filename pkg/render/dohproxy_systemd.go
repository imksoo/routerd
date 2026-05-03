package render

import (
	"fmt"
	"strconv"
	"strings"

	"routerd/pkg/api"
	"routerd/pkg/dohproxy"
)

func DoHProxySystemdUnit(name string, spec api.DoHProxySpec, binaryPath string) []byte {
	spec = dohproxy.NormalizeSpec(spec)
	if strings.TrimSpace(binaryPath) == "" {
		binaryPath = "/usr/local/sbin/routerd-doh-proxy"
	}
	var args []string
	args = append(args,
		"daemon",
		"--resource", strconv.Quote(name),
		"--backend", strconv.Quote(spec.Backend),
		"--listen-address", strconv.Quote(spec.ListenAddress),
		"--listen-port", fmt.Sprintf("%d", spec.ListenPort),
	)
	if len(spec.Upstreams) > 0 {
		args = append(args, "--upstream", strconv.Quote(strings.Join(spec.Upstreams, ",")))
	}
	args = append(args,
		"--health-interval", strconv.Quote(spec.Healthcheck.Interval),
		"--health-timeout", strconv.Quote(spec.Healthcheck.Timeout),
		"--health-fail-threshold", fmt.Sprintf("%d", spec.Healthcheck.FailThreshold),
		"--health-pass-threshold", fmt.Sprintf("%d", spec.Healthcheck.PassThreshold),
	)
	if spec.Command != "" {
		args = append(args, "--command", strconv.Quote(spec.Command))
	}
	if spec.SocketPath != "" {
		args = append(args, "--socket", strconv.Quote(spec.SocketPath))
	}
	if spec.StateFile != "" {
		args = append(args, "--state-file", strconv.Quote(spec.StateFile))
	}
	if spec.EventFile != "" {
		args = append(args, "--event-file", strconv.Quote(spec.EventFile))
	}
	return []byte(`[Unit]
Description=routerd DoH proxy ` + name + `
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + binaryPath + ` ` + strings.Join(args, " ") + `
Restart=always
RestartSec=5s
RuntimeDirectory=routerd/doh-proxy
StateDirectory=routerd/doh-proxy
ProtectSystem=strict
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
`)
}
