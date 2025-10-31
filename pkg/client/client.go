package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	baseURL     string
	leaderURL   string // Cached leader URL for writes
	httpClient  *http.Client
	maxRedirects int
}

type Config struct {
	BaseURL      string
	Timeout      time.Duration
	MaxRedirects int // Max redirects to follow (default: 3)
}

type ListResponse struct {
	Keys   []string `json:"keys"`
	Count  int      `json:"count"`
	Prefix string   `json:"prefix"`
	Limit  int      `json:"limit"`
}

type StatsResponse struct {
	Gets     int64 `json:"gets"`
	Puts     int64 `json:"puts"`
	Deletes  int64 `json:"deletes"`
	KeyCount int64 `json:"key_count"`
}

type HealthResponse struct {
	Status string `json:"status"`
}

type SnapshotResponse struct {
	Snapshot string `json:"snapshot"`
	Message  string `json:"message"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func NewClient(config Config) *Client {
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if config.MaxRedirects == 0 {
		config.MaxRedirects = 3
	}

	return &Client{
		baseURL:      config.BaseURL,
		leaderURL:    config.BaseURL, // Initially assume baseURL is leader
		maxRedirects: config.MaxRedirects,
		httpClient: &http.Client{
			Timeout: config.Timeout,
			// Disable automatic redirects - we handle them manually
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (c *Client) Get(ctx context.Context, key string) ([]byte, error) {
	url := fmt.Sprintf("%s/keys/%s", c.baseURL, url.PathEscape(key))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("key not found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	value, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return value, nil
}

func (c *Client) Put(ctx context.Context, key string, value []byte) error {
	// Try leader first if we know it
	targetURL := c.leaderURL

	for attempt := 0; attempt < c.maxRedirects; attempt++ {
		url := fmt.Sprintf("%s/keys/%s", targetURL, url.PathEscape(key))

		req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(value))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		// Handle redirect to leader
		if resp.StatusCode == http.StatusTemporaryRedirect {
			leaderAddr := resp.Header.Get("X-Raft-Leader")
			if leaderAddr != "" {
				// Cache leader for future requests
				c.leaderURL = "http://" + leaderAddr
				targetURL = c.leaderURL
				continue // Retry with leader
			}
			return fmt.Errorf("redirect without leader address")
		}

		if resp.StatusCode != http.StatusCreated {
			return c.parseError(resp)
		}

		return nil
	}

	return fmt.Errorf("max redirects exceeded (%d)", c.maxRedirects)
}

func (c *Client) Delete(ctx context.Context, key string) error {
	// Try leader first if we know it
	targetURL := c.leaderURL

	for attempt := 0; attempt < c.maxRedirects; attempt++ {
		url := fmt.Sprintf("%s/keys/%s", targetURL, url.PathEscape(key))

		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		// Handle redirect to leader
		if resp.StatusCode == http.StatusTemporaryRedirect {
			leaderAddr := resp.Header.Get("X-Raft-Leader")
			if leaderAddr != "" {
				// Cache leader for future requests
				c.leaderURL = "http://" + leaderAddr
				targetURL = c.leaderURL
				continue // Retry with leader
			}
			return fmt.Errorf("redirect without leader address")
		}

		if resp.StatusCode != http.StatusNoContent {
			return c.parseError(resp)
		}

		return nil
	}

	return fmt.Errorf("max redirects exceeded (%d)", c.maxRedirects)
}

func (c *Client) List(ctx context.Context, prefix string, limit int) (*ListResponse, error) {
	urlStr := fmt.Sprintf("%s/keys", c.baseURL)

	params := url.Values{}
	if prefix != "" {
		params.Set("prefix", prefix)
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}

	if len(params) > 0 {
		urlStr = fmt.Sprintf("%s?%s", urlStr, params.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var listResp ListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &listResp, nil
}

func (c *Client) Stats(ctx context.Context) (*StatsResponse, error) {
	url := fmt.Sprintf("%s/stats", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var statsResp StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&statsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &statsResp, nil
}

func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	url := fmt.Sprintf("%s/health", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var healthResp HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &healthResp, nil
}

func (c *Client) Ready(ctx context.Context) (*HealthResponse, error) {
	url := fmt.Sprintf("%s/ready", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var readyResp HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&readyResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &readyResp, nil
}

func (c *Client) Snapshot(ctx context.Context) (*SnapshotResponse, error) {
	url := fmt.Sprintf("%s/admin/snapshot", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var snapshotResp SnapshotResponse
	if err := json.NewDecoder(resp.Body).Decode(&snapshotResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &snapshotResp, nil
}

// GetLeaderURL returns the cached leader URL
func (c *Client) GetLeaderURL() string {
	return c.leaderURL
}

// SetLeaderURL manually sets the leader URL (useful for testing)
func (c *Client) SetLeaderURL(leaderURL string) {
	c.leaderURL = leaderURL
}

// ResetLeaderCache resets the cached leader to the base URL
func (c *Client) ResetLeaderCache() {
	c.leaderURL = c.baseURL
}

func (c *Client) parseError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("HTTP %d: failed to read error response", resp.StatusCode)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(body, &errResp); err != nil {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, errResp.Error)
}
