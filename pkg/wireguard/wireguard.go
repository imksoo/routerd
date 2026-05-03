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

	"routerd/pkg/api"
)

type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type InterfaceConfig struct {
	Name       string
	PrivateKey string
	ListenPort int
	MTU        int
	FwMark     int
	Peers      []PeerConfig
}

type PeerConfig struct {
	Name                string
	PublicKey           string
	AllowedIPs          []string
	Endpoint            string
	PersistentKeepalive int
	PresharedKey        string
}

type PeerStatus struct {
	PublicKey       string
	LatestHandshake time.Time
	TransferRxBytes int64
	TransferTxBytes int64
	LatestEndpoint  string
}

type Controller struct {
	Command CommandRunner
	DryRun  bool
}

func BuildInterface(resource api.Resource, peers []api.Resource) (InterfaceConfig, error) {
	spec, err := resource.WireGuardInterfaceSpec()
	if err != nil {
		return InterfaceConfig{}, err
	}
	cfg := InterfaceConfig{
		Name:       resource.Metadata.Name,
		PrivateKey: spec.PrivateKey,
		ListenPort: spec.ListenPort,
		MTU:        spec.MTU,
		FwMark:     spec.FwMark,
	}
	for _, peer := range peers {
		peerSpec, err := peer.WireGuardPeerSpec()
		if err != nil {
			return InterfaceConfig{}, err
		}
		if peerSpec.Interface != resource.Metadata.Name {
			continue
		}
		cfg.Peers = append(cfg.Peers, PeerConfig{
			Name:                peer.Metadata.Name,
			PublicKey:           peerSpec.PublicKey,
			AllowedIPs:          append([]string(nil), peerSpec.AllowedIPs...),
			Endpoint:            peerSpec.Endpoint,
			PersistentKeepalive: peerSpec.PersistentKeepalive,
			PresharedKey:        peerSpec.PresharedKey,
		})
	}
	sort.SliceStable(cfg.Peers, func(i, j int) bool { return cfg.Peers[i].Name < cfg.Peers[j].Name })
	return cfg, nil
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

func ParseDump(data []byte) ([]PeerStatus, error) {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) <= 1 || strings.TrimSpace(string(data)) == "" {
		return nil, nil
	}
	var peers []PeerStatus
	for _, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) < 9 {
			return nil, fmt.Errorf("wg dump peer line has %d fields, want >=9", len(fields))
		}
		handshakeUnix, _ := strconv.ParseInt(fields[5], 10, 64)
		rx, _ := strconv.ParseInt(fields[6], 10, 64)
		tx, _ := strconv.ParseInt(fields[7], 10, 64)
		var handshake time.Time
		if handshakeUnix > 0 {
			handshake = time.Unix(handshakeUnix, 0).UTC()
		}
		peers = append(peers, PeerStatus{
			PublicKey:       fields[0],
			LatestEndpoint:  fields[3],
			LatestHandshake: handshake,
			TransferRxBytes: rx,
			TransferTxBytes: tx,
		})
	}
	return peers, nil
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
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
