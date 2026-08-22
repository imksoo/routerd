// SPDX-License-Identifier: BSD-3-Clause

package bgpdaemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ControlClient struct {
	SocketPath string
}

func NewControlClient(socketPath string) *ControlClient {
	return &ControlClient{SocketPath: strings.TrimSpace(socketPath)}
}

// ObservedPath is a read-only path from the live GoBGP global RIB.  Unlike
// AppliedPath, it is not routerd's locally originated desired-path journal;
// callers use it only when they need to know what this router has actually
// learned and selected from a peer.
type ObservedPath struct {
	Prefix      string   `json:"prefix"`
	PeerAddress string   `json:"peerAddress,omitempty"`
	Best        bool     `json:"best"`
	Valid       bool     `json:"valid"`
	Stale       bool     `json:"stale,omitempty"`
	Communities []string `json:"communities,omitempty"`
}

func (c *ControlClient) ListPaths(ctx context.Context, source string) ([]AppliedPath, error) {
	if c == nil || strings.TrimSpace(c.SocketPath) == "" {
		return nil, errors.New("routerd-bgp control socket is not configured")
	}
	path := "/v1/paths"
	if strings.TrimSpace(source) != "" {
		path += "?source=" + url.QueryEscape(strings.TrimSpace(source))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://routerd-bgp"+path, nil)
	if err != nil {
		return nil, err
	}
	req.Close = true
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, responseError(resp)
	}
	var paths []AppliedPath
	if err := json.NewDecoder(resp.Body).Decode(&paths); err != nil {
		return nil, err
	}
	return Normalize(AppliedConfig{Paths: paths}).Paths, nil
}

// ListObservedPaths reads the live GoBGP RIB.  It deliberately has a
// different method and return type from ListPaths: /v1/paths is the durable
// locally-originated applied-path journal and cannot prove that a remote peer
// has taken over a route.
func (c *ControlClient) ListObservedPaths(ctx context.Context) ([]ObservedPath, error) {
	if c == nil || strings.TrimSpace(c.SocketPath) == "" {
		return nil, errors.New("routerd-bgp control socket is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://routerd-bgp/v1/observed-paths", nil)
	if err != nil {
		return nil, err
	}
	req.Close = true
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, responseError(resp)
	}
	var paths []ObservedPath
	if err := json.NewDecoder(resp.Body).Decode(&paths); err != nil {
		return nil, err
	}
	return paths, nil
}

func (c *ControlClient) UpsertPath(ctx context.Context, path AppliedPath) (AppliedPath, error) {
	if c == nil || strings.TrimSpace(c.SocketPath) == "" {
		return AppliedPath{}, errors.New("routerd-bgp control socket is not configured")
	}
	data, err := json.Marshal(NormalizeAppliedPath(path))
	if err != nil {
		return AppliedPath{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://routerd-bgp/v1/paths", bytes.NewReader(data))
	if err != nil {
		return AppliedPath{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Close = true
	resp, err := c.do(req)
	if err != nil {
		return AppliedPath{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return AppliedPath{}, responseError(resp)
	}
	var out AppliedPath
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return AppliedPath{}, err
	}
	return NormalizeAppliedPath(out), nil
}

func (c *ControlClient) DeletePath(ctx context.Context, path AppliedPath) error {
	if c == nil || strings.TrimSpace(c.SocketPath) == "" {
		return errors.New("routerd-bgp control socket is not configured")
	}
	path = NormalizeAppliedPath(path)
	reqPath := "/v1/paths?source=" + url.QueryEscape(path.Source) + "&prefix=" + url.QueryEscape(path.Prefix)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, "http://routerd-bgp"+reqPath, nil)
	if err != nil {
		return err
	}
	req.Close = true
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return responseError(resp)
	}
	return nil
}

func (c *ControlClient) do(req *http.Request) (*http.Response, error) {
	client := &http.Client{Transport: &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 3 * time.Second}
			return dialer.DialContext(ctx, "unix", c.SocketPath)
		},
	}}
	defer client.CloseIdleConnections()
	return client.Do(req)
}

func responseError(resp *http.Response) error {
	data, _ := io.ReadAll(resp.Body)
	return errors.New(string(bytes.TrimSpace(data)))
}
