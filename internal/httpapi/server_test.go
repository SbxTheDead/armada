package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SbxTheDead/armada/internal/httpapi"
	"github.com/SbxTheDead/armada/internal/service"
	"github.com/SbxTheDead/armada/internal/store/memory"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	db := memory.New()
	fleet := service.NewFleet(db.Systems, db.Tokens, db.JoinTokens, db.Identities, db.Telemetry, service.Options{
		HeartbeatInterval: time.Minute,
	})
	srv := httpapi.New(httpapi.Config{Fleet: fleet})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// doJSON is a tiny helper for issuing JSON requests in tests.
func doJSON(t *testing.T, method, url string, body any, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, data
}

func TestHealthz(t *testing.T) {
	ts := newTestServer(t)
	resp, _ := doJSON(t, http.MethodGet, ts.URL+"/healthz", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d", resp.StatusCode)
	}
}

func TestOperator_RequiresTenant(t *testing.T) {
	ts := newTestServer(t)
	resp, _ := doJSON(t, http.MethodGet, ts.URL+"/api/v1/systems", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 without tenant, got %d", resp.StatusCode)
	}
}

func TestFullEnrollmentFlowOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	tenant := map[string]string{"X-Tenant-ID": "acme"}

	// Operator registers a system.
	resp, data := doJSON(t, http.MethodPost, ts.URL+"/api/v1/systems", map[string]any{
		"name": "edge-1", "fqdn": "edge1.acme.internal", "region": "eu-west",
	}, tenant)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create system = %d: %s", resp.StatusCode, data)
	}
	var sys struct {
		ID string `json:"id"`
	}
	mustJSON(t, data, &sys)

	// Operator issues an enrollment token.
	resp, data = doJSON(t, http.MethodPost, ts.URL+"/api/v1/systems/"+sys.ID+"/enroll-token", map[string]any{
		"ttl_seconds": 300,
	}, tenant)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("issue token = %d: %s", resp.StatusCode, data)
	}
	var tok struct {
		Token string `json:"token"`
	}
	mustJSON(t, data, &tok)

	// Agent enrolls (no auth, token in body).
	resp, data = doJSON(t, http.MethodPost, ts.URL+"/agent/v1/enroll", map[string]any{
		"token": tok.Token,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enroll = %d: %s", resp.StatusCode, data)
	}
	var enr struct {
		APIKey string `json:"api_key"`
	}
	mustJSON(t, data, &enr)
	if enr.APIKey == "" {
		t.Fatal("no api key returned")
	}

	// Agent sends a heartbeat with its bearer key.
	resp, data = doJSON(t, http.MethodPost, ts.URL+"/agent/v1/heartbeat", map[string]any{
		"agent_version": "1.2.3",
	}, map[string]string{"Authorization": "Bearer " + enr.APIKey})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("heartbeat = %d: %s", resp.StatusCode, data)
	}

	// Heartbeat with a bogus key is rejected.
	resp, _ = doJSON(t, http.MethodPost, ts.URL+"/agent/v1/heartbeat", map[string]any{}, map[string]string{
		"Authorization": "Bearer not-a-real-key",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 for bad key, got %d", resp.StatusCode)
	}

	// Operator sees the system as healthy.
	resp, data = doJSON(t, http.MethodGet, ts.URL+"/api/v1/systems/"+sys.ID, nil, tenant)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get system = %d: %s", resp.StatusCode, data)
	}
	var got struct {
		Health       string `json:"health"`
		AgentVersion string `json:"agent_version"`
	}
	mustJSON(t, data, &got)
	if got.Health != "healthy" {
		t.Fatalf("want healthy, got %q", got.Health)
	}
	if got.AgentVersion != "1.2.3" {
		t.Fatalf("agent version = %q", got.AgentVersion)
	}
}

func TestJoinFlowOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	tenant := map[string]string{"X-Tenant-ID": "acme"}

	// Operator creates a reusable join key.
	resp, data := doJSON(t, http.MethodPost, ts.URL+"/api/v1/join-tokens", map[string]any{
		"name": "fleet", "region": "eu", "tags": []string{"iot"},
	}, tenant)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create join token = %d: %s", resp.StatusCode, data)
	}
	var jt struct {
		Token string `json:"token"`
	}
	mustJSON(t, data, &jt)
	if jt.Token == "" {
		t.Fatal("no join token returned")
	}

	// Device joins with facts (no prior registration).
	resp, data = doJSON(t, http.MethodPost, ts.URL+"/agent/v1/join", map[string]any{
		"join_token": jt.Token, "machine_id": "m-http-1", "hostname": "node-1",
		"fqdn": "node1.acme.internal", "os": "linux", "arch": "arm64",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("join = %d: %s", resp.StatusCode, data)
	}
	var enr struct {
		SystemID string `json:"system_id"`
		APIKey   string `json:"api_key"`
	}
	mustJSON(t, data, &enr)
	if enr.APIKey == "" || enr.SystemID == "" {
		t.Fatalf("join did not return credentials: %s", data)
	}

	// The auto-registered system carries the key's presets.
	resp, data = doJSON(t, http.MethodGet, ts.URL+"/api/v1/systems/"+enr.SystemID, nil, tenant)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get system = %d: %s", resp.StatusCode, data)
	}
	var sys struct {
		Region    string `json:"region"`
		Lifecycle string `json:"lifecycle"`
	}
	mustJSON(t, data, &sys)
	if sys.Region != "eu" || sys.Lifecycle != "enrolled" {
		t.Fatalf("presets/lifecycle wrong: %s", data)
	}

	// A bogus join key is cleanly rejected with 401 (not 500).
	resp, _ = doJSON(t, http.MethodPost, ts.URL+"/agent/v1/join", map[string]any{
		"join_token": "not-a-real-key", "machine_id": "m-http-2",
	}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 for bogus join key, got %d", resp.StatusCode)
	}
}

func mustJSON(t *testing.T, data []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
}
