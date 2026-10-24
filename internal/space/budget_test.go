package space

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpaceBudgetRoundTripsAndIsOmittedWhenZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spaces.yaml")
	dir := t.TempDir()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Add("payments", dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBudget("payments", 2); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "budget_usd: 2") {
		t.Fatalf("budget_usd not written:\n%s", raw)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if sp, _ := reopened.Get("payments"); sp.BudgetUSD != 2 {
		t.Fatalf("BudgetUSD = %v after reopen, want 2", sp.BudgetUSD)
	}

	if err := reopened.SetBudget("payments", 0); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	if strings.Contains(string(raw), "budget_usd") {
		t.Fatalf("a zero budget must be omitted:\n%s", raw)
	}
	if err := reopened.SetBudget("missing", 1); err == nil {
		t.Fatal("an unknown space must be refused")
	}
}
