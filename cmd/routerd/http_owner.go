// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
)

type servedHTTPConfig struct {
	Handler   http.Handler
	TLS       *tls.Config
	Streaming bool
}

type serveHTTPServers struct {
	mu                   sync.Mutex
	control              ownedHTTPServer
	web                  ownedHTTPServer
	controlConfiguration func(*api.Router) (controlAPIHTTPConfig, error)
	controlHandler       http.Handler
	webConfiguration     func(*api.Router) (api.WebConsoleSpec, bool, error)
	newWebHandler        func(api.WebConsoleSpec, *api.Router) http.Handler
	webSpec              *api.WebConsoleSpec
	webHandler           http.Handler
	stopping             func() bool
}

type preparedServeHTTPServers struct {
	owner      *serveHTTPServers
	control    *preparedHTTPServer
	web        *preparedHTTPServer
	webSpec    *api.WebConsoleSpec
	webHandler http.Handler
}

func (s *serveHTTPServers) prepare(router *api.Router) (*preparedServeHTTPServers, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping != nil && s.stopping() {
		return nil, errServeMutationStopping
	}
	cfg, err := s.controlConfiguration(router)
	if err != nil {
		return nil, err
	}
	var control *servedHTTPConfig
	if cfg.Enabled {
		control = &servedHTTPConfig{Handler: controlAPIAdmissionHandler(s.controlHandler, cfg.AllowPrefixes, cfg.Token)}
		if cfg.TLS.CertFile != "" {
			control.TLS, err = controlAPIServerTLSConfig(cfg.TLS)
			if err != nil {
				return nil, err
			}
		}
	}
	prepared := &preparedServeHTTPServers{owner: s}
	prepared.control, err = s.control.prepare(cfg.Listen, control)
	if err != nil {
		return nil, err
	}
	spec, enabled, err := s.webConfiguration(router)
	if err != nil {
		prepared.close()
		return nil, err
	}
	var web *servedHTTPConfig
	listen := ""
	if enabled {
		handler := s.webHandler
		if !sameWebHTTPConfig(s.webSpec, spec) {
			handler = s.newWebHandler(spec, router)
		}
		prepared.webSpec, prepared.webHandler = &spec, handler
		web = &servedHTTPConfig{Handler: handler, Streaming: true}
		listen = net.JoinHostPort(spec.ListenAddress, fmt.Sprint(spec.Port))
	}
	prepared.web, err = s.web.prepare(listen, web)
	if err != nil {
		prepared.close()
		return nil, err
	}
	return prepared, nil
}

func (s *serveHTTPServers) reconcile(observation controllerchain.RuntimeRouterObservation) error {
	prepared, err := s.prepare(observation.EffectiveRouter)
	if err != nil {
		return err
	}
	defer prepared.close()
	return prepared.publish()
}

func (p *preparedServeHTTPServers) close() {
	if p != nil {
		p.control.close()
		p.web.close()
	}
}

func (p *preparedServeHTTPServers) publish() error {
	p.owner.mu.Lock()
	defer p.owner.mu.Unlock()
	if p.owner.stopping != nil && p.owner.stopping() {
		return errServeMutationStopping
	}
	if err := p.control.publish(); err != nil {
		return err
	}
	if err := p.web.publish(); err != nil {
		return err
	}
	p.owner.webSpec, p.owner.webHandler = p.webSpec, p.webHandler
	return nil
}

func sameWebHTTPConfig(current *api.WebConsoleSpec, next api.WebConsoleSpec) bool {
	return current != nil && current.ListenAddress == next.ListenAddress && current.Port == next.Port && current.BasePath == next.BasePath && current.Title == next.Title && slices.Equal(current.Links, next.Links)
}

func (s *serveHTTPServers) close(ctx context.Context) error {
	controlDone := make(chan error, 1)
	go func() { controlDone <- s.control.close(ctx) }()
	webErr := s.web.close(ctx)
	return errors.Join(webErr, <-controlDone)
}

// ownedHTTPServer owns one current listener and any admitted requests draining
// from replaced listeners. Prepared listeners do not accept until publication.
type ownedHTTPServer struct {
	mu      sync.Mutex
	current *httpServerInstance
	retired []*httpServerInstance
	closed  bool
}

type httpServerInstance struct {
	listen   string
	listener net.Listener
	server   *http.Server
	config   atomic.Pointer[servedHTTPConfig]
	stopOnce sync.Once
	admitMu  sync.Mutex
	stopped  bool
	handlers sync.WaitGroup
	done     chan struct{}
	drained  chan struct{}
}

type preparedHTTPServer struct {
	owner    *ownedHTTPServer
	expected *httpServerInstance
	listen   string
	config   *servedHTTPConfig
	listener net.Listener
}

func (o *ownedHTTPServer) prepare(listen string, config *servedHTTPConfig) (*preparedHTTPServer, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil, errServeMutationStopping
	}
	if o.current != nil {
		select {
		case <-o.current.done:
			// A failed Serve loop no longer owns a live listener. Reusing only
			// its address/config pointer would leave the endpoint down forever.
			o.current.stop()
			o.retired = append(o.retired, o.current)
			o.current = nil
		default:
		}
	}
	for _, previous := range o.retired {
		select {
		case <-previous.drained:
		default:
			return nil, errors.New("previous HTTP requests are still draining")
		}
	}
	o.retired = nil
	prepared := &preparedHTTPServer{owner: o, expected: o.current, listen: listen, config: config}
	if config == nil || (o.current != nil && o.current.listen == listen) {
		return prepared, nil
	}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, err
	}
	prepared.listener = listener
	return prepared, nil
}

func (p *preparedHTTPServer) close() {
	if p != nil && p.listener != nil {
		_ = p.listener.Close()
		p.listener = nil
	}
}

func (p *preparedHTTPServer) publish() error {
	o := p.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || o.current != p.expected {
		return errors.New("HTTP listener changed before configuration publication")
	}
	if p.config != nil && p.listener == nil {
		o.current.config.Store(p.config)
		return nil
	}
	previous := o.current
	if p.config == nil {
		o.current = nil
	} else {
		instance := &httpServerInstance{listen: p.listen, listener: p.listener, done: make(chan struct{}), drained: make(chan struct{})}
		instance.config.Store(p.config)
		instance.server = &http.Server{
			Handler:           instance,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       30 * time.Second,
			IdleTimeout:       2 * time.Minute,
		}
		if !p.config.Streaming {
			instance.server.WriteTimeout = time.Minute
		}
		instance.server.SetKeepAlivesEnabled(false)
		o.current = instance
		p.listener = nil // ownership transferred to the active server
		go func() {
			defer close(instance.done)
			_ = instance.server.Serve(currentTLSListener{Listener: instance.listener, instance: instance})
		}()
	}
	if previous != nil {
		previous.stop()
		o.retired = append(o.retired, previous)
	}
	return nil
}

func (s *httpServerInstance) stop() {
	s.stopOnce.Do(func() {
		s.admitMu.Lock()
		s.stopped = true
		s.admitMu.Unlock()
		// Stop admissions synchronously, without waiting for the mutation request
		// that may itself be publishing this replacement to write its response.
		_ = s.listener.Close()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := s.server.Shutdown(ctx); err != nil {
				_ = s.server.Close()
			}
			// Closing connections is not an acknowledgement that an admitted
			// handler relinquished SQLite or its operation. No new handler can
			// enter after stopped=true; the owner joins the existing ones here.
			s.handlers.Wait()
			<-s.done
			close(s.drained)
		}()
	})
}

func (s *serveHTTPServers) stopAdmissions() {
	s.control.stopAdmissions()
	s.web.stopAdmissions()
}

func (o *ownedHTTPServer) stopAdmissions() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	if o.current != nil {
		o.current.stop()
		o.retired = append(o.retired, o.current)
		o.current = nil
	}
}

func (o *ownedHTTPServer) close(ctx context.Context) error {
	o.stopAdmissions()
	o.mu.Lock()
	retired := append([]*httpServerInstance(nil), o.retired...)
	o.mu.Unlock()
	for _, server := range retired {
		select {
		case <-server.drained:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *httpServerInstance) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.admitMu.Lock()
	if s.stopped {
		s.admitMu.Unlock()
		http.Error(w, "listener configuration changed", http.StatusServiceUnavailable)
		return
	}
	s.handlers.Add(1)
	s.admitMu.Unlock()
	defer s.handlers.Done()
	config := s.config.Load()
	if err := verifyCurrentHTTPTransport(r, config.TLS); err != nil {
		http.Error(w, "transport authorization changed", http.StatusUnauthorized)
		return
	}
	config.Handler.ServeHTTP(w, r)
}

// Verify the peer against the current client CA, not the VerifiedChains saved by
// an earlier handshake. A connection accepted before a policy update has no
// authority to admit a new request under the previous trust configuration.
func verifyCurrentHTTPTransport(r *http.Request, config *tls.Config) error {
	if (r.TLS == nil) != (config == nil) {
		return errors.New("HTTP transport mode changed")
	}
	if config == nil || config.ClientAuth != tls.RequireAndVerifyClientCert {
		return nil
	}
	if len(r.TLS.PeerCertificates) == 0 {
		return errors.New("client certificate required")
	}
	intermediates := x509.NewCertPool()
	for _, certificate := range r.TLS.PeerCertificates[1:] {
		intermediates.AddCert(certificate)
	}
	_, err := r.TLS.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: config.ClientCAs, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	return err
}

type currentTLSListener struct {
	net.Listener
	instance *httpServerInstance
}

func (l currentTLSListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if l.instance.config.Load().TLS == nil {
		return conn, nil
	}
	return tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS12, GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
		config := l.instance.config.Load().TLS
		if config == nil {
			return nil, fmt.Errorf("TLS listener was disabled")
		}
		return config, nil
	}}), nil
}
