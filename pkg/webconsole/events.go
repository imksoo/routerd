// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controlapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func (h Handler) controllers(w http.ResponseWriter) {
	controllers := controlapi.NewControllers(h.controllerStatuses())
	writeJSON(w, controllers)
}

func (h Handler) events(w http.ResponseWriter, r *http.Request) {
	events, err := h.eventListQuery(routerstate.EventQuery{
		Limit:    intQuery(r, "limit", 100),
		SinceID:  int64(intQuery(r, "sinceID", 0)),
		Topic:    strings.TrimSpace(r.URL.Query().Get("topic")),
		Kind:     strings.TrimSpace(r.URL.Query().Get("kind")),
		Name:     strings.TrimSpace(r.URL.Query().Get("name")),
		Resource: strings.TrimSpace(r.URL.Query().Get("resource")),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, filterStoredEvents(events, storedEventFilter{
		ResourceKind: strings.TrimSpace(r.URL.Query().Get("resourceKind")),
		ResourceName: strings.TrimSpace(r.URL.Query().Get("resourceName")),
		Severity:     strings.TrimSpace(r.URL.Query().Get("severity")),
		Query:        strings.TrimSpace(r.URL.Query().Get("q")),
	}))
}

func (h Handler) eventStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is unavailable")
		return
	}
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ctx := r.Context()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	var events <-chan bus.Event
	var cancel func()
	if h.opts.Bus != nil {
		events, cancel = h.opts.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.**"}}, 64)
		defer cancel()
	}

	_ = writeSSE(w, "connected", map[string]string{"status": "connected", "generatedAt": time.Now().UTC().Format(time.RFC3339Nano)})
	flusher.Flush()

	if h.opts.Bus == nil {
		for {
			select {
			case <-ctx.Done():
				return
			case <-heartbeat.C:
				_, _ = fmt.Fprint(w, ": heartbeat\n\n")
				flusher.Flush()
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := writeSSE(w, "routerd-event", event); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, eventName string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", eventName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	return nil
}

func (h Handler) eventList(limit int) ([]routerstate.StoredEvent, error) {
	return h.eventListQuery(routerstate.EventQuery{Limit: limit})
}

func (h Handler) eventListQuery(query routerstate.EventQuery) ([]routerstate.StoredEvent, error) {
	if lister, ok := h.opts.Store.(routerstate.EventLister); ok {
		return lister.ListEvents(query)
	}
	return nil, nil
}

type storedEventFilter struct {
	ResourceKind string
	ResourceName string
	Severity     string
	Query        string
}

func filterStoredEvents(events []routerstate.StoredEvent, filter storedEventFilter) []routerstate.StoredEvent {
	if filter.ResourceKind == "" && filter.ResourceName == "" && filter.Severity == "" && filter.Query == "" {
		return events
	}
	query := strings.ToLower(filter.Query)
	var out []routerstate.StoredEvent
	for _, event := range events {
		if filter.ResourceKind != "" && event.ResourceKind != filter.ResourceKind && event.Kind != filter.ResourceKind {
			continue
		}
		if filter.ResourceName != "" && event.ResourceName != filter.ResourceName && event.Name != filter.ResourceName {
			continue
		}
		if filter.Severity != "" && !strings.EqualFold(event.Severity, filter.Severity) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(storedEventSearchText(event)), query) {
			continue
		}
		out = append(out, event)
	}
	return out
}

func storedEventSearchText(event routerstate.StoredEvent) string {
	return strings.Join([]string{
		event.Topic,
		event.Type,
		event.Reason,
		event.Message,
		event.Kind,
		event.Name,
		event.ResourceKind,
		event.ResourceName,
		event.Severity,
		fmt.Sprint(event.Attributes),
	}, " ")
}
