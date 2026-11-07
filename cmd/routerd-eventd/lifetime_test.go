// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
)

type pipeListener struct {
	connection chan net.Conn
	closed     chan struct{}
	once       sync.Once
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connection:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}
func (l *pipeListener) Close() error   { l.once.Do(func() { close(l.closed) }); return nil }
func (l *pipeListener) Addr() net.Addr { return &net.UnixAddr{Name: "memory", Net: "unix"} }

func TestReceiverShutdownWaitsForActiveStateHandler(t *testing.T) {
	listener := &pipeListener{connection: make(chan net.Conn, 1), closed: make(chan struct{})}
	serverConn, clientConn := net.Pipe()
	listener.connection <- serverConn
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- serveReceiver(ctx, listener, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(entered)
			<-release
			w.WriteHeader(http.StatusNoContent)
		}))
	}()
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true, DialContext: func(context.Context, string, string) (net.Conn, error) { return clientConn, nil }}}
	responseDone := make(chan error, 1)
	go func() {
		response, err := client.Get("http://memory/")
		if err == nil {
			_, err = io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
		responseDone <- err
	}()
	<-entered
	cancel()
	<-listener.closed
	select {
	case err := <-done:
		t.Fatalf("server returned before active write ended: %v", err)
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-responseDone; err != nil {
		t.Fatal(err)
	}
}
