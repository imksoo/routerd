// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/eventfile"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/raobserver"
)

const daemonKind = "routerd-ra-observer"

type options struct {
	resource   string
	ifname     string
	socketPath string
	eventFile  string
	selfMAC    string
}

type daemon struct {
	opts      options
	startedAt time.Time
	cancel    context.CancelFunc

	mu         sync.Mutex
	cond       *sync.Cond
	events     []daemonapi.DaemonEvent
	nextCursor uint64

	observerError string
	selfMAC       string
	packetsSeen   uint64
	lastObserved  time.Time
	routers       map[string]raobserver.RouterObservation
}

type eventsResponse struct {
	Cursor string                  `json:"cursor,omitempty"`
	Events []daemonapi.DaemonEvent `json:"events"`
	More   bool                    `json:"more,omitempty"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "daemon":
			return daemonCommand(args[1:])
		case "selftest":
			opts, err := parseOptions("selftest", args[1:])
			if err != nil {
				return err
			}
			return json.NewEncoder(stdout).Encode(map[string]any{"resource": opts.resource, "interface": opts.ifname})
		case "help", "-h", "--help":
			usage(stdout)
			return nil
		}
	}
	return daemonCommand(args)
}

func parseOptions(name string, args []string) (options, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := options{}
	fs.StringVar(&opts.resource, "resource", "lan-ra", "RogueRADetector resource name")
	fs.StringVar(&opts.ifname, "interface", "", "interface name to observe")
	fs.StringVar(&opts.socketPath, "socket", "", "Unix socket path")
	fs.StringVar(&opts.eventFile, "event-file", "", "event JSONL path")
	fs.StringVar(&opts.selfMAC, "self-mac", "", "local router MAC address")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if strings.TrimSpace(opts.ifname) == "" {
		return opts, fmt.Errorf("--interface is required")
	}
	defaults, _ := platform.Current()
	if opts.socketPath == "" {
		opts.socketPath = filepath.Join(defaults.RuntimeDir, "ra-observer", opts.resource+".sock")
	}
	if opts.eventFile == "" {
		opts.eventFile = filepath.Join("/var/log/routerd", "ra-observer-"+opts.resource+".events.jsonl")
	}
	return opts, nil
}

func daemonCommand(args []string) error {
	opts, err := parseOptions("daemon", args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &daemon{
		opts:      opts,
		startedAt: time.Now().UTC(),
		cancel:    cancel,
		routers:   map[string]raobserver.RouterObservation{},
	}
	d.cond = sync.NewCond(&d.mu)
	d.selfMAC = firstNonEmpty(opts.selfMAC, interfaceMAC(opts.ifname))
	go d.observe(ctx)
	return d.serve(ctx)
}

func (d *daemon) observe(ctx context.Context) {
	for ctx.Err() == nil {
		socket, err := openPacketSocket(d.opts.ifname)
		if err != nil {
			d.setObserverError(err)
			select {
			case <-time.After(10 * time.Second):
				continue
			case <-ctx.Done():
				return
			}
		}
		d.clearObserverError()
		err = d.observeSocket(ctx, socket)
		_ = socket.close()
		if ctx.Err() != nil {
			return
		}
		d.setObserverError(err)
	}
}

func (d *daemon) observeSocket(ctx context.Context, socket *packetSocket) error {
	go func() {
		<-ctx.Done()
		_ = socket.close()
	}()
	frame := make([]byte, 65535)
	for {
		n, err := socket.read(frame)
		if err != nil {
			return err
		}
		adv, ok, err := raobserver.ParseEthernetIPv6RA(frame[:n])
		if err != nil || !ok {
			continue
		}
		d.recordAdvertisement(adv)
	}
}

func (d *daemon) recordAdvertisement(adv raobserver.Advertisement) {
	now := time.Now().UTC()
	d.mu.Lock()
	d.packetsSeen++
	d.lastObserved = now
	self := raobserver.IsSelfAdvertisement(adv, d.selfMAC)
	if !self {
		key := raobserver.ObservationKey(adv)
		current := d.routers[key]
		first := current.Count == 0
		d.routers[key] = raobserver.UpdateObservation(current, adv, now)
		if first {
			d.publishLocked("routerd.ipv6.ra.rogue_detected", daemonapi.SeverityWarning, "RogueRADetected", "observed non-self IPv6 router advertisement", map[string]string{
				"interface":      d.opts.ifname,
				"sourceMAC":      adv.SourceMAC,
				"sourceLLA":      adv.SourceLLA,
				"routerLifetime": strconv.Itoa(adv.RouterLifetime),
			})
		}
	}
	d.mu.Unlock()
}

func (d *daemon) serve(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(d.opts.socketPath), 0755); err != nil {
		return err
	}
	_ = os.Remove(d.opts.socketPath)
	listener, err := net.Listen("unix", d.opts.socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(d.opts.socketPath)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, _ *http.Request) {
		writeHTTPJSON(w, d.status())
	})
	mux.HandleFunc("/v1/events", d.handleEvents)
	mux.HandleFunc("/v1/commands", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req daemonapi.CommandRequest
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
		if req.Command == daemonapi.CommandStop {
			go d.cancel()
		}
		writeHTTPJSON(w, daemonapi.CommandResult{TypeMeta: daemonapi.TypeMeta{APIVersion: daemonapi.APIVersion, Kind: daemonapi.KindCommandResult}, Command: req.Command, Accepted: true, Message: "accepted"})
	})
	server := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	err = server.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (d *daemon) status() daemonapi.DaemonStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	phase := daemonapi.PhaseRunning
	health := daemonapi.HealthOK
	resourcePhase := "Watching"
	reason := "RAObserverWatching"
	message := "watching IPv6 router advertisements"
	if d.observerError != "" {
		health = daemonapi.HealthDegraded
		resourcePhase = "Pending"
		reason = "RAObserverUnavailable"
		message = d.observerError
	}
	observedRouters, _ := json.Marshal(d.observedRoutersLocked())
	observed := map[string]string{
		"interface":       d.opts.ifname,
		"selfMAC":         d.selfMAC,
		"packetsSeen":     strconv.FormatUint(d.packetsSeen, 10),
		"rogueCount":      strconv.Itoa(len(d.routers)),
		"observedRouters": string(observedRouters),
	}
	if !d.lastObserved.IsZero() {
		observed["lastObservedAt"] = d.lastObserved.Format(time.RFC3339Nano)
	}
	if d.observerError != "" {
		observed["error"] = d.observerError
	}
	return daemonapi.DaemonStatus{
		TypeMeta: daemonapi.TypeMeta{APIVersion: daemonapi.APIVersion, Kind: daemonapi.KindDaemonStatus},
		Daemon:   d.daemonRef(),
		Phase:    phase,
		Health:   health,
		Since:    d.startedAt,
		Resources: []daemonapi.ResourceStatus{{
			Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "RogueRADetector", Name: d.opts.resource},
			Phase:    resourcePhase,
			Health:   health,
			Since:    d.startedAt,
			Conditions: []daemonapi.Condition{{
				Type:               "Observing",
				Status:             conditionStatus(d.observerError == ""),
				Reason:             reason,
				Message:            message,
				LastTransitionTime: d.startedAt,
			}},
			Observed: observed,
		}},
		Observed: observed,
	}
}

func (d *daemon) observedRoutersLocked() []raobserver.RouterObservation {
	out := make([]raobserver.RouterObservation, 0, len(d.routers))
	for _, router := range d.routers {
		out = append(out, router)
	}
	sort.Slice(out, func(i, j int) bool {
		return firstNonEmpty(out[i].SourceMAC, out[i].SourceLLA) < firstNonEmpty(out[j].SourceMAC, out[j].SourceLLA)
	})
	return out
}

func (d *daemon) handleEvents(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	topic := r.URL.Query().Get("topic")
	d.mu.Lock()
	events, cursor := d.eventsSinceLocked(since, topic)
	d.mu.Unlock()
	writeHTTPJSON(w, eventsResponse{Cursor: cursor, Events: events})
}

func (d *daemon) eventsSinceLocked(since, topic string) ([]daemonapi.DaemonEvent, string) {
	var out []daemonapi.DaemonEvent
	cursor := since
	sinceID, _ := strconv.ParseUint(since, 10, 64)
	for _, event := range d.events {
		id, _ := strconv.ParseUint(event.Cursor, 10, 64)
		if id <= sinceID {
			continue
		}
		if topic != "" && event.Type != topic {
			continue
		}
		out = append(out, event)
		cursor = event.Cursor
	}
	return out, cursor
}

func (d *daemon) publishLocked(topic, severity, reason, message string, attrs map[string]string) {
	d.nextCursor++
	event := daemonapi.NewEvent(d.daemonRef(), topic, severity)
	event.Cursor = strconv.FormatUint(d.nextCursor, 10)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "RogueRADetector", Name: d.opts.resource}
	event.Reason = reason
	event.Message = message
	event.Attributes = attrs
	d.events = append(d.events, event)
	if len(d.events) > 1000 {
		d.events = append([]daemonapi.DaemonEvent(nil), d.events[len(d.events)-1000:]...)
	}
	_ = eventfile.AppendJSONLine(d.opts.eventFile, event)
	d.cond.Broadcast()
}

func (d *daemon) setObserverError(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		d.observerError = err.Error()
	}
}

func (d *daemon) clearObserverError() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.observerError = ""
}

func (d *daemon) daemonRef() daemonapi.DaemonRef {
	return daemonapi.DaemonRef{Name: daemonKind + "-" + d.opts.resource, Kind: daemonKind, Instance: d.opts.resource}
}

func interfaceMAC(ifname string) string {
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return ""
	}
	return iface.HardwareAddr.String()
}

func conditionStatus(ok bool) string {
	if ok {
		return "True"
	}
	return "False"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func writeHTTPJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerd-ra-observer daemon --resource NAME --interface IFNAME [--socket PATH]")
}
