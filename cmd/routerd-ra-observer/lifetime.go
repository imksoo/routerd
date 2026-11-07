// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

func (d *daemon) run(ctx context.Context) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Establish the control endpoint before starting receive or file workers.
	// A failed listener must not leave a background capture owner behind.
	server, listener, err := d.httpServer()
	if err != nil {
		return err
	}
	defer func() {
		if err := listener.Close(); !errors.Is(err, net.ErrClosed) {
			result = errors.Join(result, err)
		}
	}()
	defer os.Remove(d.opts.socketPath)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	d.cancel = cancel
	writerCtx, stopWriter := context.WithCancel(context.WithoutCancel(ctx))
	defer stopWriter()
	observed, written, served := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() { defer close(written); d.writeEvents(writerCtx) }()
	go func() { defer close(observed); d.observe(ctx) }()
	go func() { served <- server.Serve(listener) }()
	var serveErr error
	serveFinished := false
	select {
	case <-ctx.Done():
	case serveErr = <-served:
		serveFinished = true
	}
	cancel()
	close(d.stopping)
	shutdown, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	if err := server.Shutdown(shutdown); err != nil {
		result = errors.Join(result, err, server.Close())
	}
	if !serveFinished {
		serveErr = <-served
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		result = errors.Join(result, serveErr)
	}
	return errors.Join(result, d.finishWorkers(shutdown, observed, written))
}

func (d *daemon) finishWorkers(ctx context.Context, observed, written <-chan struct{}) error {
	select {
	case <-observed:
	case <-ctx.Done():
		return fmt.Errorf("RA capture did not join: %w", ctx.Err())
	}
	// Only a joined producer permits closing its queue. Regular filesystem
	// syscalls cannot be forcibly interrupted in-process; a missed budget is
	// an explicit failure, never a successful shutdown acknowledgement.
	close(d.fileQueue)
	select {
	case <-written:
	case <-ctx.Done():
		return fmt.Errorf("RA event writer did not join: %w", ctx.Err())
	}
	d.mu.Lock()
	lost := d.fileErrors + d.fileDropped
	d.mu.Unlock()
	if lost != 0 {
		return fmt.Errorf("RA event file lost %d events; see eventFileErrors/eventFileDropped", lost)
	}
	return nil
}
