// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
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
)

const (
	daemonKind = "routerd-arp-observer"

	sourceARPObserver = "arp-observer"
	sourceOnDemandARP = "on-demand-arp"
	sourcePVESVNet    = "pve-svnet"

	eventARPObserved      = "routerd.mobility.arp.observed"
	eventARPProbeHit      = "routerd.mobility.arp.probe.hit"
	eventPVESVNetObserved = "routerd.mobility.pve-svnet.observed"

	arpRequest = 1
	arpReply   = 2
)

type options struct {
	resource       string
	ifname         string
	eventInterface string
	socketPath     string
	eventFile      string
	poolName       string
	prefix         netip.Prefix
	sourceType     string
	network        string
	bridge         string
	sourceAddress  netip.Addr
	observe        bool
	onDemand       bool
	probeTimeout   time.Duration
	probeRetries   int
	probeCooldown  time.Duration
	scanInterval   time.Duration
	arpTablePath   string
	selfMAC        net.HardwareAddr
}

type daemon struct {
	opts      options
	startedAt time.Time
	cancel    context.CancelFunc

	mu              sync.Mutex
	cond            *sync.Cond
	events          []daemonapi.DaemonEvent
	nextCursor      uint64
	observerError   string
	packetsSeen     uint64
	observedCount   uint64
	probeCount      uint64
	probeHitCount   uint64
	scanCount       uint64
	proactiveCount  uint64
	lastPacketAt    time.Time
	lastEventAt     time.Time
	lastScanAt      time.Time
	lastProbeAt     map[string]time.Time
	pendingProbe    map[string]time.Time
	lastEventByKey  map[string]time.Time
	clients         map[string]arpClient
	proactiveCursor uint32

	socketMu sync.Mutex
}

type arpClient struct {
	IP         string    `json:"ip"`
	MAC        string    `json:"mac"`
	SourceType string    `json:"sourceType"`
	SeenAt     time.Time `json:"seenAt"`
}

type arpPacket struct {
	Operation uint16
	SenderMAC net.HardwareAddr
	SenderIP  netip.Addr
	TargetMAC net.HardwareAddr
	TargetIP  netip.Addr
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
			return json.NewEncoder(stdout).Encode(map[string]any{
				"resource":       opts.resource,
				"interface":      opts.ifname,
				"eventInterface": opts.eventInterface,
				"pool":           opts.poolName,
				"prefix":         opts.prefix.String(),
				"sourceType":     opts.sourceType,
				"observe":        opts.observe,
				"onDemand":       opts.onDemand,
			})
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
	opts := options{sourceType: sourceARPObserver, probeTimeout: time.Second, probeCooldown: 5 * time.Second, scanInterval: 10 * time.Second, arpTablePath: "/proc/net/arp"}
	prefix := ""
	sourceAddress := ""
	selfMAC := ""
	fs.StringVar(&opts.resource, "resource", "arp-observer", "observer resource name")
	fs.StringVar(&opts.ifname, "interface", "", "kernel interface name to observe")
	fs.StringVar(&opts.eventInterface, "event-interface", "", "logical interface name written to events")
	fs.StringVar(&opts.socketPath, "socket", "", "Unix socket path")
	fs.StringVar(&opts.eventFile, "event-file", "", "event JSONL path")
	fs.StringVar(&opts.poolName, "pool", "", "MobilityPool name")
	fs.StringVar(&prefix, "prefix", "", "IPv4 prefix to observe")
	fs.StringVar(&opts.sourceType, "source-type", opts.sourceType, "source type: arp-observer, on-demand-arp, or pve-svnet")
	fs.StringVar(&opts.network, "network", "", "logical svnet/network name")
	fs.StringVar(&opts.bridge, "bridge", "", "local bridge name")
	fs.StringVar(&sourceAddress, "source-address", "", "source IPv4 address for active probes")
	fs.BoolVar(&opts.observe, "observe", false, "emit passive observation events")
	fs.BoolVar(&opts.onDemand, "on-demand", false, "probe ARP targets requested on this segment")
	fs.DurationVar(&opts.probeTimeout, "probe-timeout", opts.probeTimeout, "delay between probe retries")
	fs.IntVar(&opts.probeRetries, "probe-retries", 0, "additional ARP probe retries")
	fs.DurationVar(&opts.probeCooldown, "probe-cooldown", opts.probeCooldown, "minimum interval between probes for the same target")
	fs.DurationVar(&opts.scanInterval, "scan-interval", opts.scanInterval, "interval for polling the local ARP table")
	fs.StringVar(&opts.arpTablePath, "arp-table", opts.arpTablePath, "Linux /proc/net/arp path")
	fs.StringVar(&selfMAC, "self-mac", "", "local sender MAC for active probes")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if strings.TrimSpace(opts.ifname) == "" {
		return opts, fmt.Errorf("--interface is required")
	}
	if strings.TrimSpace(opts.eventInterface) == "" {
		opts.eventInterface = opts.ifname
	}
	if strings.TrimSpace(opts.poolName) == "" {
		return opts, fmt.Errorf("--pool is required")
	}
	parsedPrefix, err := netip.ParsePrefix(strings.TrimSpace(prefix))
	if err != nil {
		return opts, fmt.Errorf("--prefix is required and must be an IPv4 prefix: %w", err)
	}
	opts.prefix = parsedPrefix.Masked()
	if !opts.prefix.Addr().Is4() {
		return opts, fmt.Errorf("--prefix must be IPv4")
	}
	opts.sourceType = strings.TrimSpace(opts.sourceType)
	switch opts.sourceType {
	case sourceARPObserver, sourcePVESVNet:
		if !opts.observe && !opts.onDemand {
			opts.observe = true
		}
	case sourceOnDemandARP:
		if !opts.observe && !opts.onDemand {
			opts.onDemand = true
		}
	default:
		return opts, fmt.Errorf("--source-type must be arp-observer, on-demand-arp, or pve-svnet")
	}
	if strings.TrimSpace(sourceAddress) != "" {
		addr, err := netip.ParseAddr(strings.TrimSpace(sourceAddress))
		if err != nil || !addr.Is4() {
			return opts, fmt.Errorf("--source-address must be IPv4")
		}
		opts.sourceAddress = addr
	}
	if strings.TrimSpace(selfMAC) != "" {
		mac, err := net.ParseMAC(strings.TrimSpace(selfMAC))
		if err != nil {
			return opts, fmt.Errorf("--self-mac: %w", err)
		}
		opts.selfMAC = mac
	} else {
		opts.selfMAC = interfaceMAC(opts.ifname)
	}
	defaults, _ := platform.Current()
	if opts.socketPath == "" {
		opts.socketPath = filepath.Join(defaults.RuntimeDir, "arp-observer", opts.resource+".sock")
	}
	if opts.eventFile == "" {
		opts.eventFile = filepath.Join(defaults.StateDir, "arp-observer", opts.resource, "events.jsonl")
	}
	if opts.probeRetries < 0 {
		opts.probeRetries = 0
	}
	if opts.probeCooldown <= 0 {
		opts.probeCooldown = 5 * time.Second
	}
	if opts.probeTimeout <= 0 {
		opts.probeTimeout = time.Second
	}
	if opts.scanInterval <= 0 {
		opts.scanInterval = 10 * time.Second
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
		opts:           opts,
		startedAt:      time.Now().UTC(),
		cancel:         cancel,
		lastProbeAt:    map[string]time.Time{},
		pendingProbe:   map[string]time.Time{},
		lastEventByKey: map[string]time.Time{},
		clients:        map[string]arpClient{},
	}
	d.cond = sync.NewCond(&d.mu)
	go d.observe(ctx)
	if opts.sourceType == sourcePVESVNet {
		go d.pollARPTable(ctx)
	}
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
	socketCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-ctx.Done()
		_ = socket.close()
	}()
	if d.opts.sourceType == sourceOnDemandARP && d.opts.onDemand {
		go d.proactiveProbeLoop(socketCtx, socket)
	}
	frame := make([]byte, 65535)
	for {
		n, err := socket.read(frame)
		if err != nil {
			return err
		}
		packet, ok, err := parseEthernetARP(frame[:n])
		if err != nil || !ok {
			continue
		}
		d.recordPacket(ctx, socket, packet)
	}
}

func (d *daemon) proactiveProbeLoop(ctx context.Context, socket *packetSocket) {
	for {
		d.probeNextPrefixTarget(ctx, socket)
		select {
		case <-time.After(d.opts.scanInterval):
		case <-ctx.Done():
			return
		}
	}
}

func (d *daemon) probeNextPrefixTarget(ctx context.Context, socket *packetSocket) {
	now := time.Now().UTC()
	target, ok := d.nextProactiveTarget()
	if !ok {
		return
	}
	if !d.shouldProbe(target, now) {
		return
	}
	d.mu.Lock()
	d.proactiveCount++
	d.mu.Unlock()
	d.probeTarget(ctx, socket, target)
}

func (d *daemon) nextProactiveTarget() (netip.Addr, bool) {
	d.mu.Lock()
	cursor := d.proactiveCursor
	target, next, ok := nextIPv4PrefixProbeTarget(d.opts.prefix, cursor, d.opts.sourceAddress)
	if ok {
		d.proactiveCursor = next
	}
	d.mu.Unlock()
	return target, ok
}

func (d *daemon) recordPacket(ctx context.Context, socket *packetSocket, packet arpPacket) {
	now := time.Now().UTC()
	d.mu.Lock()
	d.packetsSeen++
	d.lastPacketAt = now
	d.mu.Unlock()

	if sameMAC(packet.SenderMAC, d.opts.selfMAC) {
		return
	}
	if d.opts.observe && packet.SenderIP.IsValid() && packet.SenderIP.Is4() && !packet.SenderIP.Is4In6() && d.opts.prefix.Contains(packet.SenderIP) && !packet.SenderIP.IsUnspecified() {
		d.publishObservation(packet.SenderIP, packet.SenderMAC, passiveTopic(d.opts.sourceType), d.opts.sourceType, "ARPObserved", "observed local ARP sender")
	}
	if d.opts.sourceType == sourceOnDemandARP && packet.SenderIP.IsValid() && packet.SenderIP.Is4() && d.opts.prefix.Contains(packet.SenderIP) {
		if d.markProbeHit(packet.SenderIP) || packet.Operation == arpReply {
			d.publishObservation(packet.SenderIP, packet.SenderMAC, eventARPProbeHit, sourceOnDemandARP, "ARPProbeHit", "observed ARP response for probed target")
		}
	}
	if d.opts.onDemand && packet.Operation == arpRequest && packet.TargetIP.IsValid() && packet.TargetIP.Is4() && d.opts.prefix.Contains(packet.TargetIP) && !packet.TargetIP.IsUnspecified() {
		if sameAddr(packet.TargetIP, packet.SenderIP) {
			return
		}
		if d.shouldProbe(packet.TargetIP, now) {
			go d.probeTarget(ctx, socket, packet.TargetIP)
		}
	}
}

func (d *daemon) pollARPTable(ctx context.Context) {
	ticker := time.NewTicker(d.opts.scanInterval)
	defer ticker.Stop()
	for {
		d.scanARPTable()
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

func (d *daemon) scanARPTable() {
	entries, err := readARPTable(d.opts.arpTablePath)
	now := time.Now().UTC()
	d.mu.Lock()
	d.scanCount++
	d.lastScanAt = now
	d.mu.Unlock()
	if err != nil {
		d.setObserverError(err)
		return
	}
	for _, entry := range entries {
		if !arpTableDeviceMatches(entry.Device, d.opts.ifname, d.opts.eventInterface, d.opts.bridge) {
			continue
		}
		if !entry.IP.IsValid() || !entry.IP.Is4() || entry.IP.IsUnspecified() || !d.opts.prefix.Contains(entry.IP) {
			continue
		}
		if len(entry.MAC) != 6 {
			continue
		}
		d.publishObservation(entry.IP, entry.MAC, eventPVESVNetObserved, sourcePVESVNet, "PVESVNetObserved", "observed local PVE svnet ARP table entry")
	}
}

func (d *daemon) publishObservation(address netip.Addr, mac net.HardwareAddr, topic, sourceType, reason, message string) {
	now := time.Now().UTC()
	key := topic + "|" + address.String() + "|" + strings.ToLower(mac.String())
	d.mu.Lock()
	if last := d.lastEventByKey[key]; !last.IsZero() && now.Sub(last) < 30*time.Second {
		d.mu.Unlock()
		return
	}
	d.lastEventByKey[key] = now
	d.clients[address.String()] = arpClient{IP: address.String(), MAC: strings.ToLower(mac.String()), SourceType: sourceType, SeenAt: now}
	if topic == eventARPProbeHit {
		d.probeHitCount++
	} else {
		d.observedCount++
	}
	d.lastEventAt = now
	d.publishLocked(topic, daemonapi.SeverityInfo, reason, message, d.eventAttrs(address, mac, sourceType))
	d.mu.Unlock()
}

func (d *daemon) eventAttrs(address netip.Addr, mac net.HardwareAddr, sourceType string) map[string]string {
	attrs := map[string]string{
		"address":    address.String(),
		"ip":         address.String(),
		"mac":        strings.ToLower(mac.String()),
		"interface":  strings.TrimSpace(d.opts.eventInterface),
		"ifname":     strings.TrimSpace(d.opts.ifname),
		"pool":       strings.TrimSpace(d.opts.poolName),
		"sourceType": sourceType,
		"observer":   strings.TrimSpace(d.opts.resource),
		"prefix":     d.opts.prefix.String(),
	}
	if value := strings.TrimSpace(d.opts.network); value != "" {
		attrs["network"] = value
		attrs["svnet"] = value
	}
	if value := strings.TrimSpace(d.opts.bridge); value != "" {
		attrs["bridge"] = value
	}
	return attrs
}

func (d *daemon) shouldProbe(target netip.Addr, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := target.String()
	if last := d.lastProbeAt[key]; !last.IsZero() && now.Sub(last) < d.opts.probeCooldown {
		return false
	}
	d.lastProbeAt[key] = now
	d.pendingProbe[key] = now.Add(time.Duration(d.opts.probeRetries+1)*d.opts.probeTimeout + time.Second)
	return true
}

func (d *daemon) markProbeHit(address netip.Addr) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := address.String()
	deadline, ok := d.pendingProbe[key]
	if !ok || time.Now().UTC().After(deadline) {
		delete(d.pendingProbe, key)
		return false
	}
	delete(d.pendingProbe, key)
	return true
}

func (d *daemon) probeTarget(ctx context.Context, socket *packetSocket, target netip.Addr) {
	attempts := d.opts.probeRetries + 1
	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			return
		}
		if err := d.sendARPProbe(socket, target); err != nil {
			d.setObserverError(err)
			return
		}
		if i+1 < attempts {
			select {
			case <-time.After(d.opts.probeTimeout):
			case <-ctx.Done():
				return
			}
		}
	}
}

func (d *daemon) sendARPProbe(socket *packetSocket, target netip.Addr) error {
	if len(d.opts.selfMAC) != 6 {
		return fmt.Errorf("local MAC address is required for ARP probes on %s", d.opts.ifname)
	}
	frame := buildARPRequest(d.opts.selfMAC, d.opts.sourceAddress, target)
	d.socketMu.Lock()
	_, err := socket.write(frame)
	d.socketMu.Unlock()
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.probeCount++
	d.mu.Unlock()
	return nil
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
	reason := "ARPObserverWatching"
	message := "watching ARP frames"
	if d.observerError != "" {
		health = daemonapi.HealthDegraded
		resourcePhase = "Pending"
		reason = "ARPObserverUnavailable"
		message = d.observerError
	}
	clients, _ := json.Marshal(d.observedClientsLocked())
	observed := map[string]string{
		"interface":       d.opts.eventInterface,
		"ifname":          d.opts.ifname,
		"pool":            d.opts.poolName,
		"prefix":          d.opts.prefix.String(),
		"sourceType":      d.opts.sourceType,
		"observe":         strconv.FormatBool(d.opts.observe),
		"onDemand":        strconv.FormatBool(d.opts.onDemand),
		"packetsSeen":     strconv.FormatUint(d.packetsSeen, 10),
		"observedCount":   strconv.FormatUint(d.observedCount, 10),
		"probeCount":      strconv.FormatUint(d.probeCount, 10),
		"probeHitCount":   strconv.FormatUint(d.probeHitCount, 10),
		"proactiveCount":  strconv.FormatUint(d.proactiveCount, 10),
		"scanCount":       strconv.FormatUint(d.scanCount, 10),
		"scanInterval":    d.opts.scanInterval.String(),
		"observedClients": string(clients),
	}
	if !d.lastPacketAt.IsZero() {
		observed["lastPacketAt"] = d.lastPacketAt.Format(time.RFC3339Nano)
	}
	if !d.lastEventAt.IsZero() {
		observed["lastEventAt"] = d.lastEventAt.Format(time.RFC3339Nano)
	}
	if !d.lastScanAt.IsZero() {
		observed["lastScanAt"] = d.lastScanAt.Format(time.RFC3339Nano)
	}
	if d.observerError != "" {
		observed["error"] = d.observerError
	}
	if d.opts.network != "" {
		observed["network"] = d.opts.network
	}
	if d.opts.bridge != "" {
		observed["bridge"] = d.opts.bridge
	}
	return daemonapi.DaemonStatus{
		TypeMeta: daemonapi.TypeMeta{APIVersion: daemonapi.APIVersion, Kind: daemonapi.KindDaemonStatus},
		Daemon:   d.daemonRef(),
		Phase:    phase,
		Health:   health,
		Since:    d.startedAt,
		Resources: []daemonapi.ResourceStatus{{
			Resource: daemonapi.ResourceRef{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool", Name: d.opts.poolName},
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

func (d *daemon) observedClientsLocked() []arpClient {
	out := make([]arpClient, 0, len(d.clients))
	for _, client := range d.clients {
		out = append(out, client)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IP < out[j].IP })
	return out
}

func (d *daemon) handleEvents(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	topic := r.URL.Query().Get("topic")
	wait := parseWait(r.URL.Query().Get("wait"))
	tail := r.URL.Query().Get("tail") == "true"
	deadline := time.Now().Add(wait)
	d.mu.Lock()
	for {
		events, cursor := d.eventsSinceLocked(since, topic, tail)
		if len(events) > 0 || wait <= 0 || time.Now().After(deadline) {
			d.mu.Unlock()
			writeHTTPJSON(w, eventsResponse{Cursor: cursor, Events: events})
			return
		}
		timer := time.AfterFunc(time.Until(deadline), func() {
			d.mu.Lock()
			d.cond.Broadcast()
			d.mu.Unlock()
		})
		d.cond.Wait()
		timer.Stop()
	}
}

func (d *daemon) eventsSinceLocked(since, topic string, tail bool) ([]daemonapi.DaemonEvent, string) {
	var out []daemonapi.DaemonEvent
	cursor := since
	sinceID, _ := strconv.ParseUint(since, 10, 64)
	for _, event := range d.events {
		id, _ := strconv.ParseUint(event.Cursor, 10, 64)
		if tail {
			cursor = event.Cursor
			continue
		}
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
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool", Name: d.opts.poolName}
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

func parseEthernetARP(frame []byte) (arpPacket, bool, error) {
	if len(frame) < 42 {
		return arpPacket{}, false, nil
	}
	if binary.BigEndian.Uint16(frame[12:14]) != 0x0806 {
		return arpPacket{}, false, nil
	}
	arp := frame[14:]
	if binary.BigEndian.Uint16(arp[0:2]) != 1 || binary.BigEndian.Uint16(arp[2:4]) != 0x0800 || arp[4] != 6 || arp[5] != 4 {
		return arpPacket{}, false, nil
	}
	op := binary.BigEndian.Uint16(arp[6:8])
	if op != arpRequest && op != arpReply {
		return arpPacket{}, false, nil
	}
	return arpPacket{
		Operation: op,
		SenderMAC: append(net.HardwareAddr(nil), arp[8:14]...),
		SenderIP:  netip.AddrFrom4([4]byte{arp[14], arp[15], arp[16], arp[17]}),
		TargetMAC: append(net.HardwareAddr(nil), arp[18:24]...),
		TargetIP:  netip.AddrFrom4([4]byte{arp[24], arp[25], arp[26], arp[27]}),
	}, true, nil
}

func buildARPRequest(senderMAC net.HardwareAddr, source, target netip.Addr) []byte {
	frame := make([]byte, 42)
	for i := 0; i < 6; i++ {
		frame[i] = 0xff
	}
	copy(frame[6:12], senderMAC)
	binary.BigEndian.PutUint16(frame[12:14], 0x0806)
	binary.BigEndian.PutUint16(frame[14:16], 1)
	binary.BigEndian.PutUint16(frame[16:18], 0x0800)
	frame[18] = 6
	frame[19] = 4
	binary.BigEndian.PutUint16(frame[20:22], arpRequest)
	copy(frame[22:28], senderMAC)
	if source.IsValid() && source.Is4() {
		src := source.As4()
		copy(frame[28:32], src[:])
	}
	tgt := target.As4()
	copy(frame[38:42], tgt[:])
	return frame
}

func nextIPv4PrefixProbeTarget(prefix netip.Prefix, cursor uint32, source netip.Addr) (netip.Addr, uint32, bool) {
	if !prefix.IsValid() || !prefix.Addr().Is4() {
		return netip.Addr{}, cursor, false
	}
	prefix = prefix.Masked()
	bits := prefix.Bits()
	if bits < 0 || bits > 32 {
		return netip.Addr{}, cursor, false
	}
	total := uint64(1) << uint(32-bits)
	baseBytes := prefix.Addr().As4()
	base := binary.BigEndian.Uint32(baseBytes[:])
	start := uint64(cursor) % total
	for i := uint64(0); i < total; i++ {
		offset := (start + i) % total
		next := uint32((offset + 1) % total)
		if !probeUsableOffset(bits, total, offset) {
			continue
		}
		addr := netip.AddrFrom4(uint32ToIPv4(base + uint32(offset)))
		if source.IsValid() && source.Is4() && addr == source {
			continue
		}
		return addr, next, true
	}
	return netip.Addr{}, cursor, false
}

func probeUsableOffset(bits int, total, offset uint64) bool {
	if total == 0 {
		return false
	}
	if bits <= 30 && (offset == 0 || offset+1 == total) {
		return false
	}
	return true
}

func uint32ToIPv4(value uint32) [4]byte {
	return [4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)}
}

type arpTableEntry struct {
	IP     netip.Addr
	MAC    net.HardwareAddr
	Device string
}

func readARPTable(path string) ([]arpTableEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return parseARPTable(file), nil
}

func parseARPTable(r io.Reader) []arpTableEntry {
	data, err := io.ReadAll(io.LimitReader(r, 4<<20))
	if err != nil {
		return nil
	}
	var out []arpTableEntry
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || i == 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		flags, err := strconv.ParseInt(strings.TrimPrefix(fields[2], "0x"), 16, 64)
		if err != nil || flags&0x2 == 0 {
			continue
		}
		ip, err := netip.ParseAddr(fields[0])
		if err != nil || !ip.Is4() {
			continue
		}
		mac, err := net.ParseMAC(fields[3])
		if err != nil || len(mac) != 6 || strings.EqualFold(mac.String(), "00:00:00:00:00:00") {
			continue
		}
		out = append(out, arpTableEntry{IP: ip, MAC: mac, Device: fields[5]})
	}
	return out
}

func arpTableDeviceMatches(device string, candidates ...string) bool {
	device = strings.TrimSpace(device)
	if device == "" {
		return false
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == device {
			return true
		}
	}
	return false
}

func passiveTopic(sourceType string) string {
	if sourceType == sourcePVESVNet {
		return eventPVESVNetObserved
	}
	return eventARPObserved
}

func interfaceMAC(ifname string) net.HardwareAddr {
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil
	}
	return iface.HardwareAddr
}

func sameMAC(a, b net.HardwareAddr) bool {
	return len(a) == 6 && len(b) == 6 && strings.EqualFold(a.String(), b.String())
}

func sameAddr(a, b netip.Addr) bool {
	return a.IsValid() && b.IsValid() && a == b
}

func conditionStatus(ok bool) string {
	if ok {
		return "True"
	}
	return "False"
}

func parseWait(value string) time.Duration {
	if strings.TrimSpace(value) == "" {
		return 0
	}
	wait, err := time.ParseDuration(value)
	if err != nil || wait < 0 {
		return 0
	}
	return wait
}

func writeHTTPJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerd-arp-observer daemon --resource NAME --interface IFNAME --pool NAME --prefix CIDR --source-type arp-observer|on-demand-arp|pve-svnet")
}
