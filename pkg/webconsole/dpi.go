// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observe"
)

func (h Handler) dpiStatus(ctx context.Context) *DPIStatus {
	classifier := probeDPIService(ctx, "/run/routerd/dpi-classifier/default.sock", "/v1/status")
	agent := probeDPIService(ctx, "/run/routerd/ndpi-agent/default.sock", "/v1/status")
	if classifier == nil && agent == nil {
		return nil
	}
	return &DPIStatus{Classifier: classifier, Agent: agent}
}

func probeDPIService(ctx context.Context, socket, path string) *DPIServiceStatus {
	if _, err := os.Stat(socket); err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	client := &http.Client{
		Timeout: 150 * time.Millisecond,
		Transport: &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socket)
		}},
	}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix"+path, nil)
	if err != nil {
		return &DPIServiceStatus{Socket: socket, Error: err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return &DPIServiceStatus{Socket: socket, Error: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return &DPIServiceStatus{Socket: socket, Error: resp.Status}
	}
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return &DPIServiceStatus{Socket: socket, Error: err.Error()}
	}
	return dpiServiceStatusFromMap(socket, raw)
}

func dpiServiceStatusFromMap(socket string, raw map[string]any) *DPIServiceStatus {
	status := &DPIServiceStatus{
		Available:      true,
		Socket:         socket,
		Engine:         stringMapValue(raw, "engine"),
		ActiveEngine:   stringMapValue(raw, "activeEngine"),
		LibNDPILoaded:  boolMapValue(raw, "libndpiLoaded"),
		LibNDPIVersion: stringMapValue(raw, "libndpiVersion"),
		Reason:         stringMapValue(raw, "reason"),
	}
	if stats, ok := raw["stats"].(map[string]any); ok {
		status.Stats = stats
	}
	if agent, ok := raw["agent"].(map[string]any); ok {
		if status.ActiveEngine == "" && boolMapValue(agent, "available") {
			status.ActiveEngine = "ndpi-agent"
		}
		if !boolMapValue(agent, "available") && status.Reason == "" {
			status.Reason = stringMapValue(agent, "error")
		}
	}
	return status
}

func (h Handler) enrichTrafficFlowsWithDPI(ctx context.Context, flows []logstore.TrafficFlow, now time.Time, ttl time.Duration) ([]logstore.TrafficFlow, error) {
	if err := ctx.Err(); err != nil {
		return flows, err
	}
	if len(flows) == 0 {
		return flows, nil
	}
	if strings.TrimSpace(h.opts.FirewallLogPath) == "" {
		for i := range flows {
			applyTrafficFlowPortFallback(&flows[i])
		}
		return flows, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	store, err := logstore.OpenFirewallLogReadOnly(h.opts.FirewallLogPath)
	if os.IsNotExist(err) {
		applyTrafficFlowListPortFallback(flows)
		return flows, nil
	}
	if err != nil {
		return flows, err
	}
	defer store.Close()
	entries := make([]logstore.FirewallLogEntry, len(flows))
	for i := range flows {
		entries[i] = logstore.FirewallLogEntry{
			Protocol:   flows[i].Protocol,
			SrcAddress: flows[i].ClientAddress,
			SrcPort:    flows[i].ClientPort,
			DstAddress: flows[i].PeerAddress,
			DstPort:    flows[i].PeerPort,
		}
	}
	matches, err := store.FindDPIFlowsForFirewallEntries(ctx, entries, now, ttl)
	if err != nil {
		return flows, err
	}
	for i := range flows {
		dpiFlow := matches[i]
		if dpiFlow == nil {
			applyTrafficFlowPortFallback(&flows[i])
			continue
		}
		if flows[i].AppName == "" {
			flows[i].AppName = dpiFlow.AppName
		}
		if flows[i].AppCategory == "" {
			flows[i].AppCategory = dpiFlow.AppCategory
		}
		if flows[i].AppConfidence == 0 {
			flows[i].AppConfidence = dpiFlow.AppConfidence
		}
		if flows[i].DetectedProtocol == "" {
			flows[i].DetectedProtocol = dpiFlow.DetectedProtocol
		}
		if flows[i].MasterProtocol == "" {
			flows[i].MasterProtocol = dpiFlow.MasterProtocol
		}
		if flows[i].ApplicationProtocol == "" {
			flows[i].ApplicationProtocol = dpiFlow.ApplicationProtocol
		}
		if flows[i].Category == "" {
			flows[i].Category = dpiFlow.Category
		}
		if len(flows[i].Risk) == 0 {
			flows[i].Risk = append([]string(nil), dpiFlow.Risk...)
		}
		if flows[i].Confidence == 0 {
			flows[i].Confidence = dpiFlow.Confidence
		}
		if len(flows[i].Metadata) == 0 && len(dpiFlow.Metadata) > 0 {
			flows[i].Metadata = map[string]string{}
			for key, value := range dpiFlow.Metadata {
				flows[i].Metadata[key] = value
			}
		}
		if flows[i].Engine == "" {
			flows[i].Engine = dpiFlow.Engine
		}
		if flows[i].Source == "" {
			flows[i].Source = dpiFlow.Source
		}
		if flows[i].TLSSNI == "" {
			flows[i].TLSSNI = dpiFlow.TLSSNI
		}
		if flows[i].HTTPHost == "" {
			flows[i].HTTPHost = dpiFlow.HTTPHost
		}
		if flows[i].DNSQuery == "" {
			flows[i].DNSQuery = dpiFlow.DNSQuery
		}
		if flows[i].ResolvedHostname == "" {
			flows[i].ResolvedHostname = stringutil.FirstNonBlank(dpiFlow.TLSSNI, dpiFlow.HTTPHost, dpiFlow.DNSQuery)
		}
		applyTrafficFlowPortFallback(&flows[i])
	}
	return flows, nil
}

func (h Handler) enrichConnectionsWithDPI(ctx context.Context, table *observe.ConnectionTable, now time.Time, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if table == nil || len(table.Entries) == 0 {
		return nil
	}
	if strings.TrimSpace(h.opts.FirewallLogPath) == "" {
		for i := range table.Entries {
			applyConnectionPortFallback(&table.Entries[i])
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	store, err := logstore.OpenFirewallLogReadOnly(h.opts.FirewallLogPath)
	if os.IsNotExist(err) {
		applyConnectionTablePortFallback(table)
		return nil
	}
	if err != nil {
		return err
	}
	defer store.Close()
	entries := make([]logstore.FirewallLogEntry, len(table.Entries))
	for i := range table.Entries {
		entry := &table.Entries[i]
		entries[i] = logstore.FirewallLogEntry{
			Protocol:   entry.Protocol,
			SrcAddress: entry.Original.Source,
			SrcPort:    atoiDefault(entry.Original.SourcePort, 0),
			DstAddress: entry.Original.Destination,
			DstPort:    atoiDefault(entry.Original.DestinationPort, 0),
		}
	}
	matches, err := store.FindDPIFlowsForFirewallEntries(ctx, entries, now, ttl)
	if err != nil {
		return err
	}
	for i := range table.Entries {
		entry := &table.Entries[i]
		flow := matches[i]
		if flow == nil {
			applyConnectionPortFallback(entry)
			continue
		}
		entry.AppName = flow.AppName
		entry.AppCategory = flow.AppCategory
		entry.AppConfidence = flow.AppConfidence
		entry.TLSSNI = flow.TLSSNI
		entry.HTTPHost = flow.HTTPHost
		entry.DNSQuery = flow.DNSQuery
		applyConnectionPortFallback(entry)
	}
	return nil
}
