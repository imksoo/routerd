package vxlan

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type Config struct {
	Name              string
	IfName            string
	VNI               int
	LocalAddress      string
	Peers             []string
	UnderlayInterface string
	UDPPort           int
	MTU               int
	Bridge            string
}

type Controller struct {
	Command CommandRunner
	DryRun  bool
}

func Commands(cfg Config) [][]string {
	ifname := defaultString(cfg.IfName, cfg.Name)
	port := cfg.UDPPort
	if port == 0 {
		port = 4789
	}
	args := [][]string{{
		"ip", "link", "add", ifname,
		"type", "vxlan",
		"id", strconv.Itoa(cfg.VNI),
		"local", cfg.LocalAddress,
		"dev", cfg.UnderlayInterface,
		"dstport", strconv.Itoa(port),
		"nolearning",
	}}
	if cfg.MTU != 0 {
		args = append(args, []string{"ip", "link", "set", "dev", ifname, "mtu", strconv.Itoa(cfg.MTU)})
	}
	if cfg.Bridge != "" {
		args = append(args, []string{"ip", "link", "set", "dev", ifname, "master", cfg.Bridge})
	}
	args = append(args, []string{"ip", "link", "set", "dev", ifname, "up"})
	for _, peer := range cfg.Peers {
		args = append(args, []string{"bridge", "fdb", "append", "00:00:00:00:00:00", "dev", ifname, "dst", peer})
	}
	return args
}

func (c Controller) Apply(ctx context.Context, cfg Config) error {
	if cfg.VNI == 0 {
		return fmt.Errorf("vni is required")
	}
	if c.DryRun {
		RecordMetrics(ctx, cfg)
		return nil
	}
	run := c.Command
	if run == nil {
		run = runCommand
	}
	for _, cmd := range Commands(cfg) {
		if _, err := run(ctx, cmd[0], cmd[1:]...); err != nil {
			return err
		}
	}
	RecordMetrics(ctx, cfg)
	return nil
}

func RecordMetrics(ctx context.Context, cfg Config) {
	ifname := defaultString(cfg.IfName, cfg.Name)
	gauge, _ := otel.Meter("routerd.vxlan").Int64Gauge("routerd.vxlan.peers.count")
	gauge.Record(ctx, int64(len(cfg.Peers)), metric.WithAttributes(attribute.String("routerd.vxlan.interface", ifname), attribute.Int("routerd.vxlan.vni", cfg.VNI)))
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
