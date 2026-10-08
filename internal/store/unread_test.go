package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestUnreadCounts(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	t0 := time.Unix(1_700_000_000, 0)
	chat := "1@s.whatsapp.net"

	must(t, s.PutChat(ctx, chat, "Alice", false, t0))
	put := func(id string, fromMe bool, at time.Time) {
		must(t, s.PutMessage(ctx, &Message{ChatJID: chat, ID: id, FromMe: fromMe, SentAt: at, Body: id}))
	}
	put("a", false, t0)
	put("b", true, t0.Add(time.Minute))
	put("c", false, t0.Add(2*time.Minute))
	put("d", false, t0.Add(3*time.Minute))

	unread := func() int {
		c, err := s.Chat(ctx, chat)
		must(t, err)
		return c.Unread
	}
	if n := unread(); n != 3 {
		t.Fatalf("new chat: unread = %d, want 3 (own messages don't count)", n)
	}

	must(t, s.KeepUnread(ctx, chat, 2))
	if n := unread(); n != 2 {
		t.Errorf("after KeepUnread(2): unread = %d", n)
	}
	only := true
	if chats, err := s.Chats(ctx, ChatFilter{UnreadOnly: only, Limit: 10}); err != nil || len(chats) != 1 {
		t.Errorf("unread_only: %v, %v", chats, err)
	}
	msgs, err := s.Unread(ctx, chat, 10)
	must(t, err)
	if len(msgs) != 2 || msgs[0].ID != "c" {
		t.Errorf("Unread = %v", msgs)
	}

	must(t, s.MarkRead(ctx, chat, t0.Add(3*time.Minute)))
	must(t, s.MarkRead(ctx, chat, t0)) // never moves back
	if n := unread(); n != 0 {
		t.Errorf("after MarkRead: unread = %d", n)
	}
	if chats, _ := s.Chats(ctx, ChatFilter{UnreadOnly: true, Limit: 10}); len(chats) != 0 {
		t.Errorf("unread_only after read: %v", chats)
	}
}

func TestQuotedRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	t0 := time.Unix(1_700_000_000, 0)
	chat := "1@s.whatsapp.net"
	must(t, s.PutChat(ctx, chat, "", false, t0))
	must(t, s.PutMessage(ctx, &Message{ChatJID: chat, ID: "r", SentAt: t0, Body: "yes",
		QuotedID: "q", QuotedSender: "2@s.whatsapp.net", QuotedBody: "coming?"}))
	// A later copy without the quote keeps it
	must(t, s.PutMessage(ctx, &Message{ChatJID: chat, ID: "r", SentAt: t0, Body: "yes"}))

	m, err := s.Message(ctx, chat, "r")
	must(t, err)
	if m.QuotedID != "q" || m.QuotedSender != "2@s.whatsapp.net" || m.QuotedBody != "coming?" {
		t.Errorf("quote = %q %q %q", m.QuotedID, m.QuotedSender, m.QuotedBody)
	}
}

// A database from before last_read existed is upgraded with its history
// counted as read.
func TestUpgradeMarksHistoryRead(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := sql.Open(Driver, DSN(filepath.Join(dir, FileName), testKey))
	must(t, err)
	_, err = db.ExecContext(ctx, `
		CREATE TABLE chat (jid TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', is_group INTEGER NOT NULL DEFAULT 0, last_activity INTEGER NOT NULL DEFAULT 0);
		CREATE TABLE message (seq INTEGER PRIMARY KEY AUTOINCREMENT, chat_jid TEXT NOT NULL REFERENCES chat(jid), msg_id TEXT NOT NULL,
			sender_jid TEXT NOT NULL DEFAULT '', sender_name TEXT NOT NULL DEFAULT '', from_me INTEGER NOT NULL DEFAULT 0, sent_at INTEGER NOT NULL,
			body TEXT NOT NULL DEFAULT '', media_kind TEXT NOT NULL DEFAULT '', media_mime TEXT NOT NULL DEFAULT '', media_name TEXT NOT NULL DEFAULT '',
			media_size INTEGER NOT NULL DEFAULT 0, media_ref BLOB, media_file TEXT NOT NULL DEFAULT '', UNIQUE (chat_jid, msg_id));
		INSERT INTO chat VALUES ('1@s.whatsapp.net', 'Alice', 0, 1700000000);
		INSERT INTO message (chat_jid, msg_id, sent_at, body) VALUES ('1@s.whatsapp.net', 'a', 1700000000, 'hi');`)
	must(t, err)
	db.Close()

	s, err := Open(ctx, dir, testKey)
	must(t, err)
	defer s.Close()
	c, err := s.Chat(ctx, "1@s.whatsapp.net")
	must(t, err)
	if c.Unread != 0 {
		t.Errorf("old history shows %d unread", c.Unread)
	}
	m, err := s.Message(ctx, "1@s.whatsapp.net", "a")
	must(t, err)
	if m.Body != "hi" || m.QuotedID != "" {
		t.Errorf("old message = %+v", m)
	}
}
