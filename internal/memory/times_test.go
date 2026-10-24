package memory

import (
	"context"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
)

func TestMessagesCarryTheirWriteTime(t *testing.T) {
	ctx := context.Background()
	s := openTempSQLite(t)
	before := time.Now().UTC().Add(-time.Second)
	seedSession(t, s, "s-1", "reader", base, userText("first"), assistantText("second"))
	times, err := s.SessionTimes(ctx, "s-1")
	if err != nil || len(times) != 2 {
		t.Fatalf("SessionTimes = %v, %v", times, err)
	}
	for _, at := range times {
		if at.Before(before) || at.IsZero() {
			t.Fatalf("message time %v is not the write time", at)
		}
	}
}

func TestOldMessagesHaveNoTime(t *testing.T) {
	ctx := context.Background()
	s := openTempSQLite(t)
	seedSession(t, s, "s-1", "reader", base, userText("kept"))
	if _, err := s.db.ExecContext(ctx, `UPDATE messages SET created_at = NULL WHERE session_id = 's-1'`); err != nil {
		t.Fatal(err)
	}
	times, err := s.SessionTimes(ctx, "s-1")
	if err != nil || len(times) != 1 || !times[0].IsZero() {
		t.Fatalf("a message without a time has the zero time: %v, %v", times, err)
	}
	msgs, _ := s.SessionMessages(ctx, "s-1")
	if len(msgs) != 1 || msgs[0].Role != api.RoleUser {
		t.Fatal("the message itself is still there")
	}
}
