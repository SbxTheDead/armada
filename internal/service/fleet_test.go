package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/SbxTheDead/armada/internal/domain"
	"github.com/SbxTheDead/armada/internal/service"
	"github.com/SbxTheDead/armada/internal/store"
	"github.com/SbxTheDead/armada/internal/store/memory"
)

// fixedClock returns a controllable time source for deterministic tests.
type fixedClock struct{ t time.Time }

func (c *fixedClock) now() time.Time { return c.t }

func newFleet(t *testing.T, clk *fixedClock) (*service.Fleet, *memory.DB) {
	t.Helper()
	db := memory.New()
	seq := 0
	f := service.NewFleet(db.Systems, db.Tokens, db.JoinTokens, db.Identities, db.Telemetry, service.Options{
		Now: clk.now,
		IDGen: func() string {
			seq++
			return "id-" + string(rune('a'+seq))
		},
		HeartbeatInterval: time.Minute,
	})
	return f, db
}

func TestRegisterSystem_Validation(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0).UTC()}
	f, _ := newFleet(t, clk)

	_, err := f.RegisterSystem(context.Background(), domain.NewSystemInput{Name: "x"})
	if err == nil {
		t.Fatal("expected validation error for missing tenant/fqdn")
	}
}

func TestRegisterSystem_DuplicateFQDN(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0).UTC()}
	f, _ := newFleet(t, clk)
	in := domain.NewSystemInput{TenantID: "t1", Name: "web-1", FQDN: "web1.example.com"}

	if _, err := f.RegisterSystem(context.Background(), in); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if _, err := f.RegisterSystem(context.Background(), in); err == nil {
		t.Fatal("expected conflict on duplicate FQDN")
	}
}

func TestEnrollAndHeartbeatFlow(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0).UTC()}
	f, _ := newFleet(t, clk)
	ctx := context.Background()

	sys, err := f.RegisterSystem(ctx, domain.NewSystemInput{
		TenantID: "t1", Name: "web-1", FQDN: "web1.example.com",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if sys.Lifecycle != domain.LifecyclePending {
		t.Fatalf("want pending, got %s", sys.Lifecycle)
	}

	// Issue a token bound to the system, then enroll.
	plaintext, _, err := f.IssueEnrollmentToken(ctx, "t1", sys.ID, time.Minute)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	res, err := f.Enroll(ctx, plaintext, "")
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if res.APIKey == "" {
		t.Fatal("expected an API key from enrollment")
	}
	if res.System.Lifecycle != domain.LifecycleEnrolled {
		t.Fatalf("want enrolled, got %s", res.System.Lifecycle)
	}

	// Token is single-use.
	if _, err := f.Enroll(ctx, plaintext, ""); err == nil {
		t.Fatal("expected consumed token to be rejected")
	}

	// Agent authenticates with its key.
	id, err := f.AuthenticateAgent(ctx, res.APIKey)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if id.SystemID != sys.ID {
		t.Fatalf("identity bound to wrong system: %s", id.SystemID)
	}

	// Heartbeat flips health to healthy.
	err = f.RecordHeartbeat(ctx, "t1", &domain.Heartbeat{SystemID: sys.ID, AgentVersion: "1.0.0"})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	got, err := f.GetSystem(ctx, "t1", sys.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Health != domain.HealthHealthy {
		t.Fatalf("want healthy, got %s", got.Health)
	}
	if got.AgentVersion != "1.0.0" {
		t.Fatalf("agent version not recorded: %q", got.AgentVersion)
	}

	// Advance time past 3 intervals: system reads offline at query time.
	clk.t = clk.t.Add(4 * time.Minute)
	got, _ = f.GetSystem(ctx, "t1", sys.ID)
	if got.Health != domain.HealthOffline {
		t.Fatalf("want offline after missed heartbeats, got %s", got.Health)
	}
}

func TestEnroll_ExpiredToken(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0).UTC()}
	f, _ := newFleet(t, clk)
	ctx := context.Background()

	sys, _ := f.RegisterSystem(ctx, domain.NewSystemInput{TenantID: "t1", Name: "n", FQDN: "n.example.com"})
	plaintext, _, _ := f.IssueEnrollmentToken(ctx, "t1", sys.ID, time.Minute)

	clk.t = clk.t.Add(2 * time.Minute) // past expiry
	if _, err := f.Enroll(ctx, plaintext, ""); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestListSystems_Filter(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0).UTC()}
	f, _ := newFleet(t, clk)
	ctx := context.Background()

	_, _ = f.RegisterSystem(ctx, domain.NewSystemInput{TenantID: "t1", Name: "a", FQDN: "a.ex", Region: "eu"})
	_, _ = f.RegisterSystem(ctx, domain.NewSystemInput{TenantID: "t1", Name: "b", FQDN: "b.ex", Region: "us"})

	got, err := f.ListSystems(ctx, "t1", store.SystemFilter{Region: "eu"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("region filter failed: %+v", got)
	}
}
