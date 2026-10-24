package tool

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/feedback"
)

func TestFeedbackToolsReportListUpdate(t *testing.T) {
	ctx := context.Background()
	store, err := feedback.Open(filepath.Join(t.TempDir(), "feedback"))
	if err != nil {
		t.Fatal(err)
	}
	report := NewFeedbackReport(store, "payments", "debug", "0.3.0-dev")
	out, isErr := report.Execute(ctx, `{"type":"bug","title":"typo in help","body":"steps"}`)
	if isErr || out != "reported BUG-001 (open)" {
		t.Fatalf("report = %q (err %v)", out, isErr)
	}
	if out, isErr := report.Execute(ctx, `{"type":"nope","title":"x"}`); !isErr {
		t.Fatalf("an unknown type must be refused: %q", out)
	}
	list, isErr := NewFeedbackList(store).Execute(ctx, `{"status":"open"}`)
	if isErr || !strings.Contains(list, "BUG-001 open — typo in help") {
		t.Fatalf("list = %q", list)
	}
	out, isErr = NewFeedbackUpdate(store).Execute(ctx, `{"id":"BUG-001","status":"done","note":"fixed"}`)
	if isErr || out != "BUG-001 is now done" {
		t.Fatalf("update = %q (err %v)", out, isErr)
	}
	if empty, _ := NewFeedbackList(store).Execute(ctx, `{"status":"open"}`); empty != "no items" {
		t.Fatalf("after done nothing is open: %q", empty)
	}
}

func TestFeedbackToolRisks(t *testing.T) {
	store, _ := feedback.Open(t.TempDir())
	if NewFeedbackReport(store, "", "", "").Risk() != RiskWrite || NewFeedbackUpdate(store).Risk() != RiskWrite {
		t.Fatal("reporting and updating are writes")
	}
	if NewFeedbackList(store).Risk() != RiskRead {
		t.Fatal("listing is a read")
	}
}
