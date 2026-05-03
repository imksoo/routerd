package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sys/unix"

	"routerd/pkg/daemonapi"
	"routerd/pkg/dhcpv4client"
	routerotel "routerd/pkg/otel"
)

const daemonKind = "routerd-dhcpv4-client"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type options struct {
	resource         string
	ifname           string
	hostname         string
	requestedAddress string
	classID          string
	clientID         string
	timeout          time.Duration
	socketPath       string
	leaseFile        string
	eventFile        string
}

func run(args []string, stdout io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "selftest", "once", "run":
			return onceCommand(args[1:], stdout)
		case "daemon":
			return daemonCommand(args[1:])
		case "help", "-h", "--help":
			usage(stdout)
			return nil
		}
	}
	if hasFlag(args, "once") {
		return onceCommand(args, stdout)
	}
	return daemonCommand(args)
}

func parseOptions(name string, args []string) (options, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var opts options
	fs.StringVar(&opts.resource, "resource", "wan-dhcpv4", "resource name")
	fs.StringVar(&opts.ifname, "interface", "", "uplink interface name")
	fs.StringVar(&opts.hostname, "hostname", "", "DHCP option 12 hostname")
	fs.StringVar(&opts.requestedAddress, "requested-address", "", "DHCP option 50 requested IPv4 address")
	fs.StringVar(&opts.classID, "class-id", "", "DHCP option 60 vendor class identifier")
	fs.StringVar(&opts.clientID, "client-id", "", "DHCP option 61 client identifier")
	fs.DurationVar(&opts.timeout, "timeout", 30*time.Second, "overall acquisition timeout for selftest/once")
	fs.StringVar(&opts.socketPath, "socket", "", "Unix socket path")
	fs.StringVar(&opts.leaseFile, "lease-file", "", "lease snapshot path")
	fs.StringVar(&opts.eventFile, "event-file", "", "event JSONL path")
	_ = fs.Bool("once", false, "run once and exit")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if opts.ifname == "" {
		return options{}, errors.New("--interface is required")
	}
	if opts.socketPath == "" {
		opts.socketPath = filepath.Join("/run/routerd/dhcpv4-client", opts.resource+".sock")
	}
	if opts.leaseFile == "" {
		opts.leaseFile = filepath.Join("/var/lib/routerd/dhcpv4-client", opts.resource, "lease.json")
	}
	if opts.eventFile == "" {
		opts.eventFile = filepath.Join("/var/lib/routerd/dhcpv4-client", opts.resource, "events.jsonl")
	}
	return opts, nil
}

func onceCommand(args []string, stdout io.Writer) error {
	opts, err := parseOptions("once", args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	d, err := newDaemon(opts)
	if err != nil {
		return err
	}
	defer d.conn.Close()
	if err := d.acquire(ctx); err != nil {
		return err
	}
	d.mu.Lock()
	snapshot := d.snapshotLocked()
	d.mu.Unlock()
	return writeJSON(stdout, snapshot)
}

func daemonCommand(args []string) error {
	opts, err := parseOptions("daemon", args)
	if err != nil {
		return err
	}
	telemetry, err := routerotel.Setup(context.Background(), daemonKind, attribute.String("routerd.resource.name", opts.resource))
	if err != nil {
		return err
	}
	defer telemetry.Shutdown(context.Background())
	d, err := newDaemon(opts)
	if err != nil {
		return err
	}
	d.telemetry = telemetry
	d.leaseGauge, _ = telemetry.Meter.Int64Gauge("routerd.dhcpv4.client.lease.state")
	d.renewCounter, _ = telemetry.Meter.Int64Counter("routerd.dhcpv4.client.renew")
	return d.Run(context.Background())
}

type dhcpv4Daemon struct {
	opts       options
	ifi        *net.Interface
	conn       *net.UDPConn
	startedAt  time.Time
	mu         sync.Mutex
	cond       *sync.Cond
	state      dhcpv4client.State
	xid        uint32
	lease      dhcpv4client.Lease
	events     []daemonapi.DaemonEvent
	nextCursor uint64

	telemetry    *routerotel.Runtime
	leaseGauge   metric.Int64Gauge
	renewCounter metric.Int64Counter
}

type eventsResponse struct {
	Cursor string                  `json:"cursor,omitempty"`
	Events []daemonapi.DaemonEvent `json:"events"`
}

func newDaemon(opts options) (*dhcpv4Daemon, error) {
	ifi, err := net.InterfaceByName(opts.ifname)
	if err != nil {
		return nil, err
	}
	conn, err := listenDHCPv4(opts.ifname)
	if err != nil {
		return nil, err
	}
	d := &dhcpv4Daemon{opts: opts, ifi: ifi, conn: conn, startedAt: time.Now().UTC(), state: dhcpv4client.StateIdle}
	d.cond = sync.NewCond(&d.mu)
	return d, nil
}

func (d *dhcpv4Daemon) Run(ctx context.Context) error {
	defer d.conn.Close()
	if err := d.prepareFilesystem(); err != nil {
		return err
	}
	_ = d.restoreLease()
	_ = d.restoreEvents()
	server, listener, err := d.httpServer()
	if err != nil {
		return err
	}
	defer listener.Close()
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	d.publish(daemonapi.EventDaemonStarted, daemonapi.SeverityInfo, "Started", "DHCPv4 client daemon started", nil)
	if err := d.acquire(ctx); err != nil {
		d.publish("routerd.dhcpv4.client.acquire.failed", daemonapi.SeverityWarning, "AcquireFailed", err.Error(), nil)
	}
	d.publish(daemonapi.EventDaemonReady, daemonapi.SeverityInfo, "Ready", "DHCPv4 client daemon is ready", nil)
	buf := make([]byte, 1500)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.shouldRenew() {
			if err := d.renew(ctx); err != nil {
				d.publish("routerd.dhcpv4.client.renew.failed", daemonapi.SeverityWarning, "RenewFailed", err.Error(), nil)
			}
		}
		_ = d.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, _, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			if timeoutError(err) {
				continue
			}
			return err
		}
		_ = d.handlePacket(ctx, buf[:n])
	}
}

func (d *dhcpv4Daemon) acquire(ctx context.Context) error {
	d.mu.Lock()
	d.state = dhcpv4client.StateDiscovering
	d.xid = dhcpv4client.NewXID()
	d.mu.Unlock()
	if err := d.send(ctx, dhcpv4client.MessageDiscover, dhcpv4client.Message{}); err != nil {
		return err
	}
	d.publish("routerd.dhcpv4.client.discover.sent", daemonapi.SeverityInfo, "DiscoverSent", "sent DHCPv4 Discover", nil)
	deadline, _ := ctx.Deadline()
	if deadline.IsZero() {
		deadline = time.Now().Add(30 * time.Second)
	}
	buf := make([]byte, 1500)
	for time.Now().Before(deadline) {
		_ = d.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, _, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			if timeoutError(err) {
				_ = d.send(ctx, dhcpv4client.MessageDiscover, dhcpv4client.Message{})
				continue
			}
			return err
		}
		msg, err := dhcpv4client.Decode(buf[:n])
		if err != nil || msg.XID != d.xid {
			continue
		}
		if msg.MessageType() == dhcpv4client.MessageOffer {
			d.publish("routerd.dhcpv4.client.offer.received", daemonapi.SeverityInfo, "OfferReceived", "received DHCPv4 Offer", map[string]string{"address": msg.YIAddr.String()})
			d.mu.Lock()
			d.state = dhcpv4client.StateRequesting
			d.mu.Unlock()
			if err := d.send(ctx, dhcpv4client.MessageRequest, msg); err != nil {
				return err
			}
			d.publish("routerd.dhcpv4.client.request.sent", daemonapi.SeverityInfo, "RequestSent", "sent DHCPv4 Request", nil)
			continue
		}
		if msg.MessageType() == dhcpv4client.MessageACK {
			return d.bind(ctx, msg)
		}
		if msg.MessageType() == dhcpv4client.MessageNAK {
			return errors.New("DHCPv4 server returned NAK")
		}
	}
	return context.DeadlineExceeded
}

func (d *dhcpv4Daemon) renew(ctx context.Context) error {
	d.mu.Lock()
	d.state = dhcpv4client.StateRenewing
	d.xid = dhcpv4client.NewXID()
	d.mu.Unlock()
	if d.renewCounter != nil {
		d.renewCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("routerd.resource.name", d.opts.resource)))
	}
	if err := d.send(ctx, dhcpv4client.MessageRequest, dhcpv4client.Message{}); err != nil {
		return err
	}
	d.publish("routerd.dhcpv4.client.renew.sent", daemonapi.SeverityInfo, "RenewSent", "sent DHCPv4 Renew", nil)
	return nil
}

func (d *dhcpv4Daemon) handlePacket(ctx context.Context, packet []byte) error {
	msg, err := dhcpv4client.Decode(packet)
	if err != nil || msg.XID != d.xid {
		return nil
	}
	switch msg.MessageType() {
	case dhcpv4client.MessageACK:
		return d.bind(ctx, msg)
	case dhcpv4client.MessageNAK:
		d.mu.Lock()
		d.state = dhcpv4client.StateExpired
		d.mu.Unlock()
		return d.saveLease()
	default:
		return nil
	}
}

func (d *dhcpv4Daemon) bind(_ context.Context, msg dhcpv4client.Message) error {
	lease := dhcpv4client.LeaseFromACK(msg, time.Now().UTC())
	d.mu.Lock()
	d.lease = lease
	d.state = dhcpv4client.StateBound
	d.mu.Unlock()
	if err := d.saveLease(); err != nil {
		return err
	}
	d.publish("routerd.dhcpv4.client.lease.bound", daemonapi.SeverityInfo, "LeaseBound", "DHCPv4 lease bound", map[string]string{"address": lease.Address.String(), "gateway": lease.DefaultGateway.String()})
	return nil
}

func (d *dhcpv4Daemon) send(ctx context.Context, msgType byte, reply dhcpv4client.Message) error {
	opts := dhcpv4client.RequestOptions(d.config(), reply)
	packet := dhcpv4client.EncodeRequest(msgType, d.xid, d.ifi.HardwareAddr, opts)
	_, err := d.conn.WriteToUDP(packet, &net.UDPAddr{IP: net.IPv4bcast, Port: 67})
	return err
}

func (d *dhcpv4Daemon) config() dhcpv4client.Config {
	return dhcpv4client.Config{Resource: d.opts.resource, Interface: d.opts.ifname, HardwareAddr: d.ifi.HardwareAddr, Hostname: d.opts.hostname, RequestedAddress: d.opts.requestedAddress, ClassID: d.opts.classID, ClientID: d.opts.clientID}
}

func (d *dhcpv4Daemon) shouldRenew() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state == dhcpv4client.StateBound && !d.lease.RenewAt().IsZero() && time.Now().After(d.lease.RenewAt())
}

func (d *dhcpv4Daemon) snapshotLocked() dhcpv4client.Snapshot {
	now := time.Now().UTC()
	s := dhcpv4client.SnapshotFromLease(d.opts.resource, d.opts.ifname, d.state, d.lease, now)
	return s
}

func (d *dhcpv4Daemon) restoreLease() error {
	data, err := os.ReadFile(d.opts.leaseFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var snapshot dhcpv4client.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	d.mu.Lock()
	d.state = snapshot.State
	d.lease = dhcpv4client.LeaseFromSnapshot(snapshot)
	d.mu.Unlock()
	return nil
}

func (d *dhcpv4Daemon) saveLease() error {
	d.mu.Lock()
	snapshot := d.snapshotLocked()
	d.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(d.opts.leaseFile), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp := d.opts.leaseFile + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, d.opts.leaseFile)
}

func (d *dhcpv4Daemon) prepareFilesystem() error {
	if err := os.MkdirAll(filepath.Dir(d.opts.socketPath), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.opts.eventFile), 0755); err != nil {
		return err
	}
	_ = os.Remove(d.opts.socketPath)
	return nil
}

func (d *dhcpv4Daemon) httpServer() (*http.Server, net.Listener, error) {
	listener, err := net.Listen("unix", d.opts.socketPath)
	if err != nil {
		return nil, nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", d.handleStatus)
	mux.HandleFunc("/v1/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeHTTPJSON(w, map[string]string{"status": daemonapi.HealthOK})
	})
	mux.HandleFunc("/v1/events", d.handleEvents)
	mux.HandleFunc("/v1/commands/", d.handleCommand)
	return &http.Server{Handler: mux}, listener, nil
}

func (d *dhcpv4Daemon) handleStatus(w http.ResponseWriter, _ *http.Request) {
	d.mu.Lock()
	snapshot := d.snapshotLocked()
	if d.leaseGauge != nil {
		d.leaseGauge.Record(context.Background(), stateValue(snapshot.State), metric.WithAttributes(attribute.String("routerd.resource.name", d.opts.resource), attribute.String("routerd.dhcpv4.state", string(snapshot.State))))
	}
	d.mu.Unlock()
	status := daemonapi.NewStatus(d.daemonRef())
	status.Phase = daemonapi.PhaseRunning
	status.Health = daemonapi.HealthOK
	status.Since = d.startedAt
	status.Resources = []daemonapi.ResourceStatus{{
		Resource: daemonapi.ResourceRef{APIVersion: "net.routerd.net/v1alpha1", Kind: "DHCPv4Lease", Name: d.opts.resource},
		Phase:    resourcePhase(snapshot.State),
		Health:   daemonapi.HealthOK,
		Since:    snapshot.UpdatedAt,
		Conditions: []daemonapi.Condition{{
			Type:               "LeaseReady",
			Status:             conditionStatus(snapshot.State == dhcpv4client.StateBound),
			Reason:             string(snapshot.State),
			LastTransitionTime: snapshot.UpdatedAt,
		}},
		Observed: map[string]string{
			"interface":      snapshot.Interface,
			"currentAddress": snapshot.CurrentAddress,
			"defaultGateway": snapshot.DefaultGateway,
			"dnsServers":     jsonStringList(snapshot.DNSServers),
			"domain":         snapshot.Domain,
			"leaseTime":      strconv.FormatInt(snapshot.LeaseTimeSeconds, 10),
			"renewAt":        snapshot.RenewAt.Format(time.RFC3339Nano),
			"rebindAt":       snapshot.RebindAt.Format(time.RFC3339Nano),
			"expiresAt":      snapshot.ExpiresAt.Format(time.RFC3339Nano),
		},
	}}
	writeHTTPJSON(w, status)
}

func (d *dhcpv4Daemon) handleEvents(w http.ResponseWriter, r *http.Request) {
	topic := r.URL.Query().Get("topic")
	since := r.URL.Query().Get("since")
	d.mu.Lock()
	events, cursor := d.eventsSinceLocked(since, topic)
	d.mu.Unlock()
	writeHTTPJSON(w, eventsResponse{Cursor: cursor, Events: events})
}

func (d *dhcpv4Daemon) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	command := strings.TrimPrefix(r.URL.Path, "/v1/commands/")
	result := daemonapi.CommandResult{TypeMeta: daemonapi.TypeMeta{APIVersion: daemonapi.APIVersion, Kind: daemonapi.KindCommandResult}, Command: command}
	var err error
	switch command {
	case daemonapi.CommandRenew:
		err = d.renew(r.Context())
	case daemonapi.CommandRelease:
		d.mu.Lock()
		d.state = dhcpv4client.StateReleased
		d.mu.Unlock()
		err = d.saveLease()
	case daemonapi.CommandReload:
	case daemonapi.CommandStop:
	default:
		err = fmt.Errorf("unknown command %q", command)
	}
	if err != nil {
		result.Accepted = false
		result.Message = err.Error()
		w.WriteHeader(http.StatusBadRequest)
		writeHTTPJSON(w, result)
		return
	}
	result.Accepted = true
	result.Message = "accepted"
	writeHTTPJSON(w, result)
}

func (d *dhcpv4Daemon) publish(eventType, severity, reason, message string, attrs map[string]string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	event := daemonapi.NewEvent(d.daemonRef(), eventType, severity)
	event.Resource = &daemonapi.ResourceRef{APIVersion: "net.routerd.net/v1alpha1", Kind: "DHCPv4Lease", Name: d.opts.resource}
	event.Reason = reason
	event.Message = message
	event.Attributes = attrs
	d.nextCursor++
	event.Cursor = strconv.FormatUint(d.nextCursor, 10)
	d.events = append(d.events, event)
	if len(d.events) > 1000 {
		d.events = append([]daemonapi.DaemonEvent(nil), d.events[len(d.events)-1000:]...)
	}
	d.appendEventFileLocked(event)
	d.cond.Broadcast()
}

func (d *dhcpv4Daemon) appendEventFileLocked(event daemonapi.DaemonEvent) {
	if d.opts.eventFile == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(d.opts.eventFile), 0755)
	file, err := os.OpenFile(d.opts.eventFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer file.Close()
	_ = json.NewEncoder(file).Encode(event)
}

func (d *dhcpv4Daemon) restoreEvents() error {
	file, err := os.Open(d.opts.eventFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event daemonapi.DaemonEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		d.events = append(d.events, event)
		if id, err := strconv.ParseUint(event.Cursor, 10, 64); err == nil && id > d.nextCursor {
			d.nextCursor = id
		}
	}
	return scanner.Err()
}

func (d *dhcpv4Daemon) eventsSinceLocked(since, topic string) ([]daemonapi.DaemonEvent, string) {
	var out []daemonapi.DaemonEvent
	cursor := since
	sinceID, _ := strconv.ParseUint(since, 10, 64)
	for _, event := range d.events {
		id, _ := strconv.ParseUint(event.Cursor, 10, 64)
		if id <= sinceID {
			continue
		}
		if topic != "" && topic != event.Type {
			continue
		}
		out = append(out, event)
		cursor = event.Cursor
	}
	return out, cursor
}

func (d *dhcpv4Daemon) daemonRef() daemonapi.DaemonRef {
	return daemonapi.DaemonRef{Name: daemonKind + "-" + d.opts.resource, Kind: daemonKind, Instance: d.opts.resource}
}

func listenDHCPv4(ifname string) (*net.UDPConn, error) {
	lc := net.ListenConfig{Control: func(network, address string, c syscall.RawConn) error {
		var sockErr error
		if err := c.Control(func(fd uintptr) {
			sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
			if sockErr == nil {
				sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_BROADCAST, 1)
			}
			if sockErr == nil && ifname != "" {
				sockErr = bindSocketToDevice(int(fd), ifname)
			}
		}); err != nil && sockErr == nil {
			sockErr = err
		}
		return sockErr
	}}
	pc, err := lc.ListenPacket(context.Background(), "udp4", ":68")
	if err != nil {
		return nil, err
	}
	return pc.(*net.UDPConn), nil
}

func resourcePhase(state dhcpv4client.State) string {
	switch state {
	case dhcpv4client.StateBound:
		return daemonapi.ResourcePhaseBound
	case dhcpv4client.StateDiscovering, dhcpv4client.StateRequesting:
		return daemonapi.ResourcePhaseAcquiring
	case dhcpv4client.StateRenewing:
		return daemonapi.ResourcePhaseRefreshing
	case dhcpv4client.StateRebinding:
		return daemonapi.ResourcePhaseRebinding
	case dhcpv4client.StateExpired:
		return daemonapi.ResourcePhaseExpired
	case dhcpv4client.StateReleased:
		return daemonapi.ResourcePhaseReleased
	default:
		return daemonapi.ResourcePhaseIdle
	}
}

func stateValue(state dhcpv4client.State) int64 {
	switch state {
	case dhcpv4client.StateBound:
		return 4
	case dhcpv4client.StateRenewing:
		return 5
	case dhcpv4client.StateRebinding:
		return 6
	case dhcpv4client.StateExpired:
		return 7
	default:
		return 0
	}
}

func conditionStatus(ok bool) string {
	if ok {
		return daemonapi.ConditionTrue
	}
	return daemonapi.ConditionFalse
}

func jsonStringList(values []string) string {
	data, _ := json.Marshal(values)
	return string(data)
}

func timeoutError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == "--"+name || arg == "-"+name {
			return true
		}
	}
	return false
}

func writeHTTPJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "routerd-dhcpv4-client [daemon|selftest|once] --interface IFNAME [--resource NAME]")
}
