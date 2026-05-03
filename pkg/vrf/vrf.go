package vrf

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
	Name       string
	IfName     string
	RouteTable int
	Members    []string
}

type Controller struct {
	Command CommandRunner
	DryRun  bool
}

func Commands(cfg Config) [][]string {
	ifname := defaultString(cfg.IfName, cfg.Name)
	args := [][]string{
		{"ip", "link", "add", ifname, "type", "vrf", "table", strconv.Itoa(cfg.RouteTable)},
		{"ip", "link", "set", "dev", ifname, "up"},
	}
	for _, member := range cfg.Members {
		args = append(args, []string{"ip", "link", "set", "dev", member, "master", ifname})
	}
	return args
}

func (c Controller) Apply(ctx context.Context, cfg Config) error {
	if cfg.RouteTable == 0 {
		return fmt.Errorf("route table is required")
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
	gauge, _ := otel.Meter("routerd.vrf").Int64Gauge("routerd.vrf.member.count")
	gauge.Record(ctx, int64(len(cfg.Members)), metric.WithAttributes(attribute.String("routerd.vrf.name", ifname)))
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
