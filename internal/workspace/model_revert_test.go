package workspace

import (
	"context"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/provider"
)

func TestModelNotFoundRevertsAndRetriesOnce(t *testing.T) {
	notFound := provider.Step{Err: &provider.Error{Kind: provider.KindStatus, Status: 404,
		Message: "not_found_error: model: bad-model"}}
	f := newFixture(t, notFound, text("hello back"))
	c := f.ws.Chat().(*Conversation)
	var notices []string
	c.revertWarn = func(s string) { notices = append(notices, s) }
	start := f.mock.Model()

	if err := c.SetModel("bad-model"); err != nil {
		t.Fatal(err)
	}
	reply, err := c.Send(context.Background(), "hi")
	if err != nil || reply != "hello back" {
		t.Fatalf("the retry with the reverted model must answer: reply=%q err=%v", reply, err)
	}
	if got := f.mock.Model(); got != start {
		t.Fatalf("model = %q, want the reverted %q", got, start)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "model bad-model not found · reverted to "+start) {
		t.Fatalf("notices = %v", notices)
	}
}

func TestOtherFailuresDoNotRevert(t *testing.T) {
	f := newFixture(t, provider.Step{Err: &provider.Error{Kind: provider.KindStatus, Status: 500, Message: "overloaded"}})
	c := f.ws.Chat().(*Conversation)
	if err := c.SetModel("chosen-model"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Send(context.Background(), "hi"); err == nil {
		t.Fatal("a server error is returned, not retried")
	}
	if f.mock.Model() != "chosen-model" {
		t.Fatalf("model changed on an unrelated failure: %q", f.mock.Model())
	}
}
