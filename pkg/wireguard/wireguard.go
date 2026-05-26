// SPDX-License-Identifier: BSD-3-Clause

package wireguard

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/imksoo/routerd/internal/hostcmd"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type InterfaceConfig struct {
	Name           string
	PrivateKey     string
	PrivateKeyFile string
	ListenPort     int
	MTU            int
	FwMark         int
	Peers          []PeerConfig
}

type PeerConfig struct {
	Name                string
	PublicKey           string
	AllowedIPs          []string
	Endpoint            string
	PersistentKeepalive int
	PresharedKey        string
	PresharedKeyFile    string
}

type PeerStatus struct {
	PublicKey           string
	AllowedIPs          []string
	LatestHandshake     time.Time
	TransferRxBytes     int64
	TransferTxBytes     int64
	LatestEndpoint      string
	PersistentKeepalive int
}

type InterfaceStatus struct {
	Name       string
	PublicKey  string
	ListenPort int
	FwMark     string
	Peers      []PeerStatus
}

type Controller struct {
	Command CommandRunner
	DryRun  bool
}

func DefaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return runCommand(ctx, name, args...)
}

func BuildInterface(resource api.Resource, peers []api.Resource) (InterfaceConfig, error) {
	spec, err := resource.WireGuardInterfaceSpec()
	if err != nil {
		return InterfaceConfig{}, err
	}
	cfg := InterfaceConfig{
		Name:           resource.Metadata.Name,
		PrivateKey:     spec.PrivateKey,
		PrivateKeyFile: spec.PrivateKeyFile,
		ListenPort:     spec.ListenPort,
		MTU:            spec.MTU,
		FwMark:         spec.FwMark,
	}
	for _, peer := range peers {
		if peer.Kind != "WireGuardPeer" {
			continue
		}
		peerSpec, err := peer.WireGuardPeerSpec()
		if err != nil {
			return InterfaceConfig{}, err
		}
		if peerSpec.Interface != resource.Metadata.Name {
			continue
		}
		if !PeerSpecConfigured(peerSpec) {
			continue
		}
		cfg.Peers = append(cfg.Peers, PeerConfig{
			Name:                peer.Metadata.Name,
			PublicKey:           peerSpec.PublicKey,
			AllowedIPs:          append([]string(nil), peerSpec.AllowedIPs...),
			Endpoint:            peerSpec.Endpoint,
			PersistentKeepalive: peerSpec.PersistentKeepalive,
			PresharedKey:        peerSpec.PresharedKey,
			PresharedKeyFile:    peerSpec.PresharedKeyFile,
		})
	}
	sort.SliceStable(cfg.Peers, func(i, j int) bool { return cfg.Peers[i].Name < cfg.Peers[j].Name })
	return cfg, nil
}

func PeerSpecConfigured(spec api.WireGuardPeerSpec) bool {
	return strings.TrimSpace(spec.PublicKey) != "" ||
		len(spec.AllowedIPs) > 0 ||
		strings.TrimSpace(spec.Endpoint) != "" ||
		spec.PersistentKeepalive != 0 ||
		strings.TrimSpace(spec.PresharedKey) != "" ||
		strings.TrimSpace(spec.PresharedKeyFile) != ""
}

func RenderSetConf(cfg InterfaceConfig) ([]byte, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("interface name is required")
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "[Interface]\n")
	if cfg.PrivateKey != "" {
		fmt.Fprintf(&out, "PrivateKey = %s\n", cfg.PrivateKey)
	}
	if cfg.ListenPort != 0 {
		fmt.Fprintf(&out, "ListenPort = %d\n", cfg.ListenPort)
	}
	if cfg.FwMark != 0 {
		fmt.Fprintf(&out, "FwMark = %d\n", cfg.FwMark)
	}
	for _, peer := range cfg.Peers {
		if peer.PublicKey == "" {
			return nil, fmt.Errorf("peer %s public key is required", peer.Name)
		}
		if len(peer.AllowedIPs) == 0 {
			return nil, fmt.Errorf("peer %s allowedIPs is required", peer.Name)
		}
		fmt.Fprintf(&out, "\n[Peer]\n")
		fmt.Fprintf(&out, "PublicKey = %s\n", peer.PublicKey)
		if peer.PresharedKey != "" {
			fmt.Fprintf(&out, "PresharedKey = %s\n", peer.PresharedKey)
		}
		fmt.Fprintf(&out, "AllowedIPs = %s\n", strings.Join(peer.AllowedIPs, ", "))
		if peer.Endpoint != "" {
			fmt.Fprintf(&out, "Endpoint = %s\n", peer.Endpoint)
		}
		if peer.PersistentKeepalive != 0 {
			fmt.Fprintf(&out, "PersistentKeepalive = %d\n", peer.PersistentKeepalive)
		}
	}
	return out.Bytes(), nil
}

func (c Controller) Apply(ctx context.Context, cfg InterfaceConfig) ([]byte, error) {
	resolved, err := ResolveKeyFiles(cfg)
	if err != nil {
		if !c.DryRun {
			return nil, err
		}
		resolved = cfg
		if resolved.PrivateKey == "" && resolved.PrivateKeyFile != "" {
			resolved.PrivateKey = "REDACTED_FROM_FILE"
		}
		for i := range resolved.Peers {
			if resolved.Peers[i].PresharedKey == "" && resolved.Peers[i].PresharedKeyFile != "" {
				resolved.Peers[i].PresharedKey = "REDACTED_FROM_FILE"
			}
		}
	}
	cfg = resolved
	conf, err := RenderSetConf(cfg)
	if err != nil {
		return nil, err
	}
	if c.DryRun {
		return conf, nil
	}
	run := c.Command
	if run == nil {
		run = runCommand
	}
	return applyWithCommands(ctx, run, cfg, conf)
}

func ResolveKeyFiles(cfg InterfaceConfig) (InterfaceConfig, error) {
	if cfg.PrivateKey == "" && cfg.PrivateKeyFile != "" {
		value, err := readSecretFile(cfg.PrivateKeyFile)
		if err != nil {
			return cfg, fmt.Errorf("read WireGuard private key file %s: %w", cfg.PrivateKeyFile, err)
		}
		cfg.PrivateKey = value
	}
	for i := range cfg.Peers {
		if cfg.Peers[i].PresharedKey != "" || cfg.Peers[i].PresharedKeyFile == "" {
			continue
		}
		value, err := readSecretFile(cfg.Peers[i].PresharedKeyFile)
		if err != nil {
			return cfg, fmt.Errorf("read WireGuard preshared key file %s: %w", cfg.Peers[i].PresharedKeyFile, err)
		}
		cfg.Peers[i].PresharedKey = value
	}
	return cfg, nil
}

func readSecretFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", fmt.Errorf("empty key file")
	}
	return value, nil
}

func applyWithCommands(ctx context.Context, run CommandRunner, cfg InterfaceConfig, conf []byte) ([]byte, error) {
	if platform.CurrentOS() == platform.OSFreeBSD {
		return applyFreeBSD(ctx, run, cfg, conf)
	}
	return applyLinux(ctx, run, cfg, conf)
}

func applyLinux(ctx context.Context, run CommandRunner, cfg InterfaceConfig, conf []byte) ([]byte, error) {
	if _, err := run(ctx, "ip", "link", "show", cfg.Name); err != nil {
		if _, err := run(ctx, "ip", "link", "add", "dev", cfg.Name, "type", "wireguard"); err != nil {
			return nil, err
		}
	}
	if cfg.MTU != 0 {
		if _, err := run(ctx, "ip", "link", "set", "dev", cfg.Name, "mtu", strconv.Itoa(cfg.MTU)); err != nil {
			return nil, err
		}
	}
	file, err := os.CreateTemp("", "routerd-wg-*.conf")
	if err != nil {
		return nil, err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(conf); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "wg", "setconf", cfg.Name, file.Name()); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "ip", "link", "set", "up", "dev", cfg.Name); err != nil {
		return nil, err
	}
	return conf, nil
}

func applyFreeBSD(ctx context.Context, run CommandRunner, cfg InterfaceConfig, conf []byte) ([]byte, error) {
	_, _ = run(ctx, "kldload", "if_wg")
	if _, err := run(ctx, "ifconfig", cfg.Name); err != nil {
		if _, err := run(ctx, "ifconfig", cfg.Name, "create"); err != nil {
			if _, err := run(ctx, "ifconfig", "wg", "create", "name", cfg.Name); err != nil {
				return nil, err
			}
		}
	}
	file, err := os.CreateTemp("", "routerd-wg-*.conf")
	if err != nil {
		return nil, err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(conf); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	if _, err := run(ctx, "wg", "setconf", cfg.Name, file.Name()); err != nil {
		return nil, err
	}
	if cfg.MTU != 0 {
		if _, err := run(ctx, "ifconfig", cfg.Name, "mtu", strconv.Itoa(cfg.MTU)); err != nil {
			return nil, err
		}
	}
	if _, err := run(ctx, "ifconfig", cfg.Name, "up"); err != nil {
		return nil, err
	}
	return conf, nil
}

func ParseDump(data []byte) ([]PeerStatus, error) {
	status, err := ParseInterfaceDump("", data)
	if err != nil {
		return nil, err
	}
	return status.Peers, nil
}

func ParseInterfaceDump(name string, data []byte) (InterfaceStatus, error) {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) <= 1 || strings.TrimSpace(string(data)) == "" {
		return InterfaceStatus{Name: name}, nil
	}
	status := InterfaceStatus{Name: name}
	interfaceFields := strings.Split(lines[0], "\t")
	if len(interfaceFields) >= 4 {
		status.PublicKey = wireGuardDumpValue(interfaceFields[1])
		status.ListenPort = parseInt(interfaceFields[2])
		status.FwMark = wireGuardDumpValue(interfaceFields[3])
	}
	var peers []PeerStatus
	for _, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) < 8 {
			return InterfaceStatus{Name: name}, fmt.Errorf("wg dump peer line has %d fields, want >=8", len(fields))
		}
		handshakeUnix, _ := strconv.ParseInt(fields[4], 10, 64)
		rx, _ := strconv.ParseInt(fields[5], 10, 64)
		tx, _ := strconv.ParseInt(fields[6], 10, 64)
		var handshake time.Time
		if handshakeUnix > 0 {
			handshake = time.Unix(handshakeUnix, 0).UTC()
		}
		peers = append(peers, PeerStatus{
			PublicKey:           wireGuardDumpValue(fields[0]),
			AllowedIPs:          parseDumpAllowedIPs(fields[3]),
			LatestEndpoint:      wireGuardDumpValue(fields[2]),
			LatestHandshake:     handshake,
			TransferRxBytes:     rx,
			TransferTxBytes:     tx,
			PersistentKeepalive: parseInt(fields[7]),
		})
	}
	status.Peers = peers
	return status, nil
}

func ParseAllDump(data []byte) ([]InterfaceStatus, error) {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil, nil
	}
	interfaces := map[string]*InterfaceStatus{}
	ensure := func(name string) *InterfaceStatus {
		status := interfaces[name]
		if status == nil {
			status = &InterfaceStatus{Name: name}
			interfaces[name] = status
		}
		return status
	}
	for lineNo, line := range strings.Split(text, "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		switch {
		case len(fields) == 5:
			status := ensure(fields[0])
			status.PublicKey = wireGuardDumpValue(fields[2])
			status.ListenPort = parseInt(fields[3])
			status.FwMark = wireGuardDumpValue(fields[4])
		case len(fields) >= 9:
			status := ensure(fields[0])
			handshakeUnix, _ := strconv.ParseInt(fields[5], 10, 64)
			rx, _ := strconv.ParseInt(fields[6], 10, 64)
			tx, _ := strconv.ParseInt(fields[7], 10, 64)
			var handshake time.Time
			if handshakeUnix > 0 {
				handshake = time.Unix(handshakeUnix, 0).UTC()
			}
			status.Peers = append(status.Peers, PeerStatus{
				PublicKey:           wireGuardDumpValue(fields[1]),
				AllowedIPs:          parseDumpAllowedIPs(fields[4]),
				LatestEndpoint:      wireGuardDumpValue(fields[3]),
				LatestHandshake:     handshake,
				TransferRxBytes:     rx,
				TransferTxBytes:     tx,
				PersistentKeepalive: parseInt(fields[8]),
			})
		default:
			return nil, fmt.Errorf("wg dump line %d has %d fields", lineNo+1, len(fields))
		}
	}
	out := make([]InterfaceStatus, 0, len(interfaces))
	for _, status := range interfaces {
		sort.Slice(status.Peers, func(i, j int) bool { return status.Peers[i].PublicKey < status.Peers[j].PublicKey })
		out = append(out, *status)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func wireGuardDumpValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "(none)" || value == "off" {
		return ""
	}
	return value
}

func parseInt(value string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(value))
	return n
}

func parseDumpAllowedIPs(value string) []string {
	value = wireGuardDumpValue(value)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	sort.Strings(out)
	return out
}

func RecordPeerMetrics(ctx context.Context, iface string, peers []PeerStatus) {
	meter := otel.Meter("routerd.wireguard")
	handshake, _ := meter.Int64Gauge("routerd.wireguard.peer.handshake.timestamp")
	transfer, _ := meter.Int64Counter("routerd.wireguard.transfer.bytes")
	for _, peer := range peers {
		attrs := metric.WithAttributes(
			attribute.String("routerd.wireguard.interface", iface),
			attribute.String("routerd.wireguard.peer.public_key", peer.PublicKey),
		)
		if !peer.LatestHandshake.IsZero() {
			handshake.Record(ctx, peer.LatestHandshake.Unix(), attrs)
		}
		if peer.TransferRxBytes > 0 {
			transfer.Add(ctx, peer.TransferRxBytes, metric.WithAttributes(attribute.String("direction", "rx"), attribute.String("routerd.wireguard.interface", iface), attribute.String("routerd.wireguard.peer.public_key", peer.PublicKey)))
		}
		if peer.TransferTxBytes > 0 {
			transfer.Add(ctx, peer.TransferTxBytes, metric.WithAttributes(attribute.String("direction", "tx"), attribute.String("routerd.wireguard.interface", iface), attribute.String("routerd.wireguard.peer.public_key", peer.PublicKey)))
		}
	}
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, hostcmd.Resolve(name), args...).CombinedOutput()
}
