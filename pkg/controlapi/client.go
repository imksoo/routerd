// SPDX-License-Identifier: BSD-3-Clause

package controlapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"syscall"
	"time"
)

type Client struct {
	httpClient    *http.Client
	baseURL       string
	retryAttempts int
	retryDelay    time.Duration
}

func NewUnixClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		httpClient:    &http.Client{Transport: transport},
		baseURL:       "http://routerd",
		retryAttempts: 10,
		retryDelay:    300 * time.Millisecond,
	}
}

func (c *Client) Status(ctx context.Context) (*Status, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+Prefix+"/status", nil)
	if err != nil {
		return nil, err
	}
	var status Status
	if err := c.do(req, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

func (c *Client) Controllers(ctx context.Context) (*Controllers, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+Prefix+"/controllers", nil)
	if err != nil {
		return nil, err
	}
	var controllers Controllers
	if err := c.do(req, &controllers); err != nil {
		return nil, err
	}
	return &controllers, nil
}

func (c *Client) Apply(ctx context.Context, request ApplyRequest) (*ApplyResult, error) {
	request.APIVersion = APIVersion
	request.Kind = "ApplyRequest"
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+Prefix+"/apply", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	var result ApplyResult
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) Delete(ctx context.Context, request DeleteRequest) (*DeleteResult, error) {
	request.APIVersion = APIVersion
	request.Kind = "DeleteRequest"
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+Prefix+"/delete", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	var result DeleteResult
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) SetLogLevel(ctx context.Context, request LogLevelRequest) (*LogLevelResult, error) {
	request.APIVersion = APIVersion
	request.Kind = "LogLevelRequest"
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+Prefix+"/log-level", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	var result LogLevelResult
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) Connections(ctx context.Context, limit int) (*ConnectionTable, error) {
	path := c.baseURL + Prefix + "/connections"
	if limit >= 0 {
		values := url.Values{}
		values.Set("limit", strconv.Itoa(limit))
		path += "?" + values.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var table ConnectionTable
	if err := c.do(req, &table); err != nil {
		return nil, err
	}
	return &table, nil
}

func (c *Client) DNSQueries(ctx context.Context, query DNSQueriesRequest) (*DNSQueries, error) {
	path := c.baseURL + Prefix + "/dns-queries?" + logQueryValues(query.Since, query.Limit, map[string]string{
		"client": query.Client,
		"qname":  query.QName,
	}).Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var rows DNSQueries
	if err := c.do(req, &rows); err != nil {
		return nil, err
	}
	return &rows, nil
}

func (c *Client) TrafficFlows(ctx context.Context, query TrafficFlowsRequest) (*TrafficFlows, error) {
	path := c.baseURL + Prefix + "/traffic-flows?" + logQueryValues(query.Since, query.Limit, map[string]string{
		"client": query.Client,
		"peer":   query.Peer,
	}).Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var rows TrafficFlows
	if err := c.do(req, &rows); err != nil {
		return nil, err
	}
	return &rows, nil
}

func (c *Client) FirewallLogs(ctx context.Context, query FirewallLogsRequest) (*FirewallLogs, error) {
	path := c.baseURL + Prefix + "/firewall-logs?" + logQueryValues(query.Since, query.Limit, map[string]string{
		"action": query.Action,
		"src":    query.Src,
	}).Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var rows FirewallLogs
	if err := c.do(req, &rows); err != nil {
		return nil, err
	}
	return &rows, nil
}

func logQueryValues(since string, limit int, filters map[string]string) url.Values {
	values := url.Values{}
	if since != "" {
		values.Set("since", since)
	}
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	for key, value := range filters {
		if value != "" {
			values.Set(key, value)
		}
	}
	return values
}

func (c *Client) do(req *http.Request, value any) error {
	resp, err := c.doWithRetry(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr Error
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&apiErr); err == nil && apiErr.Error.Message != "" {
			return fmt.Errorf("%s", apiErr.Error.Message)
		}
		return fmt.Errorf("routerd API returned HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(value)
}

func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	attempts := c.retryAttempts
	if attempts < 1 {
		attempts = 1
	}
	if !isRetryableControlMethod(req.Method) {
		attempts = 1
	}
	delay := c.retryDelay
	if delay <= 0 {
		delay = 300 * time.Millisecond
	}
	for attempt := 0; attempt < attempts; attempt++ {
		nextReq, err := cloneRequestForAttempt(req, attempt)
		if err != nil {
			return nil, err
		}
		resp, err := c.httpClient.Do(nextReq)
		if err == nil || !isTransientControlConnectError(err) || attempt == attempts-1 {
			return resp, err
		}
		timer := time.NewTimer(delay)
		select {
		case <-req.Context().Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, req.Context().Err()
		case <-timer.C:
		}
	}
	return nil, nil
}

func cloneRequestForAttempt(req *http.Request, attempt int) (*http.Request, error) {
	if attempt == 0 {
		return req, nil
	}
	next := req.Clone(req.Context())
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		next.Body = body
		return next, nil
	}
	if req.Body != nil {
		return nil, errors.New("cannot retry request without reusable body")
	}
	return next, nil
}

func isTransientControlConnectError(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.ENOTSOCK) ||
		errors.Is(err, syscall.ECONNRESET)
}

func isRetryableControlMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}
