// SPDX-License-Identifier: BSD-3-Clause

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

type Publisher interface {
	Publish(context.Context, daemonapi.DaemonEvent) error
}

type DaemonSource struct {
	Daemon    daemonapi.DaemonRef
	Socket    string
	Publisher Publisher
	Wait      time.Duration
	Backoff   time.Duration
	Replay    bool
	// PublishTail emits only the newest event observed while the source fast
	// forwards to a daemon's current cursor.  It is useful for restored daemon
	// state: the controller does not replay stale history, but it immediately
	// refreshes the daemon status and therefore unblocks dependent resources.
	PublishTail bool
}

type EventsResponse struct {
	Cursor string                  `json:"cursor,omitempty"`
	Events []daemonapi.DaemonEvent `json:"events"`
	More   bool                    `json:"more,omitempty"`
}

func (s DaemonSource) Run(ctx context.Context) error {
	if s.Socket == "" {
		return fmt.Errorf("socket is required")
	}
	if s.Publisher == nil {
		return fmt.Errorf("publisher is required")
	}
	wait := s.Wait
	if wait == 0 {
		wait = 10 * time.Second
	}
	backoff := s.Backoff
	if backoff == 0 {
		backoff = time.Second
	}
	client := httpClientForUnixSocket(s.Socket)
	var cursor string
	if !s.Replay {
		for {
			next, tail, err := s.fastForward(ctx, client)
			if err == nil {
				cursor = next
				if s.PublishTail && tail != nil {
					if tail.Daemon.Name == "" {
						tail.Daemon = s.Daemon
					}
					if err := s.Publisher.Publish(ctx, *tail); err != nil {
						return err
					}
				}
				break
			}
			timer := time.NewTimer(backoff)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		next, err := s.poll(ctx, client, cursor, wait, true)
		if err != nil {
			timer := time.NewTimer(backoff)
			select {
			case <-timer.C:
				continue
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		}
		if next != "" {
			cursor = next
		}
	}
}

func (s DaemonSource) fastForward(ctx context.Context, client *http.Client) (string, *daemonapi.DaemonEvent, error) {
	values := url.Values{}
	values.Set("wait", "0s")
	values.Set("tail", "true")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1/events?"+values.Encode(), nil)
	if err != nil {
		return "", nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", nil, fmt.Errorf("daemon events returned HTTP %d", resp.StatusCode)
	}
	var decoded EventsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&decoded); err != nil {
		return "", nil, err
	}
	var tail *daemonapi.DaemonEvent
	if len(decoded.Events) > 0 {
		latest := decoded.Events[len(decoded.Events)-1]
		tail = &latest
	}
	if decoded.Cursor != "" {
		return decoded.Cursor, tail, nil
	}
	next := ""
	for _, event := range decoded.Events {
		if event.Cursor != "" {
			next = event.Cursor
		}
	}
	return next, tail, nil
}

func (s DaemonSource) poll(ctx context.Context, client *http.Client, cursor string, wait time.Duration, publish bool) (string, error) {
	values := url.Values{}
	values.Set("wait", wait.String())
	if cursor != "" {
		values.Set("since", cursor)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1/events?"+values.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("daemon events returned HTTP %d", resp.StatusCode)
	}
	var decoded EventsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&decoded); err != nil {
		return "", err
	}
	next := decoded.Cursor
	for _, event := range decoded.Events {
		if event.Daemon.Name == "" {
			event.Daemon = s.Daemon
		}
		if publish {
			if err := s.Publisher.Publish(ctx, event); err != nil {
				return next, err
			}
		}
		if event.Cursor != "" {
			next = event.Cursor
		}
	}
	return next, nil
}

func httpClientForUnixSocket(socketPath string) *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &http.Client{Transport: transport}
}
