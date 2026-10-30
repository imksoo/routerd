// SPDX-License-Identifier: BSD-3-Clause

//go:build linux || freebsd

// This native-test helper owns one foreground child. The shell controls that
// actual child over a private socket; it never turns a saved PID into authority.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type childStatus struct {
	PID      int    `json:"pid"`
	Running  bool   `json:"running"`
	ExitCode int    `json:"exitCode"`
	Error    string `json:"error,omitempty"`
}

type childOwner struct {
	command *exec.Cmd
	mu      sync.Mutex
	stopMu  sync.Mutex
	status  childStatus
	done    chan struct{}
}

func (c *childOwner) snapshot() childStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *childOwner) stop(kill bool) error {
	c.stopMu.Lock()
	defer c.stopMu.Unlock()
	if !c.snapshot().Running {
		if reason := c.snapshot().Error; reason != "" {
			return errors.New(reason)
		}
		return nil
	}
	sig := syscall.SIGTERM
	if kill {
		sig = syscall.SIGKILL
	}
	// os.Process coordinates Signal with Wait. FreeBSD wait6(WNOWAIT) marks
	// this direct child done before reaping, preventing a reused-PID signal.
	if err := c.command.Process.Signal(sig); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	select {
	case <-c.done:
		if reason := c.snapshot().Error; reason != "" {
			return errors.New(reason)
		}
		return nil
	case <-time.After(8 * time.Second):
		if err := c.command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
	}
	select {
	case <-c.done:
		if reason := c.snapshot().Error; reason != "" {
			return errors.New(reason)
		}
		return nil
	case <-time.After(2 * time.Second):
		return errors.New("native test child did not acknowledge exit")
	}
}

func writeIntent(path string, args []string) error {
	data, err := json.Marshal(struct {
		Command []string `json:"command"`
	}{args})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func serveChild(ctx context.Context, socket string, args []string) error {
	if len(args) == 0 || !filepath.IsAbs(socket) {
		return errors.New("private absolute control socket and child command are required")
	}
	if err := writeIntent(socket+".intent.json", args); err != nil {
		return err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chmod(socket, 0600); err != nil {
		return err
	}
	return serveChildControl(ctx, listener, args)
}

func serveChildControl(ctx context.Context, listener net.Listener, args []string) error {
	command := exec.Command(args[0], args[1:]...)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := command.Start(); err != nil {
		return err
	}
	owner := &childOwner{command: command, status: childStatus{PID: command.Process.Pid, Running: true, ExitCode: -1}, done: make(chan struct{})}
	go func() {
		err := command.Wait()
		owner.mu.Lock()
		owner.status.Running = false
		if command.ProcessState != nil {
			owner.status.ExitCode = command.ProcessState.ExitCode()
		}
		var exit *exec.ExitError
		if err != nil && !errors.As(err, &exit) {
			owner.status.Error = err.Error()
		}
		owner.mu.Unlock()
		close(owner.done)
	}()
	mux := http.NewServeMux()
	server := &http.Server{Handler: mux, ReadHeaderTimeout: time.Second, WriteTimeout: 12 * time.Second}
	closed := make(chan struct{})
	var closeOnce sync.Once
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET required", http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(owner.snapshot())
	})
	for _, path := range []string{"/term", "/kill", "/close"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "POST required", http.StatusMethodNotAllowed)
				return
			}
			if r.URL.Path == "/close" {
				if owner.snapshot().Running {
					http.Error(w, "child is still running", http.StatusConflict)
					return
				}
				closeOnce.Do(func() { close(closed) })
			} else if err := owner.stop(r.URL.Path == "/kill"); err != nil {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			_ = json.NewEncoder(w).Encode(owner.snapshot())
		})
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
	case <-closed:
	case err := <-serveDone:
		if !errors.Is(err, http.ErrServerClosed) {
			_ = owner.stop(false)
			return err
		}
	}
	if stopErr := owner.stop(false); owner.snapshot().Running || stopErr != nil && owner.snapshot().Error != "" {
		return errors.Join(stopErr, errors.New("native child exit is unconfirmed"))
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "interface" {
		if err := interfaceCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	flags := flag.NewFlagSet("freebsd-lifecycle-runtime", flag.ExitOnError)
	socket := flags.String("socket", "", "private child control socket")
	_ = flags.Parse(os.Args[1:])
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	parent := os.Getppid()
	if parent <= 1 {
		fmt.Fprintln(os.Stderr, "native test launcher has no live invoking parent")
		os.Exit(1)
	}
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if os.Getppid() != parent {
					cancel()
					return
				}
			}
		}
	}()
	if err := serveChild(ctx, *socket, flags.Args()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
