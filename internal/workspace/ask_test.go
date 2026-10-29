package workspace

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/catalog"
)

// answered is one answer delivered through Hooks.OnAsk.
type answered struct {
	origin, key, text string
	err               error
}

// addAskAgent adds an agent to the catalog and assigns it to the default space.
func addAskAgent(t *testing.T, f *fixture, name string) {
	t.Helper()
	if err := catalog.Save(f.home, catalog.AgentDef{Name: name, Description: name, Tools: []string{"recall"}}, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	addAgentToDefault(t, f, name)
}

// awaitAnswer waits for the next answer to an ask.
func awaitAnswer(t *testing.T, ch chan answered) answered {
	t.Helper()
	select {
	case a := <-ch:
		return a
	case <-time.After(2 * time.Second):
		t.Fatal("no answer to the ask")
	}
	return answered{}
}

func TestAskAnswerReachesTheOriginAndStaysWithTheAgent(t *testing.T) {
	f := newFixture(t, text("the answer"))
	addAskAgent(t, f, "scribe")
	answers := make(chan answered, 4)
	f.ws.opts.Hooks.OnAsk = func(origin, key, text string, err error) {
		answers <- answered{origin, key, text, err}
	}
	if err := f.ws.SelectAgent("scribe"); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.Ask("default/scribe", "default/reader", "what do you read?"); err != nil {
		t.Fatal(err)
	}

	got := awaitAnswer(t, answers)
	if got.origin != "default/scribe" || got.key != "default/reader" || got.text != "the answer" || got.err != nil {
		t.Fatalf("answer = %+v, want origin default/scribe from default/reader", got)
	}
	if last := f.mock.Calls[0].Messages[len(f.mock.Calls[0].Messages)-1].Content[0].Text; !strings.Contains(last, "what do you read?") {
		t.Fatalf("the reader must receive the question:\n%s", last)
	}
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	if n := f.ws.Chat().MessageCount(); n != 2 {
		t.Fatalf("the reader keeps the question and the answer, has %d messages", n)
	}
	if err := f.ws.SelectAgent("scribe"); err != nil {
		t.Fatal(err)
	}
	if n := f.ws.Chat().MessageCount(); n != 0 {
		t.Fatalf("the answer must not enter the context of the screen's agent, scribe has %d messages", n)
	}
}

func TestBusyAgentReadsTheAskWhenItsTurnEnds(t *testing.T) {
	f := newFixture(t, text("late answer"))
	addAskAgent(t, f, "scribe")
	answers := make(chan answered, 4)
	f.ws.opts.Hooks.OnAsk = func(origin, key, text string, err error) {
		answers <- answered{origin, key, text, err}
	}
	conv := f.ws.Chat().(*Conversation)
	conv.busy.Store(true)

	if err := f.ws.Ask("default/scribe", "default/reader", "are you there?"); err != nil {
		t.Fatal(err)
	}
	if len(f.mock.Calls) != 0 {
		t.Fatal("a busy agent must not start a turn for the ask")
	}
	select {
	case a := <-answers:
		t.Fatalf("the ask was answered while the agent was busy: %+v", a)
	case <-time.After(50 * time.Millisecond):
	}

	conv.busy.Store(false)
	conv.releaseTurn()
	got := awaitAnswer(t, answers)
	if got.origin != "default/scribe" || got.text != "late answer" {
		t.Fatalf("answer = %+v", got)
	}
}

func TestAskToUnknownRecipientIsRefused(t *testing.T) {
	f := newFixture(t)
	if err := f.ws.Ask("default/scribe", "default/nobody", "hi"); err == nil {
		t.Fatal("an ask to an agent that is not in the space must be refused")
	}
	if len(f.mock.Calls) != 0 {
		t.Fatal("a refused ask starts no turn")
	}
}

func TestAskAnswerIsNotReportedWithoutAnOrigin(t *testing.T) {
	f := newFixture(t, text("unexpected"))
	// The automatic turn would race the test's own turn: only the read matters here.
	f.ws.opts.Config.Work.MaxAutoTurns = 0
	answers := make(chan answered, 4)
	f.ws.opts.Hooks.OnAsk = func(origin, key, text string, err error) {
		answers <- answered{origin, key, text, err}
	}
	// A message from an agent is not an ask: nothing is reported to an origin.
	deliverFromWriter(t, f, "note", "plain message")
	_ = f.ws.SelectAgent("reader")
	if _, err := f.ws.Chat().Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	select {
	case a := <-answers:
		t.Fatalf("a plain message must not report an ask answer: %+v", a)
	case <-time.After(50 * time.Millisecond):
	}
}
