// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/eventfile"
	routerotel "github.com/imksoo/routerd/pkg/otel"
	"github.com/imksoo/routerd/pkg/pdclient"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sys/unix"
)

const daemonKind = "routerd-dhcpv6-client"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "selftest":
			return selftestCommand(args[1:], stdout)
		case "once", "run":
			return onceCommand(args[1:], stdout)
		case "daemon":
			return daemonCommand(args[1:], stdout)
		case "help", "-h", "--help":
			usage(stdout)
			return nil
		}
	}
	if hasFlag(args, "once") {
		return onceCommand(args, stdout)
	}
	return daemonCommand(args, stdout)
}

type options struct {
	resource     string
	ifname       string
	clientDUID   string
	iaid         uint
	timeout      time.Duration
	socketPath   string
	leaseFile    string
	eventFile    string
	srcLL        string
	srcMAC       string
	hopLimit     int
	listenPort   int
	renewMargin  time.Duration
	rebindMargin time.Duration
	requestMode  string
}

func bindOptions(fs *flag.FlagSet, opts *options) {
	fs.StringVar(&opts.resource, "resource", "wan-pd", "resource name")
	_ = fs.String("supervisor-owner", "", "internal routerd supervisor ownership token")
	fs.StringVar(&opts.ifname, "interface", "", "uplink interface name")
	fs.StringVar(&opts.clientDUID, "client-duid", "", "client DUID hex; default derives DUID-LL from interface MAC")
	fs.UintVar(&opts.iaid, "iaid", 1, "IAID for the requested associations")
	fs.StringVar(&opts.requestMode, "request-mode", "pd", "associations to request: pd, address, or pd-address")
	fs.DurationVar(&opts.timeout, "timeout", 60*time.Second, "overall acquisition timeout for --once")
	fs.StringVar(&opts.socketPath, "socket", "", "Unix socket path")
	fs.StringVar(&opts.leaseFile, "lease-file", "", "lease snapshot path")
	fs.StringVar(&opts.eventFile, "event-file", "", "event JSONL path")
	fs.StringVar(&opts.srcLL, "src-ll", "", "link-local source address for ablation testing")
	fs.StringVar(&opts.srcMAC, "src-mac", "", "source MAC ablation value; reserved for raw transport")
	fs.IntVar(&opts.hopLimit, "hop-limit", 1, "IPv6 multicast hop limit")
	fs.IntVar(&opts.listenPort, "listen-port", 546, "UDP listen port")
	fs.DurationVar(&opts.renewMargin, "renew-margin", 30*time.Second, "renew before T1 by this margin")
	fs.DurationVar(&opts.rebindMargin, "rebind-margin", 30*time.Second, "rebind before T2 by this margin")
}

func parseOptions(name string, args []string) (options, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	once := fs.Bool("once", false, "run one DHCPv6 acquisition and exit")
	_ = once
	var opts options
	bindOptions(fs, &opts)
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if opts.ifname == "" {
		return options{}, errors.New("--interface is required")
	}
	if _, err := requestMode(opts.requestMode); err != nil {
		return options{}, err
	}
	if strings.TrimSpace(opts.srcMAC) != "" {
		return options{}, errors.New("--src-mac requires raw Ethernet transport, which is not implemented in Phase 0")
	}
	if opts.socketPath == "" {
		opts.socketPath = filepath.Join("/run/routerd/dhcpv6-client", opts.resource+".sock")
	}
	if opts.leaseFile == "" {
		opts.leaseFile = filepath.Join("/var/lib/routerd/dhcpv6-client", opts.resource, "lease.json")
	}
	if opts.eventFile == "" {
		opts.eventFile = filepath.Join("/var/lib/routerd/dhcpv6-client", opts.resource, "events.jsonl")
	}
	return opts, nil
}

func daemonCommand(args []string, _ io.Writer) error {
	opts, err := parseOptions("daemon", args)
	if err != nil {
		return err
	}
	telemetry, err := routerotel.Setup(context.Background(), daemonKind, attribute.String("routerd.resource.name", opts.resource))
	if err != nil {
		return err
	}
	defer telemetry.ShutdownGracefully()
	daemon, err := newDHCPv6Daemon(opts)
	if err != nil {
		return err
	}
	daemon.telemetry = telemetry
	daemon.initTelemetry()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	err = daemon.Run(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func onceCommand(args []string, stdout io.Writer) error {
	opts, err := parseOptions("once", args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()

	daemon, err := newDHCPv6Daemon(opts)
	if err != nil {
		return err
	}
	defer daemon.conn.Close()
	if err := daemon.startClient(ctx); err != nil {
		return err
	}
	buf := make([]byte, 65527)
	for {
		daemon.mu.Lock()
		if daemon.client.State == pdclient.StateBound {
			result := daemon.resultLocked()
			daemon.mu.Unlock()
			return writeJSON(stdout, result)
		}
		daemon.mu.Unlock()

		_ = daemon.conn.SetReadDeadline(nextReadDeadline(ctx, time.Now(), 3*time.Second, daemon.nextRetryAt()))
		n, _, err := daemon.conn.ReadFromUDP(buf)
		if err != nil {
			if timeoutError(err) && ctx.Err() == nil {
				err = daemon.tick(ctx)
				if err != nil {
					return err
				}
				continue
			}
			daemon.mu.Lock()
			result := daemon.resultLocked()
			daemon.mu.Unlock()
			_ = writeJSON(stdout, result)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if err := daemon.handlePayload(ctx, buf[:n]); err != nil {
			return err
		}
		if err := daemon.tick(ctx); err != nil {
			return err
		}
	}
}

type dhcpv6Daemon struct {
	cancel        context.CancelFunc
	opts          options
	ifi           *net.Interface
	clientDUID    []byte
	conn          *net.UDPConn
	transport     *udpTransport
	client        *pdclient.Client
	savedSnapshot *pdclient.Snapshot

	startedAt  time.Time
	mu         sync.Mutex
	cond       *sync.Cond
	events     []daemonapi.DaemonEvent
	nextCursor uint64
	recorder   *packetRecorder

	telemetry    *routerotel.Runtime
	leaseGauge   metric.Int64Gauge
	renewCounter metric.Int64Counter
}

type eventsResponse struct {
	Cursor string                  `json:"cursor,omitempty"`
	Events []daemonapi.DaemonEvent `json:"events"`
	More   bool                    `json:"more,omitempty"`
}

func newDHCPv6Daemon(opts options) (*dhcpv6Daemon, error) {
	mode, err := requestMode(opts.requestMode)
	if err != nil {
		return nil, err
	}
	ifi, err := net.InterfaceByName(opts.ifname)
	if err != nil {
		return nil, err
	}
	clientDUID := duidLL(ifi.HardwareAddr)
	if opts.clientDUID != "" {
		clientDUID, err = parseHex(opts.clientDUID)
		if err != nil {
			return nil, fmt.Errorf("client DUID: %w", err)
		}
	}
	// Bind every DHCPv6 client to its selected interface's link-local address.
	// Config permits UDP/546 per interface; a wildcard socket either prevents
	// a second resource from binding or can receive its reply under the wrong
	// resource. An explicit --src-ll remains the operator override.
	if opts.srcLL == "" {
		opts.srcLL, err = interfaceLinkLocalIPv6(ifi)
		if err != nil || !linkLocalUsable(ifi.Name, opts.srcLL) {
			opts.srcLL, err = ensureInterfaceLinkLocalIPv6(ifi)
			if err != nil {
				return nil, fmt.Errorf("DHCPv6 link-local source on %s: %w", opts.ifname, err)
			}
		}
	}
	conn, err := listenDHCPv6(opts.srcLL, opts.ifname, opts.listenPort)
	if err != nil {
		return nil, err
	}
	recorder := &packetRecorder{limit: 1000}
	transport := &udpTransport{conn: conn, ifname: opts.ifname, ifindex: ifi.Index, hopLimit: opts.hopLimit, recorder: recorder}
	client, err := pdclient.New(pdclient.Config{
		Resource:   opts.resource,
		Interface:  opts.ifname,
		ClientDUID: clientDUID,
		IAID:       uint32(opts.iaid),
		Mode:       mode,
	}, transport)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	d := &dhcpv6Daemon{
		opts:       opts,
		ifi:        ifi,
		clientDUID: clientDUID,
		conn:       conn,
		transport:  transport,
		client:     client,
		startedAt:  time.Now().UTC(),
		recorder:   recorder,
	}
	d.cond = sync.NewCond(&d.mu)
	d.initTelemetry()
	return d, nil
}

func interfaceLinkLocalIPv6(ifi *net.Interface) (string, error) {
	addrs, err := ifi.Addrs()
	if err != nil {
		return "", err
	}
	return selectLinkLocalIPv6(addrs)
}

func addressUsableInIPOutput(output, address string) bool {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, address+"/") {
			continue
		}
		return !strings.Contains(line, " tentative") && !strings.Contains(line, " dadfailed")
	}
	return false
}

// addressUsableInIfconfigOutput reports whether exactly address is ready for
// use in FreeBSD ifconfig output. Other link-local addresses must not affect
// the result: a VMAC can have more than one, and DAD is per address.
func addressUsableInIfconfigOutput(output, address string) bool {
	want := net.ParseIP(address)
	if want == nil {
		return false
	}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "inet6" {
			continue
		}
		got := net.ParseIP(strings.SplitN(fields[1], "%", 2)[0])
		if got == nil || !got.Equal(want) {
			continue
		}
		state := strings.ToLower(line)
		return !strings.Contains(state, "tentative") &&
			!strings.Contains(state, "duplicate") &&
			!strings.Contains(state, "dadfailed")
	}
	return false
}

func linkLocalFromMAC(mac net.HardwareAddr) string {
	if len(mac) != 6 {
		return ""
	}
	return net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, mac[0] ^ 0x02, mac[1], mac[2], 0xff, 0xfe, mac[3], mac[4], mac[5]}.String()
}

func selectLinkLocalIPv6(addrs []net.Addr) (string, error) {
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err == nil && ip.To4() == nil && ip.IsLinkLocalUnicast() {
			return ip.String(), nil
		}
	}
	return "", errors.New("no IPv6 link-local address")
}

func (d *dhcpv6Daemon) initTelemetry() {
	if d.telemetry == nil {
		d.telemetry = &routerotel.Runtime{ServiceName: daemonKind}
	}
	d.telemetry.Ensure()
	d.leaseGauge = d.telemetry.Gauge("routerd.dhcpv6.client.lease.state")
	d.renewCounter = d.telemetry.Counter("routerd.dhcpv6.client.renew")
}

func (d *dhcpv6Daemon) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	defer cancel()
	defer d.conn.Close()
	if err := d.prepareFilesystem(); err != nil {
		return err
	}
	if err := d.restoreLease(ctx); err != nil {
		return err
	}
	if err := d.restoreEvents(); err != nil {
		return err
	}
	server, listener, err := d.httpServer()
	if err != nil {
		return err
	}
	defer listener.Close()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
	}()
	go func() { _ = server.Serve(listener) }()

	d.publish(daemonapi.EventDaemonStarted, daemonapi.SeverityInfo, "Started", "DHCPv6 client daemon started", nil)
	if err := d.startClient(ctx); err != nil {
		return err
	}
	d.publish(daemonapi.EventDaemonReady, daemonapi.SeverityInfo, "Ready", "DHCPv6 client daemon is ready", nil)

	buf := make([]byte, 65527)
	for {
		if ctx.Err() != nil {
			d.publish(daemonapi.EventDaemonStopped, daemonapi.SeverityInfo, "Stopped", "DHCPv6 client daemon stopped", nil)
			return ctx.Err()
		}
		_ = d.conn.SetReadDeadline(nextReadDeadline(ctx, time.Now(), 3*time.Second, d.nextRetryAt()))
		n, _, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			if timeoutError(err) {
				if err := d.tick(ctx); err != nil {
					return err
				}
				continue
			}
			return err
		}
		if err := d.handlePayload(ctx, buf[:n]); err != nil {
			return err
		}
		if err := d.tick(ctx); err != nil {
			return err
		}
	}
}

func (d *dhcpv6Daemon) startClient(ctx context.Context) error {
	d.mu.Lock()
	var sentSolicit bool
	defer func() {
		d.mu.Unlock()
		if sentSolicit {
			d.publish(daemonapi.EventDHCPv6SolicitSent, daemonapi.SeverityInfo, "SolicitSent", "sent DHCPv6 Solicit", nil)
		}
	}()
	switch d.client.State {
	case pdclient.StateBound:
		if err := d.client.TickWithMargin(ctx, d.opts.renewMargin, d.opts.rebindMargin); err != nil {
			if errors.Is(err, pdclient.ErrSend) {
				return d.saveLeaseLocked(ctx)
			}
			return err
		}
		return d.saveLeaseLocked(ctx)
	case pdclient.StateRenewing:
		if err := d.client.Renew(ctx); err != nil {
			if errors.Is(err, pdclient.ErrSend) {
				return d.saveLeaseLocked(ctx)
			}
			return err
		}
		return d.saveLeaseLocked(ctx)
	case pdclient.StateRebinding:
		if err := d.client.Rebind(ctx); err != nil {
			if errors.Is(err, pdclient.ErrSend) {
				return d.saveLeaseLocked(ctx)
			}
			return err
		}
		return d.saveLeaseLocked(ctx)
	}
	if err := d.client.Start(ctx); err != nil {
		if errors.Is(err, pdclient.ErrSend) {
			return d.saveLeaseLocked(ctx)
		}
		return err
	}
	sentSolicit = true
	return d.saveLeaseLocked(ctx)
}

func (d *dhcpv6Daemon) handlePayload(ctx context.Context, payload []byte) error {
	msg, err := pdclient.DecodeMessageForIAID(payload, d.client.Config.IAID)
	if err != nil {
		return nil
	}
	d.recorder.add(packetRecordFromMessage("recv", d.opts.ifname, msg, payload))
	d.publishReceiveEvent(msg)
	d.mu.Lock()
	before := d.client.Snapshot()
	if d.savedSnapshot != nil {
		before = *d.savedSnapshot
	}
	// Keep the owner transition for persistence/events even when the public
	// snapshot already reports authority expired at this instant.
	before.State = d.client.State
	err = d.client.HandleMessage(ctx, msg)
	if errors.Is(err, pdclient.ErrSend) {
		err = nil
	}
	after := d.client.Snapshot()
	if err == nil && snapshotChanged(before, after) {
		err = d.saveLeaseLocked(ctx)
	}
	d.mu.Unlock()
	if err != nil {
		return err
	}
	d.publishStateEvents(before, after)
	return nil
}

func (d *dhcpv6Daemon) tick(ctx context.Context) error {
	d.mu.Lock()
	before := d.client.Snapshot()
	if d.savedSnapshot != nil {
		before = *d.savedSnapshot
	}
	before.State = d.client.State
	spanCtx, span := d.telemetry.Tracer.Start(ctx, "dhcpv6.tick", trace.WithAttributes(attribute.String("routerd.resource.name", d.opts.resource)))
	err := d.client.TickWithMargin(ctx, d.opts.renewMargin, d.opts.rebindMargin)
	if errors.Is(err, pdclient.ErrSend) {
		err = nil
	}
	after := d.client.Snapshot()
	if err == nil && snapshotChanged(before, after) {
		err = d.saveLeaseLocked(ctx)
	}
	d.mu.Unlock()
	span.End()
	if err != nil {
		return err
	}
	if (!before.LastSentAt.Equal(after.LastSentAt) || before.LastMessageType != after.LastMessageType) && after.LastMessageType == pdclient.MessageRenew {
		d.renewCounter.Add(spanCtx, 1, metric.WithAttributes(attribute.String("routerd.resource.name", d.opts.resource)))
	}
	d.publishStateEvents(before, after)
	return nil
}

func (d *dhcpv6Daemon) nextRetryAt() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.client.NextRetryAt()
}

func (d *dhcpv6Daemon) restoreLease(ctx context.Context) error {
	data, err := os.ReadFile(d.opts.leaseFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	var snapshot pdclient.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	d.mu.Lock()
	d.client.Restore(snapshot)
	err = d.saveLeaseLocked(ctx)
	d.mu.Unlock()
	return err
}

func (d *dhcpv6Daemon) saveLeaseLocked(_ context.Context) error {
	if err := os.MkdirAll(filepath.Dir(d.opts.leaseFile), 0755); err != nil {
		return err
	}
	snapshot := d.client.Snapshot()
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp := d.opts.leaseFile + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, d.opts.leaseFile); err != nil {
		return err
	}
	d.savedSnapshot = &snapshot
	return nil
}

func (d *dhcpv6Daemon) prepareFilesystem() error {
	if err := os.MkdirAll(filepath.Dir(d.opts.socketPath), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.opts.eventFile), 0755); err != nil {
		return err
	}
	_ = os.Remove(d.opts.socketPath)
	return nil
}

func (d *dhcpv6Daemon) restoreEvents() error {
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
		if len(d.events) > 1000 {
			d.events = append([]daemonapi.DaemonEvent(nil), d.events[len(d.events)-1000:]...)
		}
		if id, err := strconv.ParseUint(event.Cursor, 10, 64); err == nil && id > d.nextCursor {
			d.nextCursor = id
		}
	}
	return scanner.Err()
}

func (d *dhcpv6Daemon) httpServer() (*http.Server, net.Listener, error) {
	listener, err := net.Listen("unix", d.opts.socketPath)
	if err != nil {
		return nil, nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", d.handleStatus)
	mux.HandleFunc("/v1/lease", d.handleLease)
	mux.HandleFunc("/v1/healthz", d.handleHealthz)
	mux.HandleFunc("/v1/events", d.handleEvents)
	mux.HandleFunc("/v1/commands/", d.handleCommand)
	return &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}, listener, nil
}

func (d *dhcpv6Daemon) handleLease(w http.ResponseWriter, _ *http.Request) {
	d.mu.Lock()
	snapshot := d.client.Snapshot()
	d.mu.Unlock()
	writeHTTPJSON(w, snapshot)
}

func (d *dhcpv6Daemon) handleStatus(w http.ResponseWriter, _ *http.Request) {
	d.mu.Lock()
	status := d.statusLocked()
	d.mu.Unlock()
	writeHTTPJSON(w, status)
}

func (d *dhcpv6Daemon) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeHTTPJSON(w, map[string]string{"status": daemonapi.HealthOK})
}

func (d *dhcpv6Daemon) handleEvents(w http.ResponseWriter, r *http.Request) {
	topic := r.URL.Query().Get("topic")
	since := r.URL.Query().Get("since")
	wait := eventWait(r)
	deadline := time.Now().Add(wait)
	if wait > 0 {
		timer := time.AfterFunc(wait, func() {
			d.mu.Lock()
			d.cond.Broadcast()
			d.mu.Unlock()
		})
		defer timer.Stop()
	}
	for {
		d.mu.Lock()
		events, cursor := d.eventsSinceLocked(since, topic)
		if len(events) > 0 || wait == 0 || time.Now().After(deadline) {
			d.mu.Unlock()
			writeHTTPJSON(w, eventsResponse{Cursor: cursor, Events: events})
			return
		}
		d.cond.Wait()
		d.mu.Unlock()
	}
}

func eventWait(r *http.Request) time.Duration {
	const maxWait = 30 * time.Second
	wait, _ := time.ParseDuration(r.URL.Query().Get("wait"))
	if wait < 0 {
		return 0
	}
	if wait > maxWait {
		return maxWait
	}
	return wait
}

func (d *dhcpv6Daemon) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	command := strings.TrimPrefix(r.URL.Path, "/v1/commands/")
	result := daemonapi.CommandResult{
		TypeMeta: daemonapi.TypeMeta{APIVersion: daemonapi.APIVersion, Kind: daemonapi.KindCommandResult},
		Command:  command,
	}
	d.publish(daemonapi.EventCommandReceived, daemonapi.SeverityInfo, "CommandReceived", command, map[string]string{"command": command})
	ctx := r.Context()
	d.mu.Lock()
	var err error
	switch command {
	case daemonapi.CommandRenew:
		err = d.client.Renew(ctx)
	case daemonapi.CommandRebind:
		err = d.client.Rebind(ctx)
	case daemonapi.CommandRelease:
		err = d.client.Release(ctx)
	case daemonapi.CommandInfoRequest:
		err = d.client.InfoRequest(ctx)
		if err == nil && d.conn != nil {
			_ = d.conn.SetReadDeadline(time.Now())
		}
	case daemonapi.CommandReload:
		err = errors.New("worker configuration is supplied by its supervisor; update the Router configuration to restart it")
	case daemonapi.CommandStop:
		if d.cancel == nil {
			err = errors.New("daemon is not running")
		} else {
			defer d.cancel()
		}
	case daemonapi.CommandStart:
		err = d.client.Start(ctx)
	case daemonapi.CommandFlush:
		d.events = nil
		d.recorder.clear()
		err = os.Truncate(d.opts.eventFile, 0)
	default:
		err = fmt.Errorf("unknown command %q", command)
	}
	if errors.Is(err, pdclient.ErrSend) {
		result.Message = "send failed; retry scheduled: " + err.Error()
		err = nil
	}
	if err == nil {
		err = d.saveLeaseLocked(ctx)
	}
	d.mu.Unlock()
	if err != nil {
		result.Accepted = false
		result.Message = err.Error()
		d.publish(daemonapi.EventCommandRejected, daemonapi.SeverityWarning, "CommandRejected", err.Error(), map[string]string{"command": command})
		w.WriteHeader(http.StatusBadRequest)
		writeHTTPJSON(w, result)
		return
	}
	result.Accepted = true
	if result.Message == "" {
		result.Message = "accepted"
	}
	d.publish(daemonapi.EventCommandExecuted, daemonapi.SeverityInfo, "CommandExecuted", command, map[string]string{"command": command})
	writeHTTPJSON(w, result)
}

func (d *dhcpv6Daemon) statusLocked() daemonapi.DaemonStatus {
	snapshot := d.client.Snapshot()
	ready := snapshot.CurrentPrefix != ""
	if snapshot.Mode == pdclient.RequestAddresses {
		ready = len(snapshot.Addresses) > 0
	}
	phase := resourcePhase(snapshot.State)
	if !ready && phase == daemonapi.ResourcePhaseBound {
		phase = daemonapi.ResourcePhasePending
	}
	d.leaseGauge.Record(context.Background(), leaseStateValue(snapshot.State), metric.WithAttributes(
		attribute.String("routerd.resource.name", d.opts.resource),
		attribute.String("routerd.dhcpv6.state", string(snapshot.State)),
	))
	resourceStatus := daemonapi.ResourceStatus{
		Resource: daemonapi.ResourceRef{APIVersion: "net.routerd.net/v1alpha1", Kind: d.resourceKind(), Name: d.opts.resource},
		Phase:    phase,
		Health:   daemonapi.HealthOK,
		Since:    snapshot.UpdatedAt,
		Conditions: []daemonapi.Condition{{
			Type:               "LeaseReady",
			Status:             conditionStatus(ready && (snapshot.State == pdclient.StateBound || snapshot.State == pdclient.StateRenewing || snapshot.State == pdclient.StateRebinding)),
			Reason:             string(snapshot.State),
			LastTransitionTime: snapshot.UpdatedAt,
		}},
		Observed: map[string]string{
			"interface":      d.opts.ifname,
			"leaseState":     string(snapshot.State),
			"currentPrefix":  snapshot.CurrentPrefix,
			"serverDUID":     snapshot.ServerDUID,
			"packetRing":     strconv.Itoa(len(d.recorder.snapshot())),
			"aftrName":       snapshot.AFTRName,
			"dnsServers":     jsonStringList(snapshot.DNSServers),
			"sntpServers":    jsonStringList(snapshot.SNTPServers),
			"domainSearch":   jsonStringList(snapshot.DomainSearch),
			"infoPending":    strconv.FormatBool(snapshot.InfoPending),
			"infoStatusCode": strconv.Itoa(int(snapshot.InfoStatusCode)),
			"infoLastError":  snapshot.InfoLastError,
			"prefixCount":    strconv.Itoa(len(snapshot.Prefixes)),
			"addressCount":   strconv.Itoa(len(snapshot.Addresses)),
			"lastError":      snapshot.LastError,
		},
	}
	return daemonapi.DaemonStatus{
		TypeMeta: daemonapi.TypeMeta{APIVersion: daemonapi.APIVersion, Kind: daemonapi.KindDaemonStatus},
		Daemon:   d.daemonRef(),
		Phase:    daemonapi.PhaseRunning,
		Health:   daemonapi.HealthOK,
		Since:    d.startedAt,
		Resources: []daemonapi.ResourceStatus{
			resourceStatus,
		},
	}
}

func (d *dhcpv6Daemon) eventsSinceLocked(since, topic string) ([]daemonapi.DaemonEvent, string) {
	var out []daemonapi.DaemonEvent
	cursor := since
	sinceID, _ := strconv.ParseUint(since, 10, 64)
	for _, event := range d.events {
		id, _ := strconv.ParseUint(event.Cursor, 10, 64)
		if id <= sinceID {
			continue
		}
		if topic != "" && !matchTopic(topic, event.Type) {
			continue
		}
		out = append(out, event)
		cursor = event.Cursor
	}
	return out, cursor
}

func (d *dhcpv6Daemon) publishReceiveEvent(msg pdclient.Message) {
	switch msg.Type {
	case pdclient.MessageAdvertise:
		d.publish(daemonapi.EventDHCPv6AdvertiseReceived, daemonapi.SeverityInfo, "AdvertiseReceived", "received DHCPv6 Advertise", nil)
	case pdclient.MessageReply:
		d.publish(daemonapi.EventDHCPv6ReplyReceived, daemonapi.SeverityInfo, "ReplyReceived", "received DHCPv6 Reply", nil)
	}
}

func (d *dhcpv6Daemon) publishStateEvents(before, after pdclient.Snapshot) {
	if !snapshotChanged(before, after) {
		return
	}
	if after.Mode != pdclient.RequestAddresses && (before.State != after.State || before.CurrentPrefix != after.CurrentPrefix || !prefixSnapshotsEqual(before.Prefixes, after.Prefixes)) {
		attrs := map[string]string{"prefix": after.CurrentPrefix}
		switch {
		case after.State == pdclient.StateBound && before.State == pdclient.StateRenewing:
			d.publish(daemonapi.EventDHCPv6PrefixRenewed, daemonapi.SeverityInfo, "PrefixRenewed", "delegated prefix renewed", attrs)
		case after.State == pdclient.StateBound && before.State == pdclient.StateRebinding:
			d.publish(daemonapi.EventDHCPv6PrefixRebound, daemonapi.SeverityInfo, "PrefixRebound", "delegated prefix rebound", attrs)
		case after.State == pdclient.StateBound:
			d.publish(daemonapi.EventDHCPv6PrefixBound, daemonapi.SeverityInfo, "PrefixBound", "delegated prefix bound", attrs)
		case after.State == pdclient.StateExpired:
			d.publish(daemonapi.EventDHCPv6PrefixExpired, daemonapi.SeverityWarning, "PrefixExpired", "delegated prefix expired", nil)
		}
	}
	if !addressSnapshotsEqual(before.Addresses, after.Addresses) {
		d.publish(daemonapi.EventDHCPv6AddressesChanged, daemonapi.SeverityInfo, "AddressesChanged", "DHCPv6 address leases changed", map[string]string{"addressCount": strconv.Itoa(len(after.Addresses))})
	}
	if !before.LastSentAt.Equal(after.LastSentAt) || before.LastMessageType != after.LastMessageType {
		switch after.LastMessageType {
		case pdclient.MessageRequest:
			d.publish(daemonapi.EventDHCPv6RequestSent, daemonapi.SeverityInfo, "RequestSent", "sent DHCPv6 Request", nil)
		case pdclient.MessageSolicit:
			d.publish(daemonapi.EventDHCPv6SolicitSent, daemonapi.SeverityInfo, "SolicitSent", "sent DHCPv6 Solicit", nil)
		}
	}
	if !before.InfoLastSentAt.Equal(after.InfoLastSentAt) {
		d.publish(daemonapi.EventDHCPv6InfoRequestSent, daemonapi.SeverityInfo, "InfoRequestSent", "sent DHCPv6 Information-request", nil)
	}
	if infoChanged(before, after) {
		d.publish(daemonapi.EventDHCPv6InfoReplyReceived, daemonapi.SeverityInfo, "InfoReplyReceived", "received DHCPv6 information Reply", map[string]string{
			"aftrName":     after.AFTRName,
			"dnsServers":   strings.Join(after.DNSServers, ","),
			"sntpServers":  strings.Join(after.SNTPServers, ","),
			"domainSearch": strings.Join(after.DomainSearch, ","),
		})
	}
}

func (d *dhcpv6Daemon) publish(topic, severity, reason, message string, attrs map[string]string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.nextCursor++
	event := daemonapi.NewEvent(d.daemonRef(), topic, severity)
	event.Cursor = strconv.FormatUint(d.nextCursor, 10)
	event.Resource = &daemonapi.ResourceRef{APIVersion: "net.routerd.net/v1alpha1", Kind: d.resourceKind(), Name: d.opts.resource}
	event.Reason = reason
	event.Message = message
	event.Attributes = attrs
	d.events = append(d.events, event)
	if len(d.events) > 1000 {
		d.events = append([]daemonapi.DaemonEvent(nil), d.events[len(d.events)-1000:]...)
	}
	d.appendEventFileLocked(event)
	d.cond.Broadcast()
	if d.telemetry != nil && d.telemetry.Enabled && d.telemetry.Logger != nil {
		d.telemetry.Logger.Info(message, "event.type", topic, "resource", d.opts.resource, "reason", reason)
	}
}

func (d *dhcpv6Daemon) appendEventFileLocked(event daemonapi.DaemonEvent) {
	_ = eventfile.AppendJSONLine(d.opts.eventFile, event)
}

func (d *dhcpv6Daemon) daemonRef() daemonapi.DaemonRef {
	return daemonapi.DaemonRef{Name: daemonKind + "-" + d.opts.resource, Kind: daemonKind, Instance: d.opts.resource}
}

func (d *dhcpv6Daemon) resultLocked() struct {
	Snapshot pdclient.Snapshot `json:"snapshot"`
	Sent     []sentPacket      `json:"sent"`
	Packets  []packetRecord    `json:"packets"`
} {
	return struct {
		Snapshot pdclient.Snapshot `json:"snapshot"`
		Sent     []sentPacket      `json:"sent"`
		Packets  []packetRecord    `json:"packets"`
	}{
		Snapshot: d.client.Snapshot(),
		Sent:     append([]sentPacket(nil), d.transport.sent...),
		Packets:  d.recorder.snapshot(),
	}
}

type packetRecorder struct {
	mu      sync.Mutex
	limit   int
	packets []packetRecord
}

type packetRecord struct {
	Time          time.Time `json:"time"`
	Direction     string    `json:"direction"`
	Interface     string    `json:"interface"`
	MessageType   uint8     `json:"messageType"`
	TransactionID string    `json:"transactionID"`
	Bytes         int       `json:"bytes"`
	PayloadHex    string    `json:"payloadHex,omitempty"`
}

func (r *packetRecorder) add(record packetRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.limit == 0 {
		r.limit = 1000
	}
	r.packets = append(r.packets, record)
	if len(r.packets) > r.limit {
		r.packets = append([]packetRecord(nil), r.packets[len(r.packets)-r.limit:]...)
	}
}

func (r *packetRecorder) clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.packets = nil
}

func (r *packetRecorder) snapshot() []packetRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]packetRecord(nil), r.packets...)
}

func packetRecordFromMessage(direction, ifname string, msg pdclient.Message, payload []byte) packetRecord {
	return packetRecord{
		Time:          time.Now().UTC(),
		Direction:     direction,
		Interface:     ifname,
		MessageType:   msg.Type,
		TransactionID: fmt.Sprintf("%06x", msg.TransactionID),
		Bytes:         len(payload),
		PayloadHex:    hex.EncodeToString(payload),
	}
}

type udpTransport struct {
	conn     *net.UDPConn
	ifname   string
	ifindex  int
	hopLimit int
	recorder *packetRecorder
	sent     []sentPacket
}

func (t *udpTransport) Send(ctx context.Context, packet pdclient.OutboundPacket) error {
	if deadline, ok := ctx.Deadline(); ok {
		_ = t.conn.SetWriteDeadline(deadline)
	}
	if err := sendDHCPv6Multicast(t.conn, t.ifindex, t.hopLimit, packet.Payload); err != nil {
		return err
	}
	t.sent = append(t.sent, sentPacket{
		Interface:     t.ifname,
		MessageType:   packet.Message.Type,
		TransactionID: fmt.Sprintf("%06x", packet.Message.TransactionID),
		Bytes:         len(packet.Payload),
	})
	if len(t.sent) > 1000 {
		t.sent = append([]sentPacket(nil), t.sent[len(t.sent)-1000:]...)
	}
	if t.recorder != nil {
		t.recorder.add(packetRecordFromMessage("send", t.ifname, packet.Message, packet.Payload))
	}
	return nil
}

func sendDHCPv6Multicast(conn *net.UDPConn, ifindex, hopLimit int, payload []byte) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var sockErr error
	if err := raw.Write(func(fd uintptr) bool {
		sockErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_MULTICAST_IF, ifindex)
		if sockErr != nil {
			return true
		}
		if hopLimit > 0 {
			sockErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_MULTICAST_HOPS, hopLimit)
			if sockErr != nil {
				return true
			}
		}
		addr := unix.SockaddrInet6{Port: 547, ZoneId: uint32(ifindex)}
		copy(addr.Addr[:], net.ParseIP("ff02::1:2").To16())
		sockErr = unix.Sendto(int(fd), payload, 0, &addr)
		return !errors.Is(sockErr, unix.EAGAIN) && !errors.Is(sockErr, unix.EWOULDBLOCK)
	}); err != nil {
		return err
	}
	return sockErr
}

func listenDHCPv6(srcLL, ifname string, port int) (*net.UDPConn, error) {
	addr, err := dhcpv6ListenAddr(srcLL, ifname, port)
	if err != nil {
		return nil, err
	}
	return net.ListenUDP("udp6", addr)
}

func dhcpv6ListenAddr(srcLL, ifname string, port int) (*net.UDPAddr, error) {
	addr := &net.UDPAddr{IP: net.IPv6unspecified, Port: port}
	if srcLL != "" {
		ip := net.ParseIP(srcLL)
		if ip == nil {
			return nil, fmt.Errorf("--src-ll is not an IP address: %q", srcLL)
		}
		addr = &net.UDPAddr{IP: ip, Port: port, Zone: ifname}
	}
	return addr, nil
}

func nextReadDeadline(ctx context.Context, now time.Time, interval time.Duration, retryAt time.Time) time.Time {
	deadline := now.Add(interval)
	if !retryAt.IsZero() && retryAt.Before(deadline) {
		deadline = retryAt
	}
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		return ctxDeadline
	}
	return deadline
}

func timeoutError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func selftestCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("selftest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	resource := fs.String("resource", "wan-pd", "resource name")
	ifname := fs.String("interface", "wan0", "interface name")
	clientDUIDHex := fs.String("client-duid", "00030001020000000103", "client DUID hex")
	serverDUIDHex := fs.String("server-duid", "00030001020000000001", "server DUID hex")
	prefixText := fs.String("prefix", "2001:db8:1200:1240::/60", "delegated prefix")
	if err := fs.Parse(args); err != nil {
		return err
	}
	clientDUID, err := parseHex(*clientDUIDHex)
	if err != nil {
		return fmt.Errorf("client DUID: %w", err)
	}
	serverDUID, err := parseHex(*serverDUIDHex)
	if err != nil {
		return fmt.Errorf("server DUID: %w", err)
	}
	prefix, err := netip.ParsePrefix(*prefixText)
	if err != nil {
		return fmt.Errorf("prefix: %w", err)
	}
	now := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	transport := &captureTransport{}
	xids := []uint32{0x010203, 0x010204}
	client, err := pdclient.New(pdclient.Config{
		Resource:   *resource,
		Interface:  *ifname,
		ClientDUID: clientDUID,
		IAID:       1,
		Now:        func() time.Time { return now },
		Transaction: func() (uint32, error) {
			if len(xids) == 0 {
				return 0x010205, nil
			}
			xid := xids[0]
			xids = xids[1:]
			return xid, nil
		},
	}, transport)
	if err != nil {
		return err
	}
	if err := client.Start(context.Background()); err != nil {
		return err
	}
	advertise, err := pdclient.EncodeMessage(pdclient.Message{
		Type:          pdclient.MessageAdvertise,
		TransactionID: 0x010203,
		ClientDUID:    clientDUID,
		ServerDUID:    serverDUID,
		IAPDs:         []pdclient.IAPD{{IAID: 1, T1: 7200, T2: 12600, Prefixes: []pdclient.IAPrefix{{Prefix: prefix, Preferred: 14400, Valid: 14400}}}},
	})
	if err != nil {
		return err
	}
	if err := client.Handle(context.Background(), advertise); err != nil {
		return err
	}
	reply, err := pdclient.EncodeMessage(pdclient.Message{
		Type:          pdclient.MessageReply,
		TransactionID: 0x010204,
		ClientDUID:    clientDUID,
		ServerDUID:    serverDUID,
		IAPDs:         []pdclient.IAPD{{IAID: 1, T1: 7200, T2: 12600, Prefixes: []pdclient.IAPrefix{{Prefix: prefix, Preferred: 14400, Valid: 14400}}}},
	})
	if err != nil {
		return err
	}
	if err := client.Handle(context.Background(), reply); err != nil {
		return err
	}
	result := struct {
		Snapshot pdclient.Snapshot `json:"snapshot"`
		Sent     []sentPacket      `json:"sent"`
	}{
		Snapshot: client.Snapshot(),
		Sent:     transport.sent,
	}
	return writeJSON(stdout, result)
}

type sentPacket struct {
	Interface     string `json:"interface"`
	MessageType   uint8  `json:"messageType"`
	TransactionID string `json:"transactionID"`
	Bytes         int    `json:"bytes"`
}

type captureTransport struct {
	sent []sentPacket
}

func (t *captureTransport) Send(_ context.Context, packet pdclient.OutboundPacket) error {
	t.sent = append(t.sent, sentPacket{
		Interface:     packet.Interface,
		MessageType:   packet.Message.Type,
		TransactionID: fmt.Sprintf("%06x", packet.Message.TransactionID),
		Bytes:         len(packet.Payload),
	})
	return nil
}

func parseHex(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	out, err := hex.DecodeString(value)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func duidLL(mac net.HardwareAddr) []byte {
	out := make([]byte, 4+len(mac))
	out[1] = 3
	out[3] = 1
	copy(out[4:], mac)
	return out
}

func resourcePhase(state pdclient.State) string {
	switch state {
	case pdclient.StateIdle, pdclient.StateReleased:
		return daemonapi.ResourcePhaseIdle
	case pdclient.StateSoliciting, pdclient.StateRequesting:
		return daemonapi.ResourcePhaseAcquiring
	case pdclient.StateBound, pdclient.StateRenewing, pdclient.StateRebinding:
		return daemonapi.ResourcePhaseBound
	case pdclient.StateExpired:
		return daemonapi.ResourcePhaseExpired
	default:
		return daemonapi.ResourcePhasePending
	}
}

func conditionStatus(ok bool) string {
	if ok {
		return daemonapi.ConditionTrue
	}
	return daemonapi.ConditionFalse
}

func leaseStateValue(state pdclient.State) int64 {
	switch state {
	case pdclient.StateBound:
		return 2
	case pdclient.StateRenewing, pdclient.StateRebinding:
		return 1
	case pdclient.StateExpired:
		return -2
	default:
		return 0
	}
}

func snapshotChanged(a, b pdclient.Snapshot) bool {
	return a.State != b.State ||
		a.Mode != b.Mode ||
		!addressSnapshotsEqual(a.Addresses, b.Addresses) ||
		!a.AddressUpdatedAt.Equal(b.AddressUpdatedAt) ||
		a.AddressT1Seconds != b.AddressT1Seconds || a.AddressT2Seconds != b.AddressT2Seconds ||
		a.CurrentPrefix != b.CurrentPrefix ||
		!prefixSnapshotsEqual(a.Prefixes, b.Prefixes) ||
		!a.LeaseUpdatedAt.Equal(b.LeaseUpdatedAt) ||
		a.LastError != b.LastError ||
		!a.LastSentAt.Equal(b.LastSentAt) ||
		a.LastMessageType != b.LastMessageType ||
		a.SOLMaxRTSeconds != b.SOLMaxRTSeconds ||
		a.INFMaxRTSeconds != b.INFMaxRTSeconds ||
		a.InfoPending != b.InfoPending ||
		a.InfoStatusCode != b.InfoStatusCode ||
		a.InfoLastError != b.InfoLastError ||
		!a.InfoRefreshAt.Equal(b.InfoRefreshAt) ||
		!a.InfoLastSentAt.Equal(b.InfoLastSentAt) ||
		!previousPrefixSnapshotsEqual(a.PreviousPrefixes, b.PreviousPrefixes) ||
		a.ServerDUID != b.ServerDUID ||
		a.T1Seconds != b.T1Seconds ||
		a.T2Seconds != b.T2Seconds ||
		a.Preferred != b.Preferred ||
		a.Valid != b.Valid ||
		!a.AcquiredAt.Equal(b.AcquiredAt) ||
		!a.RenewAt.Equal(b.RenewAt) ||
		!a.RebindAt.Equal(b.RebindAt) ||
		!a.ExpiresAt.Equal(b.ExpiresAt) ||
		infoChanged(a, b)
}

func prefixSnapshotsEqual(a, b []pdclient.PrefixSnapshot) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Prefix != b[i].Prefix || a[i].Preferred != b[i].Preferred || a[i].Valid != b[i].Valid || !a[i].AcquiredAt.Equal(b[i].AcquiredAt) {
			return false
		}
	}
	return true
}

func previousPrefixSnapshotsEqual(a, b []pdclient.PreviousPrefixSnapshot) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Prefix != b[i].Prefix || !a[i].ExpiresAt.Equal(b[i].ExpiresAt) {
			return false
		}
	}
	return true
}

func infoChanged(a, b pdclient.Snapshot) bool {
	return a.AFTRName != b.AFTRName ||
		!stringSliceEqual(a.DNSServers, b.DNSServers) ||
		!stringSliceEqual(a.SNTPServers, b.SNTPServers) ||
		!stringSliceEqual(a.DomainSearch, b.DomainSearch) ||
		!a.InfoUpdatedAt.Equal(b.InfoUpdatedAt)
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func jsonStringList(values []string) string {
	data, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writeHTTPJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func matchTopic(pattern, topic string) bool {
	if pattern == "" || pattern == topic {
		return true
	}
	p := strings.Split(pattern, ".")
	t := strings.Split(topic, ".")
	var walk func(int, int) bool
	walk = func(i, j int) bool {
		if i == len(p) {
			return j == len(t)
		}
		if p[i] == "**" {
			return walk(i+1, j) || (j < len(t) && walk(i, j+1))
		}
		if j == len(t) {
			return false
		}
		return (p[i] == "*" || p[i] == t[j]) && walk(i+1, j+1)
	}
	return walk(0, 0)
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == "--"+name || strings.HasPrefix(arg, "--"+name+"=") {
			return true
		}
	}
	return false
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerd-dhcpv6-client [--once] --interface IFACE [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  daemon     run the Unix-socket HTTP+JSON daemon")
	fmt.Fprintln(w, "  once       run one DHCPv6 acquisition and print the lease snapshot")
	fmt.Fprintln(w, "             --request-mode pd|address|pd-address selects the associations")
	fmt.Fprintln(w, "  selftest   run an in-process DHCPv6-PD state-machine handshake")
}
