// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
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
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/dpi"
	"github.com/imksoo/routerd/pkg/version"
)

type options struct {
	socket          string
	name            string
	engine          string
	ndpiAgentSocket string
	ndpiReader      string
	timeout         time.Duration
}

type statusResponse struct {
	OK                bool            `json:"ok"`
	Name              string          `json:"name"`
	Version           string          `json:"version"`
	Engine            string          `json:"engine"`
	ActiveEngine      string          `json:"activeEngine"`
	Mode              string          `json:"mode"`
	NDPITool          string          `json:"ndpiTool,omitempty"`
	NDPIToolNote      string          `json:"ndpiToolNote,omitempty"`
	NDPIToolPath      string          `json:"ndpiToolPath,omitempty"`
	NDPIToolAvailable bool            `json:"ndpiToolAvailable,omitempty"`
	NDPIToolUsed      bool            `json:"ndpiToolUsed,omitempty"`
	Agent             *agentStatus    `json:"agent,omitempty"`
	Stats             classifierStats `json:"stats"`
}

type classifyResponse struct {
	dpi.ClassifyResult
	FallbackReason string `json:"fallbackReason,omitempty"`
}

type agentStatus struct {
	Socket        string `json:"socket,omitempty"`
	Available     bool   `json:"available"`
	LibNDPILoaded bool   `json:"libndpiLoaded,omitempty"`
	Error         string `json:"error,omitempty"`
}

type classifierStats struct {
	Requests          int64   `json:"requests"`
	BuiltinPackets    int64   `json:"builtinPackets"`
	AgentPackets      int64   `json:"agentPackets"`
	AgentClassified   int64   `json:"agentClassified"`
	BuiltinClassified int64   `json:"builtinClassified"`
	Fallbacks         int64   `json:"fallbacks"`
	Unknown           int64   `json:"unknown"`
	TimeoutErrors     int64   `json:"timeoutErrors"`
	AgentErrors       int64   `json:"agentErrors"`
	LatencySamples    int64   `json:"latencySamples"`
	AverageLatencyMs  float64 `json:"averageLatencyMs"`
	MaxLatencyMs      float64 `json:"maxLatencyMs"`
	totalLatencyNanos int64
	maxLatencyNanos   int64
}

type classifierRuntime struct {
	opts  options
	mu    sync.Mutex
	stats classifierStats
}

type classifierEngine interface {
	Classify(context.Context, dpi.ClassifyRequest) (dpi.ClassifyResult, error)
}

type builtinEngine struct{}

func (builtinEngine) Classify(_ context.Context, req dpi.ClassifyRequest) (dpi.ClassifyResult, error) {
	return dpi.Classify(req), nil
}

type ndpiAgentEngine struct {
	socket  string
	timeout time.Duration
}

func (e ndpiAgentEngine) Classify(ctx context.Context, req dpi.ClassifyRequest) (dpi.ClassifyResult, error) {
	if strings.TrimSpace(e.socket) == "" {
		return dpi.ClassifyResult{}, errors.New("ndpi agent socket is not configured")
	}
	timeout := e.timeout
	if timeout <= 0 {
		timeout = 200 * time.Millisecond
	}
	data, err := json.Marshal(req)
	if err != nil {
		return dpi.ClassifyResult{}, err
	}
	client := unixHTTPClient(e.socket, timeout)
	defer client.CloseIdleConnections()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/observe-packet", bytes.NewReader(data))
	if err != nil {
		return dpi.ClassifyResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		return dpi.ClassifyResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return dpi.ClassifyResult{}, fmt.Errorf("ndpi agent status %s", resp.Status)
	}
	var result dpi.ClassifyResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return dpi.ClassifyResult{}, err
	}
	if result.Engine == "" {
		result.Engine = "ndpi-agent"
	}
	if result.Source == "" {
		result.Source = "ndpi-agent"
	}
	return result, nil
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
		case "classify":
			return runClassify(args[1:], os.Stdin, stdout)
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
	fmt.Fprintln(w, `usage: routerd-dpi-classifier [daemon|classify|selftest] [options]

Runs a local DPI classifier over a Unix domain socket. nDPI is intentionally
kept outside routerd's static binaries. The classifier can use the built-in
parser or forward observations to an optional routerd-ndpi-agent service, then
falls back to the built-in parser when the agent is unavailable.`)
}

func parseOptions(name string, args []string) (options, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := options{}
	fs.StringVar(&opts.socket, "socket", "/run/routerd/dpi-classifier/default.sock", "Unix socket path")
	fs.StringVar(&opts.name, "name", "default", "classifier instance name")
	fs.StringVar(&opts.engine, "engine", "builtin", "classifier engine: builtin, ndpi-agent, auto")
	fs.StringVar(&opts.ndpiAgentSocket, "ndpi-agent-socket", "/run/routerd/ndpi-agent/default.sock", "optional routerd-ndpi-agent Unix socket")
	fs.StringVar(&opts.ndpiReader, "ndpi-reader", "", "deprecated; ndpiReader is not used for classification")
	fs.DurationVar(&opts.timeout, "timeout", 200*time.Millisecond, "per-request classifier timeout")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	opts.engine = strings.ToLower(strings.TrimSpace(opts.engine))
	switch opts.engine {
	case "", "builtin":
		opts.engine = "builtin"
	case "ndpi-agent", "auto":
	default:
		return options{}, fmt.Errorf("unsupported engine %q", opts.engine)
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
	server := &http.Server{Handler: newHandler(opts), ReadHeaderTimeout: 5 * time.Second}
	fmt.Fprintf(stdout, "routerd-dpi-classifier listening on %s\n", opts.socket)
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func runClassify(args []string, stdin io.Reader, stdout io.Writer) error {
	opts, err := parseOptions("classify", args)
	if err != nil {
		return err
	}
	var req dpi.ClassifyRequest
	if err := json.NewDecoder(stdin).Decode(&req); err != nil {
		return err
	}
	resp := classifyWithFallback(context.Background(), opts, req)
	return json.NewEncoder(stdout).Encode(resp)
}

func runSelftest(args []string, stdout io.Writer) error {
	opts, err := parseOptions("selftest", args)
	if err != nil {
		return err
	}
	req := dpi.ClassifyRequest{Packet: selftestTLSPacket("routerd-dpi-selftest.example")}
	resp := classifyWithFallback(context.Background(), opts, req)
	return json.NewEncoder(stdout).Encode(resp)
}

func newHandler(opts options) http.Handler {
	runtime := &classifierRuntime{opts: opts}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, runtime.status())
	})
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, runtime.status())
	})
	mux.HandleFunc("/v1/classify", func(w http.ResponseWriter, r *http.Request) {
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
		writeJSON(w, runtime.classify(r.Context(), req))
	})
	return mux
}

func (r *classifierRuntime) status() statusResponse {
	resp := status(r.opts)
	r.mu.Lock()
	resp.Stats = r.stats
	r.mu.Unlock()
	return resp
}

func (r *classifierRuntime) classify(ctx context.Context, req dpi.ClassifyRequest) classifyResponse {
	resp := classifyWithRecorder(ctx, r.opts, req, r.recordStats)
	return resp
}

func (r *classifierRuntime) recordStats(update func(*classifierStats)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	update(&r.stats)
}

func status(opts options) statusResponse {
	resp := statusResponse{
		OK:           true,
		Name:         opts.name,
		Version:      version.Version,
		Engine:       opts.engine,
		ActiveEngine: "builtin",
		Mode:         "unix-http-json",
	}
	if opts.ndpiReader != "" {
		resp.NDPITool = opts.ndpiReader
		resp.NDPIToolNote = "deprecated; ndpiReader is not used for classification"
	}
	if opts.engine == "auto" || opts.engine == "ndpi-agent" {
		agent := probeAgent(context.Background(), opts)
		resp.Agent = &agent
		if agent.Available {
			resp.ActiveEngine = "ndpi-agent"
		}
	}
	return resp
}

func classifyWithFallback(ctx context.Context, opts options, req dpi.ClassifyRequest) classifyResponse {
	return classifyWithRecorder(ctx, opts, req, nil)
}

func classifyWithRecorder(ctx context.Context, opts options, req dpi.ClassifyRequest, record func(func(*classifierStats))) classifyResponse {
	start := time.Now()
	defer func() {
		elapsed := time.Since(start)
		recordClassifierStats(record, func(stats *classifierStats) {
			stats.LatencySamples++
			nanos := elapsed.Nanoseconds()
			stats.totalLatencyNanos += nanos
			if nanos > stats.maxLatencyNanos {
				stats.maxLatencyNanos = nanos
			}
			stats.AverageLatencyMs = float64(stats.totalLatencyNanos) / float64(stats.LatencySamples) / float64(time.Millisecond)
			stats.MaxLatencyMs = float64(stats.maxLatencyNanos) / float64(time.Millisecond)
		})
	}()
	recordClassifierStats(record, func(stats *classifierStats) {
		stats.Requests++
	})
	if opts.engine == "ndpi-agent" || opts.engine == "auto" {
		recordClassifierStats(record, func(stats *classifierStats) {
			stats.AgentPackets++
		})
		result, err := (ndpiAgentEngine{socket: opts.ndpiAgentSocket, timeout: opts.timeout}).Classify(ctx, req)
		if err == nil && result.AppName != "" && result.AppName != "unknown" {
			builtin, _ := builtinEngine{}.Classify(ctx, req)
			recordClassifierStats(record, func(stats *classifierStats) {
				stats.AgentClassified++
			})
			return classifyResponse{ClassifyResult: dpi.FinalizeResult(mergeAgentAndBuiltinResult(result, builtin))}
		}
		builtin, _ := builtinEngine{}.Classify(ctx, req)
		recordBuiltinResult(record, builtin)
		recordClassifierStats(record, func(stats *classifierStats) {
			stats.Fallbacks++
			if err != nil {
				stats.AgentErrors++
				if isTimeoutError(err) {
					stats.TimeoutErrors++
				}
			}
		})
		resp := classifyResponse{ClassifyResult: dpi.FinalizeResult(builtin)}
		if err != nil {
			resp.FallbackReason = err.Error()
		} else {
			resp.FallbackReason = "ndpi agent returned unknown"
		}
		return resp
	}
	result, _ := builtinEngine{}.Classify(ctx, req)
	recordBuiltinResult(record, result)
	return classifyResponse{ClassifyResult: dpi.FinalizeResult(result)}
}

func recordBuiltinResult(record func(func(*classifierStats)), result dpi.ClassifyResult) {
	recordClassifierStats(record, func(stats *classifierStats) {
		stats.BuiltinPackets++
		if result.AppName == "" || result.AppName == "unknown" {
			stats.Unknown++
			return
		}
		stats.BuiltinClassified++
	})
}

func recordClassifierStats(record func(func(*classifierStats)), update func(*classifierStats)) {
	if record != nil {
		record(update)
	}
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "timeout")
}

func mergeAgentAndBuiltinResult(agent, builtin dpi.ClassifyResult) dpi.ClassifyResult {
	result := agent
	if result.L3Proto == "" {
		result.L3Proto = builtin.L3Proto
	}
	if result.TransportProtocol == "" {
		result.TransportProtocol = builtin.TransportProtocol
	}
	if result.SrcAddress == "" {
		result.SrcAddress = builtin.SrcAddress
	}
	if result.SrcPort == 0 {
		result.SrcPort = builtin.SrcPort
	}
	if result.DstAddress == "" {
		result.DstAddress = builtin.DstAddress
	}
	if result.DstPort == 0 {
		result.DstPort = builtin.DstPort
	}
	if result.TLSSNI == "" {
		result.TLSSNI = builtin.TLSSNI
	}
	if result.HTTPHost == "" {
		result.HTTPHost = builtin.HTTPHost
	}
	if result.DNSQuery == "" {
		result.DNSQuery = builtin.DNSQuery
	}
	return result
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

func probeAgent(ctx context.Context, opts options) agentStatus {
	status := agentStatus{Socket: opts.ndpiAgentSocket}
	if strings.TrimSpace(opts.ndpiAgentSocket) == "" {
		status.Error = "socket is not configured"
		return status
	}
	client := unixHTTPClient(opts.ndpiAgentSocket, opts.timeout)
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1/healthz", nil)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	resp, err := client.Do(req)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		status.Error = resp.Status
		return status
	}
	var body struct {
		LibNDPILoaded bool   `json:"libndpiLoaded"`
		Reason        string `json:"reason,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		status.Error = err.Error()
		return status
	}
	status.LibNDPILoaded = body.LibNDPILoaded
	status.Available = body.LibNDPILoaded
	if !status.Available {
		status.Error = firstNonEmpty(body.Reason, "libndpi backend is unavailable")
	}
	return status
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func unixHTTPClient(socket string, timeout time.Duration) *http.Client {
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
		return os.Remove(path)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
