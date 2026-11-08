// SPDX-License-Identifier: BSD-3-Clause

package daemonapi

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
)

func ReadStatus(ctx context.Context, socketPath string) (DaemonStatus, error) {
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socketPath)
	}}}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1/status", nil)
	if err != nil {
		return DaemonStatus{}, err
	}
	req.Close = true
	resp, err := client.Do(req)
	if err != nil {
		return DaemonStatus{}, err
	}
	defer resp.Body.Close()
	var status DaemonStatus
	return status, json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&status)
}
