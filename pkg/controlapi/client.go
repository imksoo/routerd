// SPDX-License-Identifier: BSD-3-Clause

package controlapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Client struct {
	httpClient    *http.Client
	baseURL       string
	bearerToken   string
	retryAttempts int
	retryDelay    time.Duration
}

type TLSOptions struct {
	CAFile             string
	CertFile           string
	KeyFile            string
	ServerName         string
	InsecureSkipVerify bool
}

func NewUnixClient(socketPath string) *Client {
	// Issue #40: DisableKeepAlives so every request closes its connection
	// after the response. The routerd control / status http.Server already
	// sets SetKeepAlivesEnabled(false), but a polite client also closes
	// from its end immediately rather than relying on the server-side
	// Connection: close header. Unix-socket dial is cheap; the upside is
	// that internal/polling daemons cannot accumulate fd state in
	// routerd's accept queue over long uptimes.
	transport := &http.Transport{
		DisableKeepAlives: true,
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

func NewHTTPClient(baseURL string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:65432"
	}
	return &Client{
		httpClient:    http.DefaultClient,
		baseURL:       baseURL,
		retryAttempts: 3,
		retryDelay:    300 * time.Millisecond,
	}
}

func NewHTTPClientWithTLS(baseURL string, opts TLSOptions) (*Client, error) {
	client := NewHTTPClient(baseURL)
	tlsConfig, err := clientTLSConfig(opts)
	if err != nil {
		return nil, err
	}
	if tlsConfig == nil {
		return client, nil
	}
	client.httpClient = &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}
	return client, nil
}

func (c *Client) WithBearerToken(token string) *Client {
	if c == nil {
		return nil
	}
	next := *c
	next.bearerToken = strings.TrimSpace(token)
	return &next
}

func clientTLSConfig(opts TLSOptions) (*tls.Config, error) {
	if strings.TrimSpace(opts.CAFile) == "" &&
		strings.TrimSpace(opts.CertFile) == "" &&
		strings.TrimSpace(opts.KeyFile) == "" &&
		strings.TrimSpace(opts.ServerName) == "" &&
		!opts.InsecureSkipVerify {
		return nil, nil
	}
	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         strings.TrimSpace(opts.ServerName),
		InsecureSkipVerify: opts.InsecureSkipVerify,
	}
	if strings.TrimSpace(opts.CAFile) != "" {
		data, err := os.ReadFile(strings.TrimSpace(opts.CAFile))
		if err != nil {
			return nil, fmt.Errorf("read CA file %q: %w", strings.TrimSpace(opts.CAFile), err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("CA file %q contains no PEM certificates", strings.TrimSpace(opts.CAFile))
		}
		cfg.RootCAs = pool
	}
	certFile := strings.TrimSpace(opts.CertFile)
	keyFile := strings.TrimSpace(opts.KeyFile)
	if (certFile == "") != (keyFile == "") {
		return nil, errors.New("client cert file and key file must be set together")
	}
	if certFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load client certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
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

func (c *Client) Runtime(ctx context.Context) (*RuntimeStats, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+Prefix+"/runtime", nil)
	if err != nil {
		return nil, err
	}
	var stats RuntimeStats
	if err := c.do(req, &stats); err != nil {
		return nil, err
	}
	return &stats, nil
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

func (c *Client) Get(ctx context.Context, request GetRequest) (*GetResult, error) {
	values := url.Values{}
	values.Set("subject", request.Subject)
	if request.EventsLimit > 0 {
		values.Set("events-limit", strconv.Itoa(request.EventsLimit))
	}
	if request.Limit > 0 {
		values.Set("limit", strconv.Itoa(request.Limit))
	}
	if request.SinceID > 0 {
		values.Set("since-id", strconv.FormatInt(request.SinceID, 10))
	}
	if request.Topic != "" {
		values.Set("topic", request.Topic)
	}
	if request.Resource != "" {
		values.Set("resource", request.Resource)
	}
	if request.KindFilter != "" {
		values.Set("kind", request.KindFilter)
	}
	if request.NameFilter != "" {
		values.Set("name", request.NameFilter)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+Prefix+"/get?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var result GetResult
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) Describe(ctx context.Context, request DescribeRequest) (*DescribeResult, error) {
	values := url.Values{}
	values.Set("target", request.Target)
	if request.EventsLimit > 0 {
		values.Set("events-limit", strconv.Itoa(request.EventsLimit))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+Prefix+"/describe?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var result DescribeResult
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) Probe(ctx context.Context, request ProbeRequest) (*ProbeResult, error) {
	values := url.Values{}
	values.Set("subject", request.Subject)
	if request.Target != "" {
		values.Set("target", request.Target)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+Prefix+"/probe?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var result ProbeResult
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
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

func (c *Client) Plan(ctx context.Context, request PlanRequest) (*PlanResult, error) {
	request.APIVersion = APIVersion
	request.Kind = "PlanRequest"
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+Prefix+"/plan", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	var result PlanResult
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

func (c *Client) Validate(ctx context.Context, request ValidateRequest) (*ValidateResult, error) {
	request.APIVersion = APIVersion
	request.Kind = "ValidateRequest"
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+Prefix+"/validate", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	var result ValidateResult
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) SubmitSAMEnrollmentClaim(ctx context.Context, request SAMEnrollmentClaimSubmitRequest) (*SAMEnrollmentClaimSubmitResult, error) {
	request.APIVersion = APIVersion
	request.Kind = "SAMEnrollmentClaimSubmitRequest"
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+Prefix+"/sam-enrollment-claims", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	var result SAMEnrollmentClaimSubmitResult
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) GetSAMRRSet(ctx context.Context, request SAMRRSetGetRequest) (*SAMRRSetGetResult, error) {
	values := url.Values{}
	if strings.TrimSpace(request.ClaimRef) != "" {
		values.Set("claim", request.ClaimRef)
	}
	path := c.baseURL + Prefix + "/sam-rrsets/" + url.PathEscape(strings.TrimSpace(request.Name))
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var result SAMRRSetGetResult
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
	path := c.baseURL + Prefix + "/dns-queries?" + dnsQueryValues(query).Encode()
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

func (c *Client) DNSQueriesAggregate(ctx context.Context, query DNSQueriesRequest) (*DNSQueriesAggregate, error) {
	path := c.baseURL + Prefix + "/dns-queries/aggregate?" + dnsQueryValues(query).Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var agg DNSQueriesAggregate
	if err := c.do(req, &agg); err != nil {
		return nil, err
	}
	return &agg, nil
}

func (c *Client) TrafficFlows(ctx context.Context, query TrafficFlowsRequest) (*TrafficFlows, error) {
	path := c.baseURL + Prefix + "/traffic-flows?" + trafficFlowValues(query).Encode()
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

func (c *Client) TrafficFlowsAggregate(ctx context.Context, query TrafficFlowsRequest) (*TrafficFlowsAggregate, error) {
	path := c.baseURL + Prefix + "/traffic-flows/aggregate?" + trafficFlowValues(query).Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var agg TrafficFlowsAggregate
	if err := c.do(req, &agg); err != nil {
		return nil, err
	}
	return &agg, nil
}

func dnsQueryValues(query DNSQueriesRequest) url.Values {
	values := logQueryValues(query.Since, query.Limit, map[string]string{
		"client":       query.Client,
		"qname":        query.QName,
		"qname-suffix": query.QNameSuffix,
		"rcode":        query.ResponseCode,
		"upstream":     query.Upstream,
		"from":         query.From,
		"to":           query.To,
	})
	if query.DurationMinUS > 0 {
		values.Set("duration-min-us", strconv.FormatInt(query.DurationMinUS, 10))
	}
	return values
}

func trafficFlowValues(query TrafficFlowsRequest) url.Values {
	values := logQueryValues(query.Since, query.Limit, map[string]string{
		"client":      query.Client,
		"peer":        query.Peer,
		"peer-suffix": query.PeerSuffix,
		"protocol":    query.Protocol,
		"from":        query.From,
		"to":          query.To,
	})
	if query.Asymmetric {
		values.Set("asymmetric", "1")
	}
	return values
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
		if c.bearerToken != "" {
			nextReq.Header.Set("Authorization", "Bearer "+c.bearerToken)
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
