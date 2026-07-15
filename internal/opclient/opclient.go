// Package opclient is the operator-side API client used by the armada CLI. It
// speaks the same REST API the agents' control plane exposes, authenticating as
// an operator (bearer operator token + X-Tenant-ID).
package opclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/SbxTheDead/armada/internal/domain"
)

// Client is a thin, dependency-free HTTP client for the operator API.
type Client struct {
	baseURL  string
	token    string
	tenantID string
	http     *http.Client
}

// Config configures the operator client. TenantID and BaseURL are required;
// Token is optional if the server runs without ARMADA_OPERATOR_TOKEN.
type Config struct {
	BaseURL  string
	Token    string
	TenantID string
	Timeout  time.Duration
}

// New builds an operator client.
func New(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{
		baseURL:  cfg.BaseURL,
		token:    cfg.Token,
		tenantID: cfg.TenantID,
		http:     &http.Client{Timeout: timeout},
	}
}

// ListFilter narrows a systems query.
type ListFilter struct {
	Project     string
	Region      string
	Environment string
	Provider    string
	Tag         string
	Lifecycle   string
	Health      string
	Limit       int
}

// ListSystems returns systems matching the filter.
func (c *Client) ListSystems(ctx context.Context, f ListFilter) ([]domain.System, error) {
	q := url.Values{}
	set := func(k, v string) {
		if v != "" {
			q.Set(k, v)
		}
	}
	set("project", f.Project)
	set("region", f.Region)
	set("environment", f.Environment)
	set("provider", f.Provider)
	set("tag", f.Tag)
	set("lifecycle", f.Lifecycle)
	set("health", f.Health)
	if f.Limit > 0 {
		q.Set("limit", strconv.Itoa(f.Limit))
	}
	path := "/api/v1/systems"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var resp struct {
		Systems []domain.System `json:"systems"`
		Count   int             `json:"count"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Systems, nil
}

// GetSystem fetches a single system by ID.
func (c *Client) GetSystem(ctx context.Context, id string) (*domain.System, error) {
	var sys domain.System
	if err := c.do(ctx, http.MethodGet, "/api/v1/systems/"+url.PathEscape(id), nil, &sys); err != nil {
		return nil, err
	}
	return &sys, nil
}

// GetInventory fetches the latest inventory snapshot for a system.
func (c *Client) GetInventory(ctx context.Context, id string) (*domain.Inventory, error) {
	var inv domain.Inventory
	if err := c.do(ctx, http.MethodGet, "/api/v1/systems/"+url.PathEscape(id)+"/inventory", nil, &inv); err != nil {
		return nil, err
	}
	return &inv, nil
}

// GetMetrics fetches the latest heartbeat/metrics for a system.
func (c *Client) GetMetrics(ctx context.Context, id string) (*domain.Heartbeat, error) {
	var hb domain.Heartbeat
	if err := c.do(ctx, http.MethodGet, "/api/v1/systems/"+url.PathEscape(id)+"/metrics", nil, &hb); err != nil {
		return nil, err
	}
	return &hb, nil
}

// --- Join tokens ---

// JoinTokenInput describes a reusable join key to create.
type JoinTokenInput struct {
	Name        string   `json:"name,omitempty"`
	Project     string   `json:"project,omitempty"`
	Region      string   `json:"region,omitempty"`
	Environment string   `json:"environment,omitempty"`
	Provider    string   `json:"provider,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Approval    string   `json:"approval,omitempty"`
	MaxUses     int      `json:"max_uses,omitempty"`
	TTLSeconds  int      `json:"ttl_seconds,omitempty"`
}

// JoinTokenResult is the created key (plaintext shown once) plus its details.
type JoinTokenResult struct {
	ID      string            `json:"id"`
	Token   string            `json:"token"`
	Details *domain.JoinToken `json:"details"`
}

// CreateJoinToken mints a reusable join key.
func (c *Client) CreateJoinToken(ctx context.Context, in JoinTokenInput) (*JoinTokenResult, error) {
	var res JoinTokenResult
	if err := c.do(ctx, http.MethodPost, "/api/v1/join-tokens", in, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// ListJoinTokens returns the tenant's join keys.
func (c *Client) ListJoinTokens(ctx context.Context) ([]domain.JoinToken, error) {
	var resp struct {
		JoinTokens []domain.JoinToken `json:"join_tokens"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/join-tokens", nil, &resp); err != nil {
		return nil, err
	}
	return resp.JoinTokens, nil
}

// RevokeJoinToken permanently disables a join key.
func (c *Client) RevokeJoinToken(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/join-tokens/"+url.PathEscape(id), nil, nil)
}

// ApproveSystem activates a device that joined under a manual-approval key.
func (c *Client) ApproveSystem(ctx context.Context, id string) (*domain.System, error) {
	var sys domain.System
	if err := c.do(ctx, http.MethodPost, "/api/v1/systems/"+url.PathEscape(id)+"/approve", nil, &sys); err != nil {
		return nil, err
	}
	return &sys, nil
}

// do performs a JSON request with operator auth and decodes the response.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var reader io.Reader
	if in != nil {
		buf, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", c.tenantID)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return parseError(method, path, resp)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// parseError turns a non-2xx response into a readable error, extracting the
// server's {"error": "..."} body when present.
func parseError(method, path string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return fmt.Errorf("%s %s: %s (%d)", method, path, e.Error, resp.StatusCode)
	}
	return fmt.Errorf("%s %s: server returned %d", method, path, resp.StatusCode)
}
