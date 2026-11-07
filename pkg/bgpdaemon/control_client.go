// SPDX-License-Identifier: BSD-3-Clause

package bgpdaemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"strings"
	"time"

	bgpstate "github.com/imksoo/routerd/pkg/bgp"
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
	return decodePathListResponse(resp, source)
}

func decodePathListResponse(resp *http.Response, source string) ([]AppliedPath, error) {
	var paths []AppliedPath
	if err := decodeControlResponse(resp, &paths); err != nil {
		return nil, err
	}
	if paths == nil {
		return nil, fmt.Errorf("BGP path observation is null")
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if err := ValidateAppliedPath(path); err != nil {
			return nil, err
		}
		if path.UUID == "" {
			return nil, fmt.Errorf("BGP path %s has no acknowledged native UUID", path.Prefix)
		}
		if strings.TrimSpace(source) != "" && path.Source != strings.TrimSpace(source) {
			return nil, fmt.Errorf("BGP path observation returned another source")
		}
		key := AppliedPathKey(path)
		if seen[key] {
			return nil, fmt.Errorf("BGP path observation duplicated %s", key)
		}
		seen[key] = true
	}
	return paths, nil
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
	return decodeObservedPathListResponse(resp)
}

func decodeObservedPathListResponse(resp *http.Response) ([]ObservedPath, error) {
	var paths []ObservedPath
	if err := decodeControlResponse(resp, &paths); err != nil {
		return nil, err
	}
	if paths == nil {
		return nil, fmt.Errorf("live BGP observation is null")
	}
	for _, path := range paths {
		if _, err := netip.ParsePrefix(path.Prefix); err != nil {
			return nil, fmt.Errorf("live BGP prefix observation: %w", err)
		}
		if path.PeerAddress != "" {
			if _, err := netip.ParseAddr(path.PeerAddress); err != nil {
				return nil, fmt.Errorf("live BGP peer observation: %w", err)
			}
		}
		if _, err := bgpstate.StandardCommunities(path.Communities); err != nil {
			return nil, fmt.Errorf("live BGP community observation: %w", err)
		}
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
	return decodePathUpsertResponse(resp, path)
}

func decodePathUpsertResponse(resp *http.Response, path AppliedPath) (AppliedPath, error) {
	var out AppliedPath
	if err := decodeControlResponse(resp, &out); err != nil {
		return AppliedPath{}, err
	}
	if err := ValidateAppliedPath(out); err != nil {
		return AppliedPath{}, err
	}
	if out.UUID == "" || AppliedPathKey(out) != AppliedPathKey(path) || !reflect.DeepEqual(NormalizeAppliedPath(out).Attrs, NormalizeAppliedPath(path).Attrs) {
		return AppliedPath{}, fmt.Errorf("BGP path acknowledgment does not match its requested native effect")
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
	return decodePathWithdrawalResponse(resp, path)
}

func decodePathWithdrawalResponse(resp *http.Response, path AppliedPath) error {
	var applied AppliedConfig
	if err := decodeControlResponse(resp, &applied); err != nil {
		return err
	}
	if err := Validate(applied); err != nil {
		return err
	}
	if applied.Version != AppliedVersion {
		return fmt.Errorf("BGP path withdrawal acknowledgment has unsupported version %d", applied.Version)
	}
	if applied.PendingPolicy != nil {
		return fmt.Errorf("BGP path withdrawal awaits policy recovery")
	}
	key := AppliedPathKey(path)
	for _, group := range [][]AppliedPath{applied.Paths, applied.PendingPathAdds, applied.PendingPathRemovals} {
		for _, owned := range group {
			if AppliedPathKey(owned) == key {
				return fmt.Errorf("BGP path withdrawal is not durably complete")
			}
		}
	}
	return nil
}

func (c *ControlClient) do(req *http.Request) (*http.Response, error) {
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
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
	data, err := io.ReadAll(io.LimitReader(resp.Body, (4<<10)+1))
	if err != nil {
		return err
	}
	if len(data) > 4<<10 {
		data = append(data[:4<<10], []byte(" [truncated]")...)
	}
	return fmt.Errorf("BGP control HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(data))
}

func decodeControlResponse(resp *http.Response, value any) error {
	data, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 4<<20 {
		return fmt.Errorf("BGP control response exceeds 4 MiB")
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("BGP control response is null")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		return fmt.Errorf("BGP control response contains trailing data")
	}
	return nil
}
