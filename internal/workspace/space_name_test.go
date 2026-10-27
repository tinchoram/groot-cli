package workspace

import (
	"context"
	"testing"
)

// TestFeedbackRecordsTheSpaceNameNotThePath: the report tool writes the name of
// the space, so items read "space: default" and not the directory.
func TestFeedbackRecordsTheSpaceNameNotThePath(t *testing.T) {
	f := newFixture(t, text("ok"))
	reg, err := f.ws.buildTools(f.dir, []string{"feedback_report"}, nil, nil, nil, nil, nil, "reader")
	if err != nil {
		t.Fatal(err)
	}
	tl, ok := reg.Get("feedback_report")
	if !ok {
		t.Fatal("the tool is registered")
	}
	out, isErr := tl.Execute(context.Background(), `{"type":"bug","title":"typo"}`)
	if isErr {
		t.Fatalf("report failed: %s", out)
	}
	items, _ := f.ws.Backlog("")
	if len(items) != 1 || items[0].Space != "default" {
		t.Fatalf("items = %+v", items)
	}
}
