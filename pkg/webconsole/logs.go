// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"fmt"
	"net/http"
	"os"
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
	table, err := h.opts.Connections(r.Context(), intQuery(r, "limit", h.opts.ConnectionsLimit))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.enrichConnectionsWithDPI(r.Context(), table, time.Now().UTC(), time.Hour); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.enrichConnectionsWithLocalRedirect(table); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
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
		agg, err := h.queryLogAggregate(r.Context(), filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, agg)
		return
	}
	rows, err := h.queryLogList(r.Context(), filter)
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
	agg, err := h.queryLogAggregate(r.Context(), filter)
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
		agg, err := h.trafficFlowAggregate(r.Context(), filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, agg)
		return
	}
	rows, err := h.trafficFlowList(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	queries, err := h.queryLogList(r.Context(), logstore.DNSQueryFilter{Since: filter.Since, Limit: 1000})
	if err == nil {
		rows = enrichTrafficFlowsWithDNS(rows, queries)
	}
	if enriched, err := h.enrichTrafficFlowsWithDPI(r.Context(), rows, time.Now().UTC(), time.Hour); err == nil {
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
	agg, err := h.trafficFlowAggregate(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, agg)
}

func buildConsoleDNSFilter(r *http.Request) (logstore.DNSQueryFilter, error) {
	q := r.URL.Query()
	since, until, err := consoleTimeRange(r, time.Hour)
	if err != nil {
		return logstore.DNSQueryFilter{}, err
	}
	var durMinUS int64
	if raw := strings.TrimSpace(q.Get("duration-min")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return logstore.DNSQueryFilter{}, fmt.Errorf("duration-min: %w", err)
		}
		if d < 0 {
			return logstore.DNSQueryFilter{}, fmt.Errorf("duration-min must not be negative")
		}
		durMinUS = d.Microseconds()
	} else if raw := strings.TrimSpace(q.Get("duration-min-us")); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return logstore.DNSQueryFilter{}, fmt.Errorf("duration-min-us: %w", err)
		}
		if v < 0 {
			return logstore.DNSQueryFilter{}, fmt.Errorf("duration-min-us must not be negative")
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
	since, until, err := consoleTimeRange(r, time.Hour)
	if err != nil {
		return logstore.TrafficFlowFilter{}, err
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

func consoleTimeRange(r *http.Request, fallback time.Duration) (time.Time, time.Time, error) {
	window, err := consoleDurationQuery(r, "since", fallback)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	since := time.Now().Add(-window)
	var until time.Time
	for _, key := range []string{"from", "until", "to"} {
		raw := strings.TrimSpace(r.URL.Query().Get(key))
		if raw == "" {
			continue
		}
		parsed, err := parseConsoleAbsTime(raw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("%s: %w", key, err)
		}
		if key == "from" {
			since = parsed
		} else {
			until = parsed
		}
	}
	if !until.IsZero() && until.Before(since) {
		return time.Time{}, time.Time{}, fmt.Errorf("until must not be before from")
	}
	return since, until, nil
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
	window, err := consoleDurationQuery(r, "since", 24*time.Hour)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	since := time.Now().Add(-window)
	rows, err := h.firewallLogList(r.Context(), logstore.FirewallLogFilter{
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
	if err := h.enrichFirewallLogsWithAddressSets(rows); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, rows)
}

func (h Handler) firewallDenyTimeline(w http.ResponseWriter, r *http.Request) {
	window, err := consoleDurationQuery(r, "range", 24*time.Hour)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if window < time.Minute {
		window = time.Minute
	}
	if window > 7*24*time.Hour {
		window = 7 * 24 * time.Hour
	}
	bucket, err := consoleDurationQuery(r, "bucket", 5*time.Minute)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if bucket < time.Minute {
		bucket = time.Minute
	}
	if bucket > time.Hour {
		bucket = time.Hour
	}
	now := time.Now().UTC()
	rows, err := h.firewallDenyTimelineList(r.Context(), now.Add(-window), now, bucket)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rows == nil {
		rows = []logstore.FirewallDenyTimelineBucket{}
	}
	writeJSON(w, rows)
}

func (h Handler) queryLogList(ctx context.Context, filter logstore.DNSQueryFilter) ([]logstore.DNSQuery, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(h.opts.DNSQueryLogPath) == "" {
		return nil, nil
	}
	store, err := logstore.OpenDNSQueryLogReadOnly(h.opts.DNSQueryLogPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	return store.List(ctx, filter)
}

func (h Handler) queryLogAggregate(ctx context.Context, filter logstore.DNSQueryFilter) (logstore.DNSQueryAggregate, error) {
	if err := ctx.Err(); err != nil {
		return logstore.DNSQueryAggregate{}, err
	}
	if strings.TrimSpace(h.opts.DNSQueryLogPath) == "" {
		return logstore.DNSQueryAggregate{Since: filter.Since, Until: filter.Until}, nil
	}
	store, err := logstore.OpenDNSQueryLogReadOnly(h.opts.DNSQueryLogPath)
	if os.IsNotExist(err) {
		return logstore.DNSQueryAggregate{Since: filter.Since, Until: filter.Until}, nil
	}
	if err != nil {
		return logstore.DNSQueryAggregate{}, err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return store.Aggregate(ctx, filter)
}

func (h Handler) trafficFlowList(ctx context.Context, filter logstore.TrafficFlowFilter) ([]logstore.TrafficFlow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(h.opts.TrafficFlowLogPath) == "" {
		return nil, nil
	}
	store, err := logstore.OpenTrafficFlowLogReadOnly(h.opts.TrafficFlowLogPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	return store.List(ctx, filter)
}

func (h Handler) trafficFlowAggregate(ctx context.Context, filter logstore.TrafficFlowFilter) (logstore.TrafficFlowAggregate, error) {
	if err := ctx.Err(); err != nil {
		return logstore.TrafficFlowAggregate{}, err
	}
	if strings.TrimSpace(h.opts.TrafficFlowLogPath) == "" {
		return logstore.TrafficFlowAggregate{Since: filter.Since, Until: filter.Until}, nil
	}
	store, err := logstore.OpenTrafficFlowLogReadOnly(h.opts.TrafficFlowLogPath)
	if os.IsNotExist(err) {
		return logstore.TrafficFlowAggregate{Since: filter.Since, Until: filter.Until}, nil
	}
	if err != nil {
		return logstore.TrafficFlowAggregate{}, err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return store.Aggregate(ctx, filter)
}

func (h Handler) firewallLogList(ctx context.Context, filter logstore.FirewallLogFilter) ([]logstore.FirewallLogEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(h.opts.FirewallLogPath) == "" {
		return nil, nil
	}
	store, err := logstore.OpenFirewallLogReadOnly(h.opts.FirewallLogPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	return store.List(ctx, filter)
}

func (h Handler) firewallDenyTimelineList(ctx context.Context, since time.Time, until time.Time, bucket time.Duration) ([]logstore.FirewallDenyTimelineBucket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(h.opts.FirewallLogPath) == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	store, err := logstore.OpenFirewallLogReadOnly(h.opts.FirewallLogPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.DenyTimeline(ctx, since, until, bucket)
}

func (h Handler) conntrackTuningSummary(ctx context.Context, now time.Time, window time.Duration, autoApply bool) (conntracktuning.Summary, error) {
	if err := ctx.Err(); err != nil {
		return conntracktuning.Summary{}, err
	}
	inputs := conntracktuning.Inputs{Now: now, Window: window, AutoApply: autoApply}
	if strings.TrimSpace(h.opts.FirewallLogPath) == "" {
		return conntracktuning.Analyze(inputs), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	store, err := logstore.OpenFirewallLogReadOnly(h.opts.FirewallLogPath)
	if os.IsNotExist(err) {
		return conntracktuning.Analyze(inputs), nil
	}
	if err != nil {
		return conntracktuning.Summary{}, err
	}
	defer store.Close()
	since := now.Add(-window)
	inputs.FirewallLogs, err = store.List(ctx, logstore.FirewallLogFilter{Since: since, Limit: 1000})
	if err != nil {
		return conntracktuning.Summary{}, err
	}
	inputs.DPIFlows, err = store.ListDPIFlows(ctx, logstore.DPIFlowFilter{Since: since, Limit: 5000})
	if err != nil {
		return conntracktuning.Summary{}, err
	}
	inputs.ExpiredFlows, err = store.ListExpiredFlows(ctx, logstore.ExpiredFlowFilter{Since: since, Limit: 5000})
	if err != nil {
		return conntracktuning.Summary{}, err
	}
	return conntracktuning.Analyze(inputs), nil
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
