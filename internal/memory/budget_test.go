package memory

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestSpendSumsEndedAndLiveSessionsPerAgent(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	now := time.Now().UTC()

	// reader: one ended session and one live session, both persisted per turn.
	if err := s.StartSession(ctx, Session{ID: "r-1", Agent: "reader", StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSessionCost(ctx, "r-1", 0.30); err != nil {
		t.Fatal(err)
	}
	if err := s.EndSession(ctx, "r-1", 0.40); err != nil {
		t.Fatal(err)
	}
	if err := s.StartSession(ctx, Session{ID: "r-2", Agent: "reader", StartedAt: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSessionCost(ctx, "r-2", 0.25); err != nil {
		t.Fatal(err)
	}
	// ops: a session that is still live.
	if err := s.StartSession(ctx, Session{ID: "o-1", Agent: "ops", StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSessionCost(ctx, "o-1", 1.00); err != nil {
		t.Fatal(err)
	}

	got, err := s.SpendUSD(ctx, "reader")
	if err != nil || !approx(got, 0.65) {
		t.Fatalf("reader spend = %v, %v; want 0.65 (0.40 ended + 0.25 live)", got, err)
	}
	by, err := s.SpendByAgent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !approx(by["reader"], 0.65) || !approx(by["ops"], 1.00) || len(by) != 2 {
		t.Fatalf("SpendByAgent = %v", by)
	}
	if got, _ := s.SpendUSD(ctx, "nobody"); got != 0 {
		t.Fatalf("an agent without sessions spent %v", got)
	}
}

func TestSpendSurvivesARestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "groot.db")
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.StartSession(ctx, Session{ID: "r-1", Agent: "reader", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	// No EndSession: the process stops in the middle of the session.
	if err := s.SetSessionCost(ctx, "r-1", 0.42); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	reopened, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, _ := reopened.SpendUSD(ctx, "reader"); !approx(got, 0.42) {
		t.Fatalf("spend after restart = %v, want 0.42", got)
	}
}

func TestLatestSessionReportsItsCost(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if err := s.StartSession(ctx, Session{ID: "r-1", Agent: "reader", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSessionCost(ctx, "r-1", 0.5); err != nil {
		t.Fatal(err)
	}
	sess, ok, err := s.LatestSession(ctx, "reader")
	if err != nil || !ok || !approx(sess.CostUSD, 0.5) {
		t.Fatalf("LatestSession cost = %v (ok %v, err %v), want 0.5", sess.CostUSD, ok, err)
	}
}

func TestNoMemoryHasNoSpend(t *testing.T) {
	ctx := context.Background()
	var m NoMemory
	if err := m.SetSessionCost(ctx, "x", 1); err != nil {
		t.Fatal(err)
	}
	if got, err := m.SpendUSD(ctx, "reader"); got != 0 || err != nil {
		t.Fatalf("SpendUSD = %v, %v", got, err)
	}
	if by, err := m.SpendByAgent(ctx); len(by) != 0 || err != nil {
		t.Fatalf("SpendByAgent = %v, %v", by, err)
	}
}
