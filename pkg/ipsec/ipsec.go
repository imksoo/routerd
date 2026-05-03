package ipsec

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"routerd/pkg/api"
)

type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type Controller struct {
	Command CommandRunner
	DryRun  bool
}

func RenderSwanctl(name string, spec api.IPsecConnectionSpec) ([]byte, error) {
	if name == "" {
		return nil, fmt.Errorf("connection name is required")
	}
	if spec.PreSharedKey == "" && spec.CertificateRef == "" {
		return nil, fmt.Errorf("preSharedKey or certificateRef is required")
	}
	ike := defaultList(spec.Phase1Proposals, "aes256-sha256-modp2048")
	esp := defaultList(spec.Phase2Proposals, "aes256-sha256")
	var out bytes.Buffer
	fmt.Fprintf(&out, "connections {\n")
	fmt.Fprintf(&out, "  %s {\n", name)
	fmt.Fprintf(&out, "    local_addrs = %s\n", spec.LocalAddress)
	fmt.Fprintf(&out, "    remote_addrs = %s\n", spec.RemoteAddress)
	fmt.Fprintf(&out, "    proposals = %s\n", strings.Join(ike, ","))
	fmt.Fprintf(&out, "    local {\n")
	if spec.CertificateRef != "" {
		fmt.Fprintf(&out, "      certs = %s\n", spec.CertificateRef)
	} else {
		fmt.Fprintf(&out, "      auth = psk\n")
	}
	fmt.Fprintf(&out, "    }\n")
	fmt.Fprintf(&out, "    remote {\n")
	fmt.Fprintf(&out, "      auth = %s\n", authMode(spec))
	fmt.Fprintf(&out, "    }\n")
	fmt.Fprintf(&out, "    children {\n")
	fmt.Fprintf(&out, "      net {\n")
	fmt.Fprintf(&out, "        local_ts = %s\n", spec.LeftSubnet)
	fmt.Fprintf(&out, "        remote_ts = %s\n", spec.RightSubnet)
	fmt.Fprintf(&out, "        esp_proposals = %s\n", strings.Join(esp, ","))
	fmt.Fprintf(&out, "        start_action = trap\n")
	fmt.Fprintf(&out, "      }\n")
	fmt.Fprintf(&out, "    }\n")
	fmt.Fprintf(&out, "  }\n")
	fmt.Fprintf(&out, "}\n")
	if spec.PreSharedKey != "" {
		fmt.Fprintf(&out, "secrets {\n")
		fmt.Fprintf(&out, "  ike-%s {\n", name)
		fmt.Fprintf(&out, "    secret = %s\n", spec.PreSharedKey)
		fmt.Fprintf(&out, "  }\n")
		fmt.Fprintf(&out, "}\n")
	}
	return out.Bytes(), nil
}

func (c Controller) Load(ctx context.Context, path string) error {
	if c.DryRun {
		return nil
	}
	run := c.Command
	if run == nil {
		run = runCommand
	}
	args := []string{"--load-conns"}
	if path != "" {
		args = append(args, "--file", path)
	}
	_, err := run(ctx, "swanctl", args...)
	return err
}

func RecordMetrics(ctx context.Context, connection string, established int64, bytes int64) {
	meter := otel.Meter("routerd.ipsec")
	count, _ := meter.Int64Counter("routerd.ipsec.sa.established.count")
	traffic, _ := meter.Int64Counter("routerd.ipsec.tunnel.bytes")
	attrs := metric.WithAttributes(attribute.String("routerd.ipsec.connection", connection))
	if established > 0 {
		count.Add(ctx, established, attrs)
	}
	if bytes > 0 {
		traffic.Add(ctx, bytes, attrs)
	}
}

func defaultList(values []string, fallback string) []string {
	if len(values) == 0 {
		return []string{fallback}
	}
	return values
}

func authMode(spec api.IPsecConnectionSpec) string {
	if spec.CertificateRef != "" {
		return "pubkey"
	}
	return "psk"
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
