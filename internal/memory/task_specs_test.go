package memory

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/tinchoram/groot-cli/internal/flow"
)

func largeTaskWithSpecs(t *testing.T, st *SQLite, agent string) flow.Task {
	t.Helper()
	ctx := context.Background()
	tk, err := st.Tasks().Open(ctx, newTask("payments", agent, base))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().SetSpecs(ctx, tk.ID, []flow.Spec{{N: 1, Text: "split the module"}, {N: 2, Text: "keep the totals"}}); err != nil {
		t.Fatal(err)
	}
	return tk
}

func TestTaskSpecsRoundTripVerdictsAndReset(t *testing.T) {
	ctx := context.Background()
	st := openTempSQLite(t)
	tk := largeTaskWithSpecs(t, st, "developer")
	store := st.Tasks()

	if err := store.SetVerdict(ctx, tk.ID, 1, flow.VerdictPass, "tests ok"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetVerdict(ctx, tk.ID, 2, flow.VerdictFail, "totals off"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []flow.SpecState{
		{Spec: flow.Spec{N: 1, Text: "split the module"}, Status: flow.VerdictPass, Note: "tests ok"},
		{Spec: flow.Spec{N: 2, Text: "keep the totals"}, Status: flow.VerdictFail, Note: "totals off"},
	}
	if len(got.Specs) != 2 || got.Specs[0] != want[0] || got.Specs[1] != want[1] {
		t.Fatalf("Get specs = %+v", got.Specs)
	}
	active, ok, err := store.Active(ctx, "payments", "developer")
	if err != nil || !ok || len(active.Specs) != 2 {
		t.Fatalf("Active specs = %+v, %v, %v", active.Specs, ok, err)
	}

	if err := store.ResetVerdicts(ctx, tk.ID); err != nil {
		t.Fatal(err)
	}
	specs, err := store.Specs(ctx, tk.ID)
	if err != nil || len(specs) != 2 || specs[0].Status != flow.VerdictPending || specs[1].Note != "" {
		t.Fatalf("specs after reset = %+v, %v", specs, err)
	}
	if err := store.SetVerdict(ctx, tk.ID, 9, flow.VerdictPass, "x"); err == nil {
		t.Fatal("a verdict for a spec that does not exist is refused")
	}
}

func TestTaskSpecsTableIsAddedToAnOldDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	st, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	tk, err := st.Tasks().Open(ctx, newTask("payments", "developer", base))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`DROP TABLE task_specs`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st.Close()
	if err := st.Tasks().SetSpecs(ctx, tk.ID, []flow.Spec{{N: 1, Text: "after the migration"}}); err != nil {
		t.Fatalf("specs on a migrated database: %v", err)
	}
	specs, err := st.Tasks().Specs(ctx, tk.ID)
	if err != nil || len(specs) != 1 || specs[0].Text != "after the migration" {
		t.Fatalf("specs = %+v, %v", specs, err)
	}
}

func TestCopyAgentCarriesSpecsToTheNewTaskIDs(t *testing.T) {
	ctx := context.Background()
	src := openTempSQLite(t)
	dst := openTempSQLite(t)
	if _, err := dst.Tasks().Open(ctx, newTask("billing", "other", base)); err != nil {
		t.Fatal(err)
	}
	tk := largeTaskWithSpecs(t, src, "developer")
	if err := src.Tasks().SetVerdict(ctx, tk.ID, 2, flow.VerdictFail, "totals off"); err != nil {
		t.Fatal(err)
	}

	if _, err := src.CopyAgentData(ctx, dst, "developer"); err != nil {
		t.Fatal(err)
	}
	copied, ok, err := dst.Tasks().Active(ctx, "payments", "developer")
	if err != nil || !ok {
		t.Fatalf("copied task = %v, %v", ok, err)
	}
	if copied.ID == tk.ID {
		t.Fatal("the copy must get a new task id in the target")
	}
	if len(copied.Specs) != 2 || copied.Specs[1].Status != flow.VerdictFail || copied.Specs[1].Note != "totals off" {
		t.Fatalf("copied specs = %+v", copied.Specs)
	}
	if original, err := src.Tasks().Get(ctx, tk.ID); err != nil || len(original.Specs) != 2 {
		t.Fatalf("source specs after the copy = %+v, %v", original.Specs, err)
	}
}

func TestRenameAgentKeepsTheSpecsOfItsTasks(t *testing.T) {
	ctx := context.Background()
	st := openTempSQLite(t)
	tk := largeTaskWithSpecs(t, st, "developer")
	if err := st.RenameAgent(ctx, "developer", "builder"); err != nil {
		t.Fatal(err)
	}
	got, err := st.Tasks().Get(ctx, tk.ID)
	if err != nil || got.Agent != "builder" || len(got.Specs) != 2 {
		t.Fatalf("renamed task = %+v, %v", got, err)
	}
}
