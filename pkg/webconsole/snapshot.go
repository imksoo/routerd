// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"net/http"
	"time"

	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/conntracktuning"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observe"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func (h Handler) Snapshot(opts SnapshotOptions) Snapshot {
	return h.snapshot(context.Background(), opts)
}

func (h Handler) snapshot(ctx context.Context, opts SnapshotOptions) Snapshot {
	if opts.EventLimit == 0 {
		opts.EventLimit = 50
	}
	if opts.FirewallLimit == 0 {
		opts.FirewallLimit = 50
	}
	if opts.DNSQueryLimit == 0 {
		opts.DNSQueryLimit = 50
	}
	if opts.TrafficFlowLimit == 0 {
		opts.TrafficFlowLimit = -1
	}
	if opts.FingerprintQueryLimit <= 0 {
		opts.FingerprintQueryLimit = opts.DNSQueryLimit
	}
	if opts.DHCPFingerprintLimit <= 0 {
		opts.DHCPFingerprintLimit = 200
	}
	now := time.Now().UTC()
	clientSince := now.Add(-clientObservationWindow)
	var errors []string
	var err error
	var resources []routerstate.ObjectStatus
	if !opts.SkipResources {
		resources, err = h.resourceStatuses()
		if err != nil {
			errors = append(errors, err.Error())
		}
	}
	var events []routerstate.StoredEvent
	if opts.EventLimit >= 0 {
		events, err = h.eventList(opts.EventLimit)
		if err != nil {
			errors = append(errors, err.Error())
		}
	}
	var connections *observe.ConnectionTable
	if h.opts.Connections != nil && opts.ConnectionsLimit >= 0 {
		connections, err = h.opts.Connections(opts.ConnectionsLimit)
		if err != nil {
			errors = append(errors, err.Error())
		} else if opts.IncludeDPIEnrichment {
			if err := h.enrichConnectionsWithDPI(ctx, connections, now, clientObservationWindow); err != nil {
				errors = append(errors, err.Error())
			}
		} else {
			applyConnectionTablePortFallback(connections)
		}
		h.enrichConnectionsWithLocalRedirect(connections)
		if err := h.enrichConnectionsWithRemoteIdentity(ctx, connections); err != nil {
			errors = append(errors, err.Error())
		}
	}
	var dnsQueries []logstore.DNSQuery
	if opts.DNSQueryLimit >= 0 {
		dnsQueries, err = h.queryLogList(ctx, logstore.DNSQueryFilter{Since: clientSince, Limit: opts.DNSQueryLimit})
		if err != nil {
			errors = append(errors, err.Error())
		}
	}
	fingerprintDNSQueries := dnsQueries
	if opts.IncludeClients && opts.FingerprintQueryLimit > opts.DNSQueryLimit {
		if queries, err := h.queryLogList(ctx, logstore.DNSQueryFilter{Since: clientSince, Limit: opts.FingerprintQueryLimit}); err == nil {
			fingerprintDNSQueries = queries
		} else {
			errors = append(errors, err.Error())
		}
	}
	var trafficFlows []logstore.TrafficFlow
	if opts.TrafficFlowLimit >= 0 {
		trafficFlows, err = h.trafficFlowList(ctx, logstore.TrafficFlowFilter{Since: clientSince, Limit: opts.TrafficFlowLimit})
		if err != nil {
			errors = append(errors, err.Error())
		}
		trafficFlows = enrichTrafficFlowsWithDNS(trafficFlows, dnsQueries)
		if opts.IncludeDPIEnrichment {
			if enriched, err := h.enrichTrafficFlowsWithDPI(ctx, trafficFlows, now, clientObservationWindow); err == nil {
				trafficFlows = enriched
			} else {
				errors = append(errors, err.Error())
			}
		} else {
			applyTrafficFlowListPortFallback(trafficFlows)
		}
	}
	var firewallLogs []logstore.FirewallLogEntry
	if opts.FirewallLimit >= 0 {
		firewallSince := now.Add(-24 * time.Hour)
		if opts.IncludeClients {
			firewallSince = clientSince
		}
		firewallLogs, err = h.firewallLogList(ctx, logstore.FirewallLogFilter{Since: firewallSince, Action: "drop", Limit: opts.FirewallLimit})
		if err != nil {
			errors = append(errors, err.Error())
		}
		if err := h.enrichFirewallLogsWithRemoteIdentity(ctx, firewallLogs); err != nil {
			errors = append(errors, err.Error())
		}
		h.enrichFirewallLogsWithAddressSets(firewallLogs)
	}
	var conntrackTuning *conntracktuning.Summary
	if opts.IncludeConntrackTuning {
		tuning, err := h.conntrackTuningSummary(time.Now().UTC(), 24*time.Hour, h.opts.Router != nil && h.opts.Router.Spec.Apply.AutoTuneConntrack)
		if err != nil {
			errors = append(errors, err.Error())
		} else {
			conntrackTuning = &tuning
		}
	}
	var dhcpLeases []DHCPLease
	var stickyLeases []logstore.DHCPStickyLease
	if !opts.SkipDHCPLeases || opts.IncludeClients {
		dhcpLeases, err = h.dhcpLeaseList()
		if err != nil {
			errors = append(errors, err.Error())
		}
		stickyLeases, err = h.dhcpStickyLeaseList(logstore.DHCPStickyFilter{HeldOnly: true, Now: now, Limit: 10000})
		if err != nil {
			errors = append(errors, err.Error())
		}
		dhcpLeases = annotateDHCPLeasesWithSticky(dhcpLeases, stickyLeases, now)
	}
	var dhcpFingerprints []logstore.DHCPFingerprint
	var neighbors []NeighborEntry
	var clientFirewallLogs []logstore.FirewallLogEntry
	var clients []ClientEntry
	if opts.IncludeClients {
		dhcpFingerprints, err = h.dhcpFingerprintList(logstore.DHCPFingerprintFilter{Since: clientSince, Limit: opts.DHCPFingerprintLimit})
		if err != nil {
			errors = append(errors, err.Error())
		}
		if opts.FirewallLimit < 0 {
			clientFirewallLogs, err = h.firewallLogList(ctx, logstore.FirewallLogFilter{Since: clientSince, Action: "drop", Limit: 1000})
			if err != nil {
				errors = append(errors, err.Error())
			}
		} else {
			clientFirewallLogs = firewallLogs
		}
		neighbors, err = neighborList()
		if err != nil {
			errors = append(errors, err.Error())
		}
		clients = h.annotateClientsWithPolicy(correlateClients(dhcpLeases, neighbors, trafficFlows, fingerprintDNSQueries, clientFirewallLogs, dhcpFingerprints))
	}
	var vpn VPNStatus
	if opts.IncludeVPN {
		vpn, err = h.vpnStatus()
		if err != nil {
			errors = append(errors, err.Error())
		}
		errors = append(errors, vpn.Errors...)
	}
	result := (*apply.Result)(nil)
	if h.opts.Result != nil {
		result = h.opts.Result()
	}
	dpiStatus := h.dpiStatus(ctx)
	systemUsage := h.readSystemUsage()
	result = resultWithLatestGeneration(result, h.opts.Store)
	controllers := h.controllerStatuses()
	recordConsoleMetrics(ctx, resources, controllers, dhcpLeases, clients, stickyLeases, now)
	return Snapshot{
		GeneratedAt:      now,
		ConsoleLinks:     cleanConsoleLinks(h.opts.ConsoleLinks),
		Status:           statusWithControllers(result, controllers),
		Controllers:      controllers,
		GatewayHealth:    gatewayHealth(resources),
		Phases:           phaseCounts(resources),
		Resources:        resources,
		Interfaces:       h.interfaceSummaries(resources),
		Events:           events,
		Connections:      connections,
		DNSQueries:       dnsQueries,
		TrafficFlows:     trafficFlows,
		FirewallLogs:     firewallLogs,
		ConntrackTuning:  conntrackTuning,
		DHCPFingerprints: dhcpFingerprints,
		DHCPLeases:       dhcpLeases,
		Neighbors:        neighbors,
		Clients:          clients,
		VPN:              vpn,
		DPI:              dpiStatus,
		SystemUsage:      systemUsage,
		Errors:           errors,
	}
}

func statusWithControllers(result *apply.Result, controllers []controlapi.ControllerStatus) controlapi.Status {
	status := controlapi.NewStatus(result)
	status.Status.Controllers = controllers
	return status
}

func resultWithLatestGeneration(result *apply.Result, store routerstate.Store) *apply.Result {
	if store == nil {
		return result
	}
	reader, ok := store.(routerstate.LatestGenerationReader)
	if !ok {
		return result
	}
	generation := reader.LatestGeneration()
	if generation == 0 {
		return result
	}
	if result == nil {
		return &apply.Result{Generation: generation}
	}
	next := *result
	next.Generation = generation
	return &next
}

func (h Handler) summary(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.snapshot(r.Context(), SnapshotOptions{
		EventLimit:             signedIntQuery(r, "events", 50),
		ConnectionsLimit:       signedIntQuery(r, "connections", h.opts.ConnectionsLimit),
		FirewallLimit:          signedIntQuery(r, "firewallLogs", 50),
		DNSQueryLimit:          signedIntQuery(r, "dnsQueries", 50),
		TrafficFlowLimit:       signedIntQuery(r, "trafficFlows", 50),
		FingerprintQueryLimit:  intQuery(r, "fingerprintQueries", 1000),
		DHCPFingerprintLimit:   intQuery(r, "dhcpFingerprints", 1000),
		IncludeDPIEnrichment:   boolQuery(r, "dpi", false),
		IncludeClients:         boolQuery(r, "clients", false),
		IncludeConntrackTuning: boolQuery(r, "tuning", false),
		IncludeVPN:             boolQuery(r, "vpn", true),
		SkipResources:          !boolQuery(r, "resources", true),
		SkipDHCPLeases:         !boolQuery(r, "dhcpLeases", true),
	}))
}

func (h Handler) resources(w http.ResponseWriter) {
	resources, err := h.resourceStatuses()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, resources)
}
