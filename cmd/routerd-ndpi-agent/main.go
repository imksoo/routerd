// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"container/list"
	"context"
	"crypto/sha256"
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
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/imksoo/routerd/pkg/dpi"
	"github.com/imksoo/routerd/pkg/version"
)

type options struct {
	socket              string
	name                string
	timeout             time.Duration
	flowTTL             time.Duration
	flowLimit           int
	firstPayloadPackets int
}

type statusResponse struct {
	OK             bool                `json:"ok"`
	Name           string              `json:"name"`
	Version        string              `json:"version"`
	Engine         string              `json:"engine"`
	LibNDPILoaded  bool                `json:"libndpiLoaded"`
	LibNDPIVersion string              `json:"libndpiVersion,omitempty"`
	Mode           string              `json:"mode"`
	Reason         string              `json:"reason,omitempty"`
	FlowTTL        string              `json:"flowTTL"`
	FlowLimit      int                 `json:"flowLimit"`
	FirstPackets   int                 `json:"firstPayloadPackets"`
	Stats          agentStats          `json:"stats"`
	Selftest       *dpi.ClassifyResult `json:"selftest,omitempty"`
}

type agentStats struct {
	ActiveFlows       int   `json:"activeFlows"`
	ObservedPackets   int64 `json:"observedPackets"`
	BackendPackets    int64 `json:"backendPackets"`
	ClassifiedPackets int64 `json:"classifiedPackets"`
	UnknownPackets    int64 `json:"unknownPackets"`
	SkippedPackets    int64 `json:"skippedPackets"`
	ErrorPackets      int64 `json:"errorPackets"`
	PrunedFlows       int64 `json:"prunedFlows"`
}

type flowState struct {
	firstSeen time.Time
	lastSeen  time.Time
	packets   int
	result    dpi.ClassifyResult
	position  *list.Element
	done      bool
}

type backendStatus struct {
	Loaded  bool
	Version string
	Reason  string
}

type ndpiBackend interface {
	Status() backendStatus
	Classify(context.Context, string, dpi.ClassifyRequest, *flowState) (dpi.ClassifyResult, error)
	Forget(string)
	Close()
}

type agent struct {
	opts    options
	backend ndpiBackend

	operationMu sync.Mutex
	mu          sync.Mutex
	flows       map[string]*flowState
	stats       agentStats
	order       list.List
}

func newAgent(opts options, backend ndpiBackend) *agent {
	if opts.flowTTL <= 0 {
		opts.flowTTL = time.Hour
	}
	if opts.flowLimit <= 0 {
		opts.flowLimit = 100000
	}
	if opts.firstPayloadPackets <= 0 {
		opts.firstPayloadPackets = 10
	}
	if backend == nil {
		backend = newBackend(opts)
	}
	return &agent{opts: opts, backend: backend, flows: map[string]*flowState{}}
}

func (a *agent) Close() {
	if a == nil || a.backend == nil {
		return
	}
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	a.mu.Lock()
	a.flows = map[string]*flowState{}
	a.order.Init()
	a.mu.Unlock()
	a.backend.Close()
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
			return runDaemon(args[1:], stdout)
		case "selftest":
			return runSelftest(args[1:], stdout)
		case "help", "-h", "--help":
			usage(stdout)
			return nil
		}
	}
	return runDaemon(args, stdout)
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `usage: routerd-ndpi-agent [daemon|selftest] [options]

Runs the optional nDPI analysis service for routerd. This process owns DPI flow
state and exposes a Unix-socket API used by routerd-dpi-classifier. The default
build provides the service boundary without native classification; libndpi-backed
classification is enabled by building with CGO and the libndpi build tag.`)
}

func parseOptions(name string, args []string) (options, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := options{}
	fs.StringVar(&opts.socket, "socket", "/run/routerd/ndpi-agent/default.sock", "Unix socket path")
	fs.StringVar(&opts.name, "name", "default", "agent instance name")
	fs.DurationVar(&opts.timeout, "timeout", 200*time.Millisecond, "per-request timeout")
	fs.DurationVar(&opts.flowTTL, "flow-ttl", time.Hour, "DPI flow state retention")
	fs.IntVar(&opts.flowLimit, "flow-limit", 100000, "maximum retained DPI flow states")
	fs.IntVar(&opts.firstPayloadPackets, "first-payload-packets", 10, "maximum payload-bearing packets inspected per flow")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if opts.flowLimit < 0 {
		return options{}, errors.New("--flow-limit must be non-negative")
	}
	if opts.firstPayloadPackets < 0 {
		return options{}, errors.New("--first-payload-packets must be non-negative")
	}
	return opts, nil
}

func runDaemon(args []string, stdout io.Writer) error {
	opts, err := parseOptions("daemon", args)
	if err != nil {
		return err
	}
	if opts.socket == "" {
		return errors.New("--socket is required")
	}
	if err := os.MkdirAll(filepath.Dir(opts.socket), 0o755); err != nil {
		return err
	}
	if err := removeStaleSocket(opts.socket); err != nil {
		return err
	}
	listener, err := net.Listen("unix", opts.socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chmod(opts.socket, 0o660); err != nil {
		return err
	}
	agent := newAgent(opts, nil)
	defer agent.Close()
	server := &http.Server{Handler: newHandler(agent), ReadHeaderTimeout: 5 * time.Second}
	fmt.Fprintf(stdout, "routerd-ndpi-agent listening on %s\n", opts.socket)
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func runSelftest(args []string, stdout io.Writer) error {
	opts, err := parseOptions("selftest", args)
	if err != nil {
		return err
	}
	agent := newAgent(opts, nil)
	defer agent.Close()
	resp := agent.Status()
	selftest := agent.Observe(context.Background(), dpi.ClassifyRequest{Packet: selftestTLSPacket("routerd-ndpi-selftest.example")}, time.Now().UTC())
	resp.Selftest = &selftest
	return json.NewEncoder(stdout).Encode(resp)
}

func newHandler(agent *agent) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, agent.Status())
	})
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, agent.Status())
	})
	mux.HandleFunc("/v1/observe-packet", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()
		var req dpi.ClassifyRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		result := agent.Observe(r.Context(), req, time.Now().UTC())
		writeJSON(w, result)
	})
	return mux
}

func (a *agent) Status() statusResponse {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	backend := a.backend.Status()
	stats := a.snapshotStats(time.Now().UTC())
	return statusResponse{
		// A reachable agent without its classifier must not report a healthy DPI
		// capability.  FreeBSD currently uses the explicit unavailable backend.
		OK:             backend.Loaded,
		Name:           a.opts.name,
		Version:        version.Version,
		Engine:         "ndpi-agent",
		LibNDPILoaded:  backend.Loaded,
		LibNDPIVersion: backend.Version,
		Mode:           "unix-http-json",
		Reason:         backend.Reason,
		FlowTTL:        a.opts.flowTTL.String(),
		FlowLimit:      a.opts.flowLimit,
		FirstPackets:   a.opts.firstPayloadPackets,
		Stats:          stats,
	}
}

func (a *agent) Observe(ctx context.Context, req dpi.ClassifyRequest, now time.Time) dpi.ClassifyResult {
	// One owner spans flow selection, native mutation and result publication.
	// This also prevents an expired generation from recreating a newer flow.
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	if err := ctx.Err(); err != nil {
		result := metadataOnlyResult(req)
		result.Reason = err.Error()
		return result
	}
	key := flowKey(req)
	if key == "" {
		result := metadataOnlyResult(req)
		result.Engine = "ndpi-agent"
		result.Source = "ndpi-agent"
		result.Reason = "flow_tuple_unavailable"
		a.addStats(func(stats *agentStats) {
			stats.ObservedPackets++
			stats.SkippedPackets++
		})
		return result
	}
	payloadBearing := len(req.L4Payload) > 0 || len(req.Payload) > 0
	if len(req.Packet) > 0 {
		info, _ := dpi.InspectPacket(req.Packet)
		payloadBearing = len(info.Payload) > 0
	}
	state, shouldClassify := a.prepareFlow(key, now, payloadBearing)
	if !shouldClassify {
		a.backend.Forget(key)
		if state.result.AppName != "" && state.result.AppName != "unknown" {
			return resultForPacket(state.result, req)
		}
		result := metadataOnlyResult(req)
		result.Engine = "ndpi-agent"
		result.Source = "ndpi-agent"
		result.Reason = "flow_packet_limit_reached"
		return result
	}
	result, err := a.backend.Classify(ctx, key, req, state)
	if err != nil {
		result = metadataOnlyResult(req)
		result.Engine = "ndpi-agent"
		result.Source = "ndpi-agent"
		result.Reason = "backend_error:" + err.Error()
		a.finishFlow(key, now, result, false, true)
		return result
	}
	classified := result.AppName != "" && result.AppName != "unknown"
	result = resultForPacket(result, req)
	a.finishFlow(key, now, result, classified, false)
	return result
}

func (a *agent) prepareFlow(key string, now time.Time, payloadBearing bool) (*flowState, bool) {
	a.mu.Lock()
	a.stats.ObservedPackets++
	forget := a.pruneLocked(now)
	state := a.flows[key]
	if state == nil {
		state = &flowState{firstSeen: now, position: a.order.PushBack(key)}
		a.flows[key] = state
		state.lastSeen = now
		forget = append(forget, a.pruneLocked(now)...)
	}
	state.lastSeen = now
	a.order.MoveToBack(state.position)
	if payloadBearing {
		state.packets++
	}
	if state.done {
		a.stats.SkippedPackets++
		clone := cloneFlowState(state)
		a.mu.Unlock()
		a.forgetFlows(forget)
		return clone, false
	}
	if state.packets > a.opts.firstPayloadPackets {
		state.done = true
		a.stats.SkippedPackets++
		clone := cloneFlowState(state)
		a.mu.Unlock()
		a.forgetFlows(append(forget, key))
		return clone, false
	}
	a.stats.BackendPackets++
	clone := cloneFlowState(state)
	a.mu.Unlock()
	a.forgetFlows(forget)
	return clone, true
}

func (a *agent) finishFlow(key string, now time.Time, result dpi.ClassifyResult, classified bool, failed bool) {
	a.mu.Lock()
	state := a.flows[key]
	if state == nil {
		a.mu.Unlock()
		return
	}
	state.lastSeen = now
	state.result = result
	if classified {
		state.done = true
		a.stats.ClassifiedPackets++
	} else if failed {
		a.stats.ErrorPackets++
	} else {
		a.stats.UnknownPackets++
	}
	a.mu.Unlock()
	if classified || failed {
		a.backend.Forget(key)
	}
}

func (a *agent) snapshotStats(now time.Time) agentStats {
	a.mu.Lock()
	forget := a.pruneLocked(now)
	stats := a.stats
	stats.ActiveFlows = len(a.flows)
	a.mu.Unlock()
	a.forgetFlows(forget)
	return stats
}

func (a *agent) addStats(update func(*agentStats)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	update(&a.stats)
}

func (a *agent) pruneLocked(now time.Time) []string {
	var forget []string
	for front := a.order.Front(); front != nil; front = a.order.Front() {
		key := front.Value.(string)
		state := a.flows[key]
		expired := a.opts.flowTTL > 0 && !now.Before(state.lastSeen.Add(a.opts.flowTTL))
		if !expired && (a.opts.flowLimit <= 0 || len(a.flows) <= a.opts.flowLimit) {
			break
		}
		a.order.Remove(front)
		delete(a.flows, key)
		forget = append(forget, key)
		a.stats.PrunedFlows++
	}
	return forget
}

func (a *agent) forgetFlows(keys []string) {
	for _, key := range keys {
		a.backend.Forget(key)
	}
}

func cloneFlowState(state *flowState) *flowState {
	if state == nil {
		return nil
	}
	clone := *state
	clone.position = nil
	return &clone
}

func metadataOnlyResult(req dpi.ClassifyRequest) dpi.ClassifyResult {
	meta := dpi.ClassifyResult{L3Proto: req.L3Proto, TransportProtocol: strings.ToLower(req.TransportProtocol), SrcAddress: req.SrcAddress, SrcPort: req.SrcPort, DstAddress: req.DstAddress, DstPort: req.DstPort}
	if len(req.Packet) > 0 {
		info, ok := dpi.InspectPacket(req.Packet)
		if !ok {
			return dpi.ClassifyResult{AppName: "unknown", Engine: "ndpi-agent", Source: "ndpi-agent"}
		}
		meta = dpi.ClassifyResult{L3Proto: info.L3Proto, TransportProtocol: info.TransportProtocol, SrcAddress: info.SrcAddress, SrcPort: info.SrcPort, DstAddress: info.DstAddress, DstPort: info.DstPort}
	}
	meta.AppName, meta.Engine, meta.Source = "unknown", "ndpi-agent", "ndpi-agent"
	return dpi.FinalizeResult(meta)
}

func resultForPacket(result dpi.ClassifyResult, req dpi.ClassifyRequest) dpi.ClassifyResult {
	meta := metadataOnlyResult(req)
	result.L3Proto, result.TransportProtocol = meta.L3Proto, meta.TransportProtocol
	result.SrcAddress, result.SrcPort = meta.SrcAddress, meta.SrcPort
	result.DstAddress, result.DstPort = meta.DstAddress, meta.DstPort
	return result
}

func flowKey(req dpi.ClassifyRequest) string {
	meta := metadataOnlyResult(req)
	proto := strings.ToLower(strings.TrimSpace(meta.TransportProtocol))
	src, srcErr := netip.ParseAddr(meta.SrcAddress)
	dst, dstErr := netip.ParseAddr(meta.DstAddress)
	if proto == "" || srcErr != nil || dstErr != nil || src.Is4() != dst.Is4() || meta.SrcPort < 0 || meta.SrcPort > 65535 || meta.DstPort < 0 || meta.DstPort > 65535 {
		return ""
	}
	if len(req.Packet) > 0 {
		info, ok := dpi.InspectPacket(req.Packet)
		if !ok || ((proto == "tcp" || proto == "udp") && info.SrcPort == 0 && info.DstPort == 0 && len(info.Payload) == 0) {
			return ""
		}
	}
	left, right := netip.AddrPortFrom(src.Unmap(), uint16(meta.SrcPort)).String(), netip.AddrPortFrom(dst.Unmap(), uint16(meta.DstPort)).String()
	if right < left {
		left, right = right, left
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{proto, left, right}, "|")))
	return hex.EncodeToString(sum[:16])
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func unixHTTPClient(socket string, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 200 * time.Millisecond
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socket)
		}},
	}
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("refusing to remove non-socket path %s", path)
		}
		conn, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			return fmt.Errorf("socket %s is already active", path)
		}
		if !errors.Is(dialErr, syscall.ECONNREFUSED) && !errors.Is(dialErr, os.ErrNotExist) {
			return fmt.Errorf("inspect socket %s before removal: %w", path, dialErr)
		}
		return os.Remove(path)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func selftestTLSPacket(host string) []byte {
	payload := dpi.MinimalTLSClientHello(host)
	packet := append([]byte{
		0x45, 0x00, 0x00, 0x00, 0, 0, 0, 0, 64, 6, 0, 0,
		192, 0, 2, 10,
		198, 51, 100, 10,
		0xcf, 0xb0, 0x01, 0xbb,
		0, 0, 0, 0,
		0, 0, 0, 0,
		0x50, 0x18, 0, 0, 0, 0, 0, 0,
	}, payload...)
	packet[2] = byte(len(packet) >> 8)
	packet[3] = byte(len(packet))
	return packet
}
