package flow

import (
	"strings"
	"testing"
)

const validFeatureDoc = `# Billing split

## Specs
S1. split the billing module in two
S2. invoices keep their totals

## Tasks
T1. (S1) move the invoice code into payments/billing
T2. (S1, S2) add the totals check to the move

## Log
L1. "split the billing module in two, keep the totals"
`

func TestParseFeatureDocReadsSpecsTasksAndLog(t *testing.T) {
	doc, err := ParseFeatureDoc(validFeatureDoc)
	if err != nil {
		t.Fatalf("ParseFeatureDoc: %v", err)
	}
	if len(doc.Specs) != 2 || doc.Specs[1].N != 2 || doc.Specs[1].Text != "invoices keep their totals" {
		t.Fatalf("specs = %+v", doc.Specs)
	}
	if len(doc.Tasks) != 2 || len(doc.Tasks[1].Specs) != 2 || doc.Tasks[1].Specs[1] != 2 {
		t.Fatalf("tasks = %+v", doc.Tasks)
	}
	if doc.Tasks[0].Text != "move the invoice code into payments/billing" {
		t.Fatalf("task text = %q", doc.Tasks[0].Text)
	}
	if len(doc.Log) != 1 || doc.Log[0].N != 1 {
		t.Fatalf("log = %+v", doc.Log)
	}
}

func TestParseFeatureDocRefusesInvalidDocuments(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantErr string
	}{
		{"no specs section", "## Tasks\nT1. (S1) x\n## Log\nL1. r\n", "at least one spec"},
		{"no tasks section", "## Specs\nS1. a\n## Log\nL1. r\n", "at least one task"},
		{"no log entry one", "## Specs\nS1. a\n## Tasks\nT1. (S1) x\n## Log\nL2. r\n", "L1"},
		{"no log section", "## Specs\nS1. a\n## Tasks\nT1. (S1) x\n", "L1"},
		{"spec numbering skips", "## Specs\nS1. a\nS3. c\n## Tasks\nT1. (S1) x\n## Log\nL1. r\n", "S3 is out of order, expected S2"},
		{"spec numbering starts late", "## Specs\nS2. a\n## Tasks\nT1. (S2) x\n## Log\nL1. r\n", "S2 is out of order, expected S1"},
		{"task numbering skips", "## Specs\nS1. a\n## Tasks\nT1. (S1) x\nT3. (S1) y\n## Log\nL1. r\n", "T3 is out of order, expected T2"},
		{"log numbering skips", "## Specs\nS1. a\n## Tasks\nT1. (S1) x\n## Log\nL1. r\nL3. s\n", "L3 is out of order, expected L2"},
		{"unknown spec reference", "## Specs\nS1. a\n## Tasks\nT1. (S1, S2) x\n## Log\nL1. r\n", "S2, which does not exist"},
		{"task without a spec reference", "## Specs\nS1. a\n## Tasks\nT1. x\n## Log\nL1. r\n", "T1 must name the specs it serves"},
		{"empty spec text", "## Specs\nS1.\n## Tasks\nT1. (S1) x\n## Log\nL1. r\n", "S1 has no text"},
		{"duplicate section", "## Specs\nS1. a\n## Specs\nS2. b\n## Tasks\nT1. (S1) x\n## Log\nL1. r\n", "appears twice"},
		{"task item under specs", "## Specs\nS1. a\nT1. (S1) x\n## Tasks\nT1. (S1) x\n## Log\nL1. r\n", "T1. is in ## Specs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseFeatureDoc(tt.src)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want mention of %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseFeatureDocErrorsCarryTheTemplate(t *testing.T) {
	_, err := ParseFeatureDoc("# nothing here\n")
	if err == nil {
		t.Fatal("an empty document is refused")
	}
	for _, want := range []string{
		"## Specs\nS1. \"<what the user asked, verbatim>\"",
		"## Tasks\nT1. (S1) <what you will change>",
		"## Log\nL1. <the original request, verbatim>",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error lacks the template line %q:\n%v", want, err)
		}
	}
	if !strings.HasSuffix(err.Error(), "L1. <the original request, verbatim>") {
		t.Fatalf("the template ends the error:\n%v", err)
	}
}

func TestParseFeatureDocJoinsContinuationLines(t *testing.T) {
	src := "## Specs\nS1. the first line\n  and the second\n\n  and a third\n## Tasks\nT1. (S1) do it\n  in two steps\n## Log\nL1. r\n"
	doc, err := ParseFeatureDoc(src)
	if err != nil {
		t.Fatalf("ParseFeatureDoc: %v", err)
	}
	if got := doc.Specs[0].Text; got != "the first line and the second and a third" {
		t.Fatalf("spec text = %q", got)
	}
	if got := doc.Tasks[0].Text; got != "do it in two steps" {
		t.Fatalf("task text = %q", got)
	}
}

func TestParseFeatureDocIgnoresOtherSectionsAndAcceptsAnyOrder(t *testing.T) {
	src := "## Log\nL1. the request\n\n## Notes\nS9. not a spec, another section\n\n## tasks\nT1. (S1) build\n\n## SPECS\nS1. the spec\n"
	doc, err := ParseFeatureDoc(src)
	if err != nil {
		t.Fatalf("ParseFeatureDoc: %v", err)
	}
	if len(doc.Specs) != 1 || doc.Specs[0].Text != "the spec" {
		t.Fatalf("specs = %+v", doc.Specs)
	}
	if len(doc.Log) != 1 || len(doc.Tasks) != 1 {
		t.Fatalf("doc = %+v", doc)
	}
}
