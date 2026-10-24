package ui

import (
	"strings"
	"testing"
)

func TestAgentUpgradePrintsWhatChanged(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/agent upgrade --all")
	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, "devjunior: up to date") || !strings.Contains(out, "reviewer: up to date") {
		t.Fatalf("upgrade output:\n%s", out)
	}
}

func TestAgentUpgradeNeedsOneArgument(t *testing.T) {
	m, _ := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/agent upgrade")
	if !strings.Contains(stripANSI(m.transcriptText()), "usage: /agent upgrade <name>|--all") {
		t.Fatal("a bare upgrade should print its usage")
	}
}
