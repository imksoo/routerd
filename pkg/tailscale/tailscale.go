// SPDX-License-Identifier: BSD-3-Clause

package tailscale

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type CommandFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

type Status struct {
	BackendState    string       `json:"backendState,omitempty" yaml:"backendState,omitempty"`
	TailnetName     string       `json:"tailnetName,omitempty" yaml:"tailnetName,omitempty"`
	MagicDNSSuffix  string       `json:"magicDNSSuffix,omitempty" yaml:"magicDNSSuffix,omitempty"`
	MagicDNSEnabled bool         `json:"magicDNSEnabled,omitempty" yaml:"magicDNSEnabled,omitempty"`
	CertDomains     []string     `json:"certDomains,omitempty" yaml:"certDomains,omitempty"`
	HostName        string       `json:"hostName,omitempty" yaml:"hostName,omitempty"`
	DNSName         string       `json:"dnsName,omitempty" yaml:"dnsName,omitempty"`
	TailscaleIPs    []string     `json:"tailscaleIPs,omitempty" yaml:"tailscaleIPs,omitempty"`
	AllowedIPs      []string     `json:"allowedIPs,omitempty" yaml:"allowedIPs,omitempty"`
	Online          bool         `json:"online,omitempty" yaml:"online,omitempty"`
	Active          bool         `json:"active,omitempty" yaml:"active,omitempty"`
	ExitNode        bool         `json:"exitNode,omitempty" yaml:"exitNode,omitempty"`
	ExitNodeOption  bool         `json:"exitNodeOption,omitempty" yaml:"exitNodeOption,omitempty"`
	Peers           []PeerStatus `json:"peers,omitempty" yaml:"peers,omitempty"`
}

type PeerStatus struct {
	ID             string   `json:"id,omitempty" yaml:"id,omitempty"`
	HostName       string   `json:"hostName,omitempty" yaml:"hostName,omitempty"`
	DNSName        string   `json:"dnsName,omitempty" yaml:"dnsName,omitempty"`
	TailscaleIPs   []string `json:"tailscaleIPs,omitempty" yaml:"tailscaleIPs,omitempty"`
	AllowedIPs     []string `json:"allowedIPs,omitempty" yaml:"allowedIPs,omitempty"`
	Online         bool     `json:"online,omitempty" yaml:"online,omitempty"`
	Active         bool     `json:"active,omitempty" yaml:"active,omitempty"`
	ExitNode       bool     `json:"exitNode,omitempty" yaml:"exitNode,omitempty"`
	ExitNodeOption bool     `json:"exitNodeOption,omitempty" yaml:"exitNodeOption,omitempty"`
	Relay          string   `json:"relay,omitempty" yaml:"relay,omitempty"`
	LastSeen       string   `json:"lastSeen,omitempty" yaml:"lastSeen,omitempty"`
	RxBytes        int64    `json:"rxBytes,omitempty" yaml:"rxBytes,omitempty"`
	TxBytes        int64    `json:"txBytes,omitempty" yaml:"txBytes,omitempty"`
}

type peerJSON struct {
	HostName       string   `json:"HostName"`
	DNSName        string   `json:"DNSName"`
	TailscaleIPs   []string `json:"TailscaleIPs"`
	AllowedIPs     []string `json:"AllowedIPs"`
	Online         bool     `json:"Online"`
	Active         bool     `json:"Active"`
	ExitNode       bool     `json:"ExitNode"`
	ExitNodeOption bool     `json:"ExitNodeOption"`
	Relay          string   `json:"Relay"`
	LastSeen       string   `json:"LastSeen"`
	RxBytes        int64    `json:"RxBytes"`
	TxBytes        int64    `json:"TxBytes"`
}

func Fetch(ctx context.Context, binary string, command CommandFunc) (Status, error) {
	if strings.TrimSpace(binary) == "" {
		binary = "tailscale"
	}
	out, err := command(ctx, binary, "status", "--json")
	if err != nil {
		return Status{}, err
	}
	return ParseStatusJSON(out)
}

func ParseStatusJSON(data []byte) (Status, error) {
	if strings.TrimSpace(string(data)) == "" {
		return Status{}, nil
	}
	var raw struct {
		BackendState   string `json:"BackendState"`
		CurrentTailnet struct {
			Name            string `json:"Name"`
			MagicDNSSuffix  string `json:"MagicDNSSuffix"`
			MagicDNSEnabled bool   `json:"MagicDNSEnabled"`
		} `json:"CurrentTailnet"`
		CertDomains []string            `json:"CertDomains"`
		Self        peerJSON            `json:"Self"`
		Peer        map[string]peerJSON `json:"Peer"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Status{}, err
	}
	status := Status{
		BackendState:    raw.BackendState,
		TailnetName:     raw.CurrentTailnet.Name,
		MagicDNSSuffix:  raw.CurrentTailnet.MagicDNSSuffix,
		MagicDNSEnabled: raw.CurrentTailnet.MagicDNSEnabled,
		CertDomains:     raw.CertDomains,
		HostName:        raw.Self.HostName,
		DNSName:         raw.Self.DNSName,
		TailscaleIPs:    raw.Self.TailscaleIPs,
		AllowedIPs:      raw.Self.AllowedIPs,
		Online:          raw.Self.Online,
		Active:          raw.Self.Active,
		ExitNode:        raw.Self.ExitNode,
		ExitNodeOption:  raw.Self.ExitNodeOption,
	}
	for id, peer := range raw.Peer {
		status.Peers = append(status.Peers, PeerStatus{
			ID:             id,
			HostName:       peer.HostName,
			DNSName:        peer.DNSName,
			TailscaleIPs:   peer.TailscaleIPs,
			AllowedIPs:     peer.AllowedIPs,
			Online:         peer.Online,
			Active:         peer.Active,
			ExitNode:       peer.ExitNode,
			ExitNodeOption: peer.ExitNodeOption,
			Relay:          peer.Relay,
			LastSeen:       peer.LastSeen,
			RxBytes:        peer.RxBytes,
			TxBytes:        peer.TxBytes,
		})
	}
	sort.Slice(status.Peers, func(i, j int) bool {
		left, right := status.Peers[i], status.Peers[j]
		if left.Online != right.Online {
			return left.Online
		}
		if left.Active != right.Active {
			return left.Active
		}
		if LastSeenAfter(left.LastSeen, right.LastSeen) {
			return true
		}
		if LastSeenAfter(right.LastSeen, left.LastSeen) {
			return false
		}
		return strings.ToLower(firstNonEmpty(left.HostName, left.DNSName, left.ID)) < strings.ToLower(firstNonEmpty(right.HostName, right.DNSName, right.ID))
	})
	return status, nil
}

func OnlinePeerCount(status Status) int {
	count := 0
	for _, peer := range status.Peers {
		if peer.Online {
			count++
		}
	}
	return count
}

func RecordMetrics(ctx context.Context, resourceName string, status Status, now time.Time) {
	meter := otel.Meter("routerd.tailscale")
	peerCount, _ := meter.Int64Gauge("routerd.tailscale.peer.count")
	handshakeAge, _ := meter.Int64Gauge("routerd.tailscale.last_handshake.seconds")
	attrs := metric.WithAttributes(attribute.String("routerd.tailscale.node", resourceName))
	peerCount.Record(ctx, int64(OnlinePeerCount(status)), attrs)
	for _, peer := range status.Peers {
		age := int64(0)
		if seen, err := time.Parse(time.RFC3339Nano, peer.LastSeen); err == nil && !seen.IsZero() {
			age = int64(now.Sub(seen).Seconds())
			if age < 0 {
				age = 0
			}
		}
		handshakeAge.Record(ctx, age, metric.WithAttributes(
			attribute.String("routerd.tailscale.node", resourceName),
			attribute.String("routerd.tailscale.peer", firstNonEmpty(peer.HostName, peer.DNSName, peer.ID)),
		))
	}
}

func LastSeenAfter(left, right string) bool {
	leftTime, leftErr := time.Parse(time.RFC3339Nano, left)
	rightTime, rightErr := time.Parse(time.RFC3339Nano, right)
	if leftErr != nil || rightErr != nil {
		return left != "" && right == ""
	}
	return leftTime.After(rightTime)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
