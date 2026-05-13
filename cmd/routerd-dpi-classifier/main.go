// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"routerd/pkg/dpi"
	"routerd/pkg/version"
)

type options struct {
	socket     string
	name       string
	ndpiReader string
	timeout    time.Duration
}

type statusResponse struct {
	OK                bool   `json:"ok"`
	Name              string `json:"name"`
	Version           string `json:"version"`
	Engine            string `json:"engine"`
	NDPITool          string `json:"ndpiTool,omitempty"`
	NDPIToolPath      string `json:"ndpiToolPath,omitempty"`
	NDPIToolAvailable bool   `json:"ndpiToolAvailable"`
	Mode              string `json:"mode"`
}

type classifyResponse struct {
	dpi.ClassifyResult
	NDPITool          string `json:"ndpiTool,omitempty"`
	NDPIToolPath      string `json:"ndpiToolPath,omitempty"`
	NDPIToolAvailable bool   `json:"ndpiToolAvailable"`
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
kept outside routerd's static binary: when ndpiReader is installed it is
reported as an available runtime tool, while the built-in parser provides the
Phase 3.7.1 TLS-SNI/HTTP/DNS PoC path.`)
}

func parseOptions(name string, args []string) (options, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := options{}
	fs.StringVar(&opts.socket, "socket", "/run/routerd/dpi-classifier/default.sock", "Unix socket path")
	fs.StringVar(&opts.name, "name", "default", "classifier instance name")
	fs.StringVar(&opts.ndpiReader, "ndpi-reader", "ndpiReader", "external nDPI reader command")
	fs.DurationVar(&opts.timeout, "timeout", 2*time.Second, "external tool probe timeout")
	if err := fs.Parse(args); err != nil {
		return options{}, err
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
	_ = os.Remove(opts.socket)
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
	resp := classifyResponse{ClassifyResult: dpi.Classify(req)}
	resp.NDPITool = opts.ndpiReader
	resp.NDPIToolPath, resp.NDPIToolAvailable = findTool(opts.ndpiReader)
	return json.NewEncoder(stdout).Encode(resp)
}

func runSelftest(args []string, stdout io.Writer) error {
	opts, err := parseOptions("selftest", args)
	if err != nil {
		return err
	}
	req := dpi.ClassifyRequest{L4Payload: dpi.MinimalTLSClientHello("routerd-dpi-selftest.example"), TransportProtocol: "tcp", DstPort: 443}
	resp := classifyResponse{ClassifyResult: dpi.Classify(req)}
	resp.NDPITool = opts.ndpiReader
	resp.NDPIToolPath, resp.NDPIToolAvailable = findTool(opts.ndpiReader)
	return json.NewEncoder(stdout).Encode(resp)
}

func newHandler(opts options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, status(opts))
	})
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, status(opts))
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
		resp := classifyResponse{ClassifyResult: dpi.Classify(req)}
		resp.NDPITool = opts.ndpiReader
		resp.NDPIToolPath, resp.NDPIToolAvailable = findTool(opts.ndpiReader)
		writeJSON(w, resp)
	})
	return mux
}

func status(opts options) statusResponse {
	path, ok := findTool(opts.ndpiReader)
	return statusResponse{
		OK:                true,
		Name:              opts.name,
		Version:           version.Version,
		Engine:            "routerd-dpi-parser",
		NDPITool:          opts.ndpiReader,
		NDPIToolPath:      path,
		NDPIToolAvailable: ok,
		Mode:              "subprocess-ipc",
	}
}

func findTool(name string) (string, bool) {
	if strings.TrimSpace(name) == "" {
		return "", false
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}
	return path, true
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func unixHTTPClient(socket string, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socket)
		}},
	}
}
