package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseReadsBudgetUSD(t *testing.T) {
	src := "---\nname: reviewer\ndescription: reviews\ntools: [read_file]\nbudget_usd: 2.5\n---\nYou review code.\n"
	d, err := ParseAgentFile([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if d.BudgetUSD != 2.5 {
		t.Fatalf("BudgetUSD = %v, want 2.5", d.BudgetUSD)
	}
}

func TestBudgetUSDIsOmittedWhenZeroAndRoundTripsWhenSet(t *testing.T) {
	unlimited, err := RenderAgentFile(validDef("reviewer"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(unlimited), "budget_usd") {
		t.Fatalf("a zero budget must not be written:\n%s", unlimited)
	}

	d := validDef("reviewer")
	d.BudgetUSD = 0.75
	raw, err := RenderAgentFile(d)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseAgentFile(raw)
	if err != nil || back.BudgetUSD != 0.75 {
		t.Fatalf("round trip = %v, %v; want 0.75", back.BudgetUSD, err)
	}
}

func TestValidateRefusesANegativeBudget(t *testing.T) {
	d := validDef("reviewer")
	d.BudgetUSD = -1
	if err := Validate(d); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("a negative budget must be refused: %v", err)
	}
}

func TestSaveKeepsBudgetInTheFile(t *testing.T) {
	home := t.TempDir()
	d := validDef("reviewer")
	d.BudgetUSD = 3
	if err := Save(home, d, "You review code."); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "agents", "reviewer.md"))
	if err != nil || !strings.Contains(string(raw), "budget_usd: 3") {
		t.Fatalf("budget_usd not saved: %v\n%s", err, raw)
	}
}
