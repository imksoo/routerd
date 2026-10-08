// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/conntracktuning"
	"github.com/imksoo/routerd/pkg/logstore"
)

func (h Handler) connections(w http.ResponseWriter, r *http.Request) {
	if h.opts.Connections == nil {
		writeError(w, http.StatusNotImplemented, "connections observer is unavailable")
		return
	}
	table, err := h.opts.Connections(intQuery(r, "limit", h.opts.ConnectionsLimit))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.enrichConnectionsWithDPI(table, time.Now().UTC(), time.Hour); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.enrichConnectionsWithLocalRedirect(table)
	if err := h.enrichConnectionsWithRemoteIdentity(r.Context(), table); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, table)
}

func (h Handler) dnsQueries(w http.ResponseWriter, r *http.Request) {
	filter, err := buildConsoleDNSFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if r.URL.Query().Get("agg") == "1" {
		agg, err := h.queryLogAggregate(filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, agg)
		return
	}
	rows, err := h.queryLogList(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, rows)
}

func (h Handler) dnsQueriesAggregate(w http.ResponseWriter, r *http.Request) {
	filter, err := buildConsoleDNSFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	agg, err := h.queryLogAggregate(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, agg)
}

func (h Handler) trafficFlows(w http.ResponseWriter, r *http.Request) {
	filter, err := buildConsoleTrafficFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if r.URL.Query().Get("agg") == "1" {
		agg, err := h.trafficFlowAggregate(filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, agg)
		return
	}
	rows, err := h.trafficFlowList(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	queries, err := h.queryLogList(logstore.DNSQueryFilter{Since: filter.Since, Limit: 1000})
	if err == nil {
		rows = enrichTrafficFlowsWithDNS(rows, queries)
	}
	if enriched, err := h.enrichTrafficFlowsWithDPI(rows, time.Now().UTC(), time.Hour); err == nil {
		rows = enriched
	}
	writeJSON(w, rows)
}

func (h Handler) trafficFlowsAggregate(w http.ResponseWriter, r *http.Request) {
	filter, err := buildConsoleTrafficFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	agg, err := h.trafficFlowAggregate(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, agg)
}

func buildConsoleDNSFilter(r *http.Request) (logstore.DNSQueryFilter, error) {
	q := r.URL.Query()
	since := time.Now().Add(-time.Hour)
	if raw := strings.TrimSpace(q.Get("since")); raw != "" {
		if duration, err := parseConsoleDuration(raw); err == nil {
			since = time.Now().Add(-duration)
		}
	}
	var until time.Time
	if raw := strings.TrimSpace(q.Get("from")); raw != "" {
		t, err := parseConsoleAbsTime(raw)
		if err != nil {
			return logstore.DNSQueryFilter{}, fmt.Errorf("from: %w", err)
		}
		since = t
	}
	if raw := strings.TrimSpace(q.Get("until")); raw != "" {
		t, err := parseConsoleAbsTime(raw)
		if err != nil {
			return logstore.DNSQueryFilter{}, fmt.Errorf("until: %w", err)
		}
		until = t
	}
	if raw := strings.TrimSpace(q.Get("to")); raw != "" {
		t, err := parseConsoleAbsTime(raw)
		if err != nil {
			return logstore.DNSQueryFilter{}, fmt.Errorf("to: %w", err)
		}
		until = t
	}
	var durMinUS int64
	if raw := strings.TrimSpace(q.Get("duration-min")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return logstore.DNSQueryFilter{}, fmt.Errorf("duration-min: %w", err)
		}
		durMinUS = d.Microseconds()
	} else if raw := strings.TrimSpace(q.Get("duration-min-us")); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return logstore.DNSQueryFilter{}, fmt.Errorf("duration-min-us: %w", err)
		}
		durMinUS = v
	}
	return logstore.DNSQueryFilter{
		Since:         since,
		Until:         until,
		Client:        q.Get("client"),
		QName:         q.Get("qname"),
		QNameSuffix:   q.Get("qname-suffix"),
		ResponseCode:  q.Get("rcode"),
		Upstream:      q.Get("upstream"),
		DurationMinUS: durMinUS,
		Limit:         intQuery(r, "limit", 100),
	}, nil
}

func buildConsoleTrafficFilter(r *http.Request) (logstore.TrafficFlowFilter, error) {
	q := r.URL.Query()
	since := time.Now().Add(-time.Hour)
	if raw := strings.TrimSpace(q.Get("since")); raw != "" {
		if duration, err := parseConsoleDuration(raw); err == nil {
			since = time.Now().Add(-duration)
		}
	}
	var until time.Time
	if raw := strings.TrimSpace(q.Get("from")); raw != "" {
		t, err := parseConsoleAbsTime(raw)
		if err != nil {
			return logstore.TrafficFlowFilter{}, fmt.Errorf("from: %w", err)
		}
		since = t
	}
	if raw := strings.TrimSpace(q.Get("until")); raw != "" {
		t, err := parseConsoleAbsTime(raw)
		if err != nil {
			return logstore.TrafficFlowFilter{}, fmt.Errorf("until: %w", err)
		}
		until = t
	}
	if raw := strings.TrimSpace(q.Get("to")); raw != "" {
		t, err := parseConsoleAbsTime(raw)
		if err != nil {
			return logstore.TrafficFlowFilter{}, fmt.Errorf("to: %w", err)
		}
		until = t
	}
	asym := q.Get("asymmetric") == "1" || strings.EqualFold(q.Get("asymmetric"), "true")
	return logstore.TrafficFlowFilter{
		Since:      since,
		Until:      until,
		Client:     q.Get("client"),
		Peer:       q.Get("peer"),
		PeerSuffix: q.Get("peer-suffix"),
		Protocol:   q.Get("protocol"),
		Asymmetric: asym,
		Limit:      intQuery(r, "limit", 100),
	}, nil
}

func parseConsoleAbsTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse %q (expected RFC3339)", value)
}

func (h Handler) firewallLogs(w http.ResponseWriter, r *http.Request) {
	since := time.Now().Add(-24 * time.Hour)
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		if duration, err := parseConsoleDuration(raw); err == nil {
			since = time.Now().Add(-duration)
		}
	}
	rows, err := h.firewallLogList(logstore.FirewallLogFilter{
		Since:  since,
		Action: r.URL.Query().Get("action"),
		Src:    r.URL.Query().Get("src"),
		Limit:  intQuery(r, "limit", 100),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.enrichFirewallLogsWithRemoteIdentity(r.Context(), rows); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.enrichFirewallLogsWithAddressSets(rows)
	writeJSON(w, rows)
}

func (h Handler) firewallDenyTimeline(w http.ResponseWriter, r *http.Request) {
	window := 24 * time.Hour
	if raw := strings.TrimSpace(r.URL.Query().Get("range")); raw != "" {
		if duration, err := parseConsoleDuration(raw); err == nil {
			window = duration
		}
	}
	if window < time.Minute {
		window = time.Minute
	}
	if window > 7*24*time.Hour {
		window = 7 * 24 * time.Hour
	}
	bucket := 5 * time.Minute
	if raw := strings.TrimSpace(r.URL.Query().Get("bucket")); raw != "" {
		if duration, err := parseConsoleDuration(raw); err == nil {
			bucket = duration
		}
	}
	if bucket < time.Minute {
		bucket = time.Minute
	}
	if bucket > time.Hour {
		bucket = time.Hour
	}
	now := time.Now().UTC()
	rows, err := h.firewallDenyTimelineList(now.Add(-window), now, bucket)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rows == nil {
		rows = []logstore.FirewallDenyTimelineBucket{}
	}
	writeJSON(w, rows)
}

func (h Handler) queryLogList(filter logstore.DNSQueryFilter) ([]logstore.DNSQuery, error) {
	if strings.TrimSpace(h.opts.DNSQueryLogPath) == "" {
		return nil, nil
	}
	store, err := logstore.OpenDNSQueryLogReadOnly(h.opts.DNSQueryLogPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	return store.List(ctx, filter)
}

func (h Handler) queryLogAggregate(filter logstore.DNSQueryFilter) (logstore.DNSQueryAggregate, error) {
	if strings.TrimSpace(h.opts.DNSQueryLogPath) == "" {
		return logstore.DNSQueryAggregate{Since: filter.Since, Until: filter.Until}, nil
	}
	store, err := logstore.OpenDNSQueryLogReadOnly(h.opts.DNSQueryLogPath)
	if err != nil {
		return logstore.DNSQueryAggregate{}, err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.Aggregate(ctx, filter)
}

func (h Handler) trafficFlowList(filter logstore.TrafficFlowFilter) ([]logstore.TrafficFlow, error) {
	if strings.TrimSpace(h.opts.TrafficFlowLogPath) == "" {
		return nil, nil
	}
	store, err := logstore.OpenTrafficFlowLogReadOnly(h.opts.TrafficFlowLogPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	return store.List(ctx, filter)
}

func (h Handler) trafficFlowAggregate(filter logstore.TrafficFlowFilter) (logstore.TrafficFlowAggregate, error) {
	if strings.TrimSpace(h.opts.TrafficFlowLogPath) == "" {
		return logstore.TrafficFlowAggregate{Since: filter.Since, Until: filter.Until}, nil
	}
	store, err := logstore.OpenTrafficFlowLogReadOnly(h.opts.TrafficFlowLogPath)
	if err != nil {
		return logstore.TrafficFlowAggregate{}, err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.Aggregate(ctx, filter)
}

func (h Handler) firewallLogList(filter logstore.FirewallLogFilter) ([]logstore.FirewallLogEntry, error) {
	if strings.TrimSpace(h.opts.FirewallLogPath) == "" {
		return nil, nil
	}
	store, err := logstore.OpenFirewallLogReadOnly(h.opts.FirewallLogPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	return store.List(ctx, filter)
}

func (h Handler) firewallDenyTimelineList(since time.Time, until time.Time, bucket time.Duration) ([]logstore.FirewallDenyTimelineBucket, error) {
	if strings.TrimSpace(h.opts.FirewallLogPath) == "" {
		return nil, nil
	}
	store, err := logstore.OpenFirewallLog(h.opts.FirewallLogPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.DenyTimeline(context.Background(), since, until, bucket)
}

func (h Handler) conntrackTuningSummary(now time.Time, window time.Duration, autoApply bool) (conntracktuning.Summary, error) {
	if strings.TrimSpace(h.opts.FirewallLogPath) == "" {
		return conntracktuning.Analyze(conntracktuning.Inputs{Now: now, Window: window, AutoApply: autoApply}), nil
	}
	store, err := logstore.OpenFirewallLog(h.opts.FirewallLogPath)
	if err != nil {
		return conntracktuning.Summary{}, err
	}
	defer store.Close()
	since := now.Add(-window)
	firewallLogs, err := store.List(context.Background(), logstore.FirewallLogFilter{Since: since, Limit: 1000})
	if err != nil {
		return conntracktuning.Summary{}, err
	}
	dpiFlows, err := store.ListDPIFlows(context.Background(), logstore.DPIFlowFilter{Since: since, Limit: 5000})
	if err != nil {
		return conntracktuning.Summary{}, err
	}
	expiredFlows, err := store.ListExpiredFlows(context.Background(), logstore.ExpiredFlowFilter{Since: since, Limit: 5000})
	if err != nil {
		return conntracktuning.Summary{}, err
	}
	return conntracktuning.Analyze(conntracktuning.Inputs{
		DPIFlows:     dpiFlows,
		FirewallLogs: firewallLogs,
		ExpiredFlows: expiredFlows,
		Now:          now,
		Window:       window,
		AutoApply:    autoApply,
	}), nil
}

func enrichTrafficFlowsWithDNS(flows []logstore.TrafficFlow, queries []logstore.DNSQuery) []logstore.TrafficFlow {
	if len(flows) == 0 || len(queries) == 0 {
		return flows
	}
	labels := map[string]string{}
	for _, query := range queries {
		name := strings.TrimSuffix(query.QuestionName, ".")
		if name == "" {
			continue
		}
		for _, answer := range query.Answers {
			answer = strings.TrimSpace(answer)
			if answer == "" {
				continue
			}
			if _, exists := labels[answer]; !exists {
				labels[answer] = name
			}
		}
	}
	for i := range flows {
		if strings.TrimSpace(flows[i].ResolvedHostname) == "" {
			flows[i].ResolvedHostname = labels[flows[i].PeerAddress]
		}
	}
	return flows
}
