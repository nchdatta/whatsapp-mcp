package store

import (
	"context"
	"testing"
	"time"
)

func TestUnanswered(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	now := time.Now().Truncate(time.Second)
	put := func(chat, id string, fromMe bool, at time.Time) {
		t.Helper()
		must(t, s.PutChat(ctx, chat, "", false, at))
		must(t, s.PutMessage(ctx, &Message{ChatJID: chat, ID: id, SenderJID: chat, FromMe: fromMe, SentAt: at, Body: id}))
	}
	put("waiting@s.whatsapp.net", "1", false, now.Add(-2*time.Minute)) // unanswered, old enough
	put("answered@s.whatsapp.net", "2", false, now.Add(-3*time.Minute))
	put("answered@s.whatsapp.net", "3", true, now.Add(-2*time.Minute)) // I replied
	put("fresh@lid", "4", false, now.Add(-10*time.Second))             // still within the delay
	put("group@g.us", "5", false, now.Add(-2*time.Minute))
	put("ancient@s.whatsapp.net", "6", false, now.Add(-2*time.Hour)) // backlog, ignored

	since, before := now.Add(-30*time.Minute), now.Add(-time.Minute)
	got, err := s.Unanswered(ctx, since, before, now.Add(-3*time.Hour), false)
	must(t, err)
	if len(got) != 1 || got[0] != "waiting@s.whatsapp.net" {
		t.Fatalf("Unanswered = %v", got)
	}
	got, err = s.Unanswered(ctx, since, before, now.Add(-3*time.Hour), true)
	must(t, err)
	if len(got) != 2 {
		t.Fatalf("with groups: %v", got)
	}

	// Cooldown: an away reply was sent recently, so don't send another
	must(t, s.MarkAwaySent(ctx, "waiting@s.whatsapp.net", now.Add(-time.Hour)))
	got, err = s.Unanswered(ctx, since, before, now.Add(-3*time.Hour), false)
	must(t, err)
	if len(got) != 0 {
		t.Fatalf("after away reply: %v", got)
	}

	must(t, s.SetSetting(ctx, "k", "v1"))
	must(t, s.SetSetting(ctx, "k", "v2"))
	if v, _ := s.Setting(ctx, "k"); v != "v2" {
		t.Fatalf("Setting = %q", v)
	}
}
