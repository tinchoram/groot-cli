package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// newOnboardingModel starts a model with no operator name, as on the first run.
func newOnboardingModel(t *testing.T) (Model, *fakeWorkspace, *fakeSession) {
	t.Helper()
	chat := &fakeSession{model: "m", reply: "ok"}
	ws := newFakeWorkspace(chat)
	ws.operator = ""
	m := NewModel(Options{Workspace: ws, Version: "test"})
	m.width, m.height = 120, 40
	m.resize()
	return m, ws, chat
}

func TestBannerShowsOperatorNextToVersion(t *testing.T) {
	out := stripANSI(bannerView(120, shineDisabled, "0.6.0-dev", "Tinchoram"))
	if !strings.Contains(out, "v0.6.0-dev") || !strings.Contains(out, "operated by: @Tinchoram") {
		t.Fatalf("banner lacks the version or the operator:\n%s", out)
	}
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "operated by") {
			line = l
		}
	}
	if !strings.Contains(line, "v0.6.0-dev") {
		t.Fatalf("operator must sit on the version line, got %q", line)
	}
	if col(out, "v0.6.0-dev") != col(out, tagline) {
		t.Fatal("the version line stays aligned with the tagline")
	}
}

func TestBannerWithoutNameOmitsOperator(t *testing.T) {
	for _, width := range []int{30, 60, 120} {
		out := stripANSI(bannerView(width, shineDisabled, "0.6.0-dev", ""))
		if strings.Contains(out, "operated by") {
			t.Fatalf("width %d: no name, no operator line:\n%s", width, out)
		}
		if !strings.Contains(out, "v0.6.0-dev") {
			t.Fatalf("width %d: the version must stay", width)
		}
	}
}

func TestBannerOperatorWithoutVersion(t *testing.T) {
	out := stripANSI(bannerView(120, shineDisabled, "", "Tinchoram"))
	if !strings.Contains(out, "operated by: @Tinchoram") {
		t.Fatalf("the operator line must show even without a version:\n%s", out)
	}
}

func TestHelpListsMe(t *testing.T) {
	m, _, _ := newMentionModel(t)
	if !strings.Contains(stripANSI(helpBlock()), "/me [name]") {
		t.Fatal("/help must list /me")
	}
	_ = m
}

func TestMeShowsTheName(t *testing.T) {
	m, _, _ := newMentionModel(t)
	m = sendLine(t, m, "/me")
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "you are @Tinchoram") {
		t.Fatalf("/me must show the name:\n%s", out)
	}
}

func TestMeWithoutNameAsksForOne(t *testing.T) {
	m, ws, _ := newOnboardingModel(t)
	m, _ = press(m, tea.KeyEsc) // skip the first-start prompt for this session
	m = sendLine(t, m, "/me")
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "no name set: /me <name>") {
		t.Fatalf("/me without a name must say how to set one:\n%s", out)
	}
	if ws.operator != "" {
		t.Fatalf("showing the name must not change it: %q", ws.operator)
	}
}

func TestMeSetsNameSavesAndRefreshesBanner(t *testing.T) {
	m, ws, chat := newMentionModel(t)
	m = sendLine(t, m, "/me @Martin")

	if ws.operator != "Martin" {
		t.Fatalf("operator = %q, want Martin", ws.operator)
	}
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "you are @Martin") {
		t.Fatalf("/me <name> must confirm:\n%s", out)
	}
	if out := stripANSI(m.banner()); !strings.Contains(out, "operated by: @Martin") {
		t.Fatalf("the banner must show the new name:\n%s", out)
	}
	if len(chat.sends) != 0 {
		t.Fatalf("/me must not reach the agent: %v", chat.sends)
	}
}

func TestMeRejectsInvalidNameAndKeepsOld(t *testing.T) {
	m, ws, _ := newMentionModel(t)
	m = sendLine(t, m, "/me no way")
	if ws.operator != "Tinchoram" {
		t.Fatalf("an invalid name changed the operator to %q", ws.operator)
	}
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "user.name must be 2-32 letters, digits, _ or -") {
		t.Fatalf("missing validation error:\n%s", out)
	}
}

func TestOnboardingAsksOnFirstStartWithoutName(t *testing.T) {
	m, _, _ := newOnboardingModel(t)
	if !m.onboard {
		t.Fatal("no name set: the first start must ask for one")
	}
	if m.input.Placeholder != onboardingPrompt {
		t.Fatalf("placeholder = %q", m.input.Placeholder)
	}
	if !strings.Contains(onboardingPrompt, "How should agents call you? (one word, e.g. Tinchoram)") {
		t.Fatalf("prompt text = %q", onboardingPrompt)
	}
}

func TestOnboardingSavesTheName(t *testing.T) {
	m, ws, chat := newOnboardingModel(t)
	m = typeText(m, "Tinchoram")
	m, _ = press(m, tea.KeyEnter)

	if ws.operator != "Tinchoram" {
		t.Fatalf("operator = %q, want Tinchoram", ws.operator)
	}
	if m.onboard || m.input.Placeholder != "" {
		t.Fatalf("onboarding must end: onboard=%v placeholder=%q", m.onboard, m.input.Placeholder)
	}
	if len(chat.sends) != 0 {
		t.Fatalf("the name is not a message: %v", chat.sends)
	}
}

func TestOnboardingSkipAsksAgainNextStart(t *testing.T) {
	m, ws, _ := newOnboardingModel(t)
	m, _ = press(m, tea.KeyEsc)
	if m.onboard {
		t.Fatal("esc skips the prompt for this session")
	}
	if ws.operator != "" {
		t.Fatalf("skipping must not save a name: %q", ws.operator)
	}
	if m.input.Placeholder != "" {
		t.Fatalf("the prompt must leave the input: %q", m.input.Placeholder)
	}

	again, _, _ := newOnboardingModel(t)
	if !again.onboard {
		t.Fatal("the next start asks again")
	}
}

func TestOnboardingInvalidNameStaysOnPrompt(t *testing.T) {
	m, ws, _ := newOnboardingModel(t)
	m = typeText(m, "x")
	m, _ = press(m, tea.KeyEnter)

	if !m.onboard || m.input.Placeholder != onboardingPrompt {
		t.Fatalf("an invalid name keeps the prompt: onboard=%v", m.onboard)
	}
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "user.name must be 2-32 letters, digits, _ or -") {
		t.Fatalf("missing validation error:\n%s", out)
	}
	if ws.operator != "" {
		t.Fatalf("an invalid name must not be saved: %q", ws.operator)
	}
}
