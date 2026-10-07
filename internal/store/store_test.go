package store

import (
	"context"
	"testing"
	"time"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestChatTitleIsNotOverwrittenByEmpty(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	t0 := time.Unix(1_700_000_000, 0)

	must(t, s.PutChat(ctx, "1@s.whatsapp.net", "Alice", false, t0))
	must(t, s.PutChat(ctx, "1@s.whatsapp.net", "", false, t0.Add(-time.Hour)))

	c, err := s.Chat(ctx, "1@s.whatsapp.net")
	must(t, err)
	if c.Title != "Alice" {
		t.Errorf("title = %q, want Alice", c.Title)
	}
	if !c.LastActivity.Equal(t0) {
		t.Errorf("last activity moved backwards: %v", c.LastActivity)
	}
}

func TestUpsertKeepsSeqAndMediaFile(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	t0 := time.Unix(1_700_000_000, 0)
	must(t, s.PutChat(ctx, "g@g.us", "Group", true, t0))

	m := &Message{ChatJID: "g@g.us", ID: "A", SenderJID: "2@s.whatsapp.net", SentAt: t0, MediaKind: "image"}
	must(t, s.PutMessage(ctx, m))
	first, err := s.Message(ctx, "g@g.us", "A")
	must(t, err)
	must(t, s.SetMediaFile(ctx, "g@g.us", "A", "/tmp/a.jpg"))

	m.Body = "edited caption"
	must(t, s.PutMessage(ctx, m))
	again, err := s.Message(ctx, "g@g.us", "A")
	must(t, err)

	if again.Seq != first.Seq {
		t.Errorf("seq changed: %d -> %d", first.Seq, again.Seq)
	}
	if again.MediaFile != "/tmp/a.jpg" {
		t.Errorf("media file lost: %q", again.MediaFile)
	}
	if again.Body != "edited caption" {
		t.Errorf("body not updated: %q", again.Body)
	}
}

func TestSearchAndAround(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	t0 := time.Unix(1_700_000_000, 0)
	must(t, s.PutChat(ctx, "c@s.whatsapp.net", "Bob", false, t0))
	bodies := []string{"one", "two 100%", "three", "four", "five"}
	for i, b := range bodies {
		// Two messages share a timestamp to exercise the seq tie-break
		ts := t0.Add(time.Duration(i/2*2) * time.Minute)
		must(t, s.PutMessage(ctx, &Message{ChatJID: "c@s.whatsapp.net", ID: b, SenderJID: "c@s.whatsapp.net", SentAt: ts, Body: b}))
	}

	got, err := s.Messages(ctx, MessageFilter{Text: "100%", Limit: 10})
	must(t, err)
	if len(got) != 1 || got[0].ID != "two 100%" {
		t.Errorf("wildcard search returned %v", ids(got))
	}

	got, err = s.Messages(ctx, MessageFilter{Text: "%", Limit: 10})
	must(t, err)
	if len(got) != 1 {
		t.Errorf("literal %% should match one message, got %v", ids(got))
	}

	around, err := s.Around(ctx, "c@s.whatsapp.net", "three", 1)
	must(t, err)
	if want := []string{"two 100%", "three", "four"}; !equal(ids(around), want) {
		t.Errorf("around = %v, want %v", ids(around), want)
	}

	got, err = s.Messages(ctx, MessageFilter{SenderJID: "c", Limit: 10})
	must(t, err)
	if len(got) != len(bodies) {
		t.Errorf("sender filter by bare user found %d, want %d", len(got), len(bodies))
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func ids(ms []Message) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSince(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	start, err := s.LastSeq(ctx)
	must(t, err)
	if start != 0 {
		t.Fatalf("empty store: LastSeq = %d", start)
	}
	must(t, s.PutChat(ctx, "c@s.whatsapp.net", "Bob", false, time.Now()))
	for _, id := range []string{"a", "b", "c"} {
		must(t, s.PutMessage(ctx, &Message{ChatJID: "c@s.whatsapp.net", ID: id, SenderJID: "c@s.whatsapp.net", SentAt: time.Now(), Body: id}))
	}
	all, err := s.Since(ctx, 0, 10)
	must(t, err)
	if len(all) != 3 || all[0].ID != "a" {
		t.Fatalf("Since(0) = %v", all)
	}
	rest, err := s.Since(ctx, all[0].Seq, 10)
	must(t, err)
	if len(rest) != 2 || rest[0].ID != "b" {
		t.Fatalf("Since(first) = %v", rest)
	}
	last, err := s.LastSeq(ctx)
	must(t, err)
	if last != all[2].Seq {
		t.Fatalf("LastSeq = %d, want %d", last, all[2].Seq)
	}
}
