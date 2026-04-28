package controlapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
)

type Client struct {
	httpClient *http.Client
	baseURL    string
}

func NewUnixClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		httpClient: &http.Client{Transport: transport},
		baseURL:    "http://routerd",
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

func (c *Client) NAPT(ctx context.Context, limit int) (*NAPTTable, error) {
	path := c.baseURL + Prefix + "/napt"
	if limit >= 0 {
		values := url.Values{}
		values.Set("limit", strconv.Itoa(limit))
		path += "?" + values.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var table NAPTTable
	if err := c.do(req, &table); err != nil {
		return nil, err
	}
	return &table, nil
}

func (c *Client) do(req *http.Request, value any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr Error
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err == nil && apiErr.Error.Message != "" {
			return fmt.Errorf("%s", apiErr.Error.Message)
		}
		return fmt.Errorf("routerd API returned HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(value)
}
