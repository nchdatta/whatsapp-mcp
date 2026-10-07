// Package store persists chats and messages in a local SQLite database.
//
// Times are stored as unix seconds. Every message gets a monotonically
// increasing seq, which external tools can use to tail new messages.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// FileName is the history database inside the data directory.
const FileName = "history.db"

const schema = `
CREATE TABLE IF NOT EXISTS chat (
	jid           TEXT PRIMARY KEY,
	title         TEXT    NOT NULL DEFAULT '',
	is_group      INTEGER NOT NULL DEFAULT 0,
	last_activity INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS message (
	seq         INTEGER PRIMARY KEY AUTOINCREMENT,
	chat_jid    TEXT    NOT NULL REFERENCES chat(jid),
	msg_id      TEXT    NOT NULL,
	sender_jid  TEXT    NOT NULL DEFAULT '',
	sender_name TEXT    NOT NULL DEFAULT '',
	from_me     INTEGER NOT NULL DEFAULT 0,
	sent_at     INTEGER NOT NULL,
	body        TEXT    NOT NULL DEFAULT '',
	media_kind  TEXT    NOT NULL DEFAULT '',
	media_mime  TEXT    NOT NULL DEFAULT '',
	media_name  TEXT    NOT NULL DEFAULT '',
	media_size  INTEGER NOT NULL DEFAULT 0,
	media_ref   BLOB,
	media_file  TEXT    NOT NULL DEFAULT '',
	UNIQUE (chat_jid, msg_id)
);

CREATE INDEX IF NOT EXISTS message_by_chat ON message (chat_jid, sent_at);
CREATE INDEX IF NOT EXISTS message_by_time ON message (sent_at);
`

// DSN returns a modernc.org/sqlite connection string with WAL and a busy
// timeout, so other processes can read while whatsapp-mcp writes.
func DSN(path string) string {
	return "file:" + filepath.ToSlash(path) +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
}

type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the history database in dataDir.
func Open(ctx context.Context, dataDir string) (*Store, error) {
	db, err := sql.Open("sqlite", DSN(filepath.Join(dataDir, FileName)))
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// ---- Writing ----

// Chat is a conversation, either direct or a group.
type Chat struct {
	JID          string    `json:"jid"`
	Title        string    `json:"title"`
	IsGroup      bool      `json:"is_group"`
	LastActivity time.Time `json:"last_activity"`
	Preview      string    `json:"last_message,omitempty"`
}

// PutChat records a chat. An empty title never replaces a known one, and
// last_activity only moves forward.
func (s *Store) PutChat(ctx context.Context, jid, title string, isGroup bool, activity time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO chat (jid, title, is_group, last_activity) VALUES (?, ?, ?, ?)
		ON CONFLICT (jid) DO UPDATE SET
			title = CASE WHEN excluded.title <> '' THEN excluded.title ELSE chat.title END,
			last_activity = MAX(chat.last_activity, excluded.last_activity)`,
		jid, title, isGroup, unix(activity))
	return err
}

// SetChatTitle replaces a chat's title (e.g. after a group rename).
func (s *Store) SetChatTitle(ctx context.Context, jid, title string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE chat SET title = ? WHERE jid = ?`, title, jid)
	return err
}

// ChatTitle returns the stored title, or "" if unknown.
func (s *Store) ChatTitle(ctx context.Context, jid string) string {
	var title string
	s.db.QueryRowContext(ctx, `SELECT title FROM chat WHERE jid = ?`, jid).Scan(&title)
	return title
}

// Message is one stored message.
type Message struct {
	Seq        int64     `json:"-"`
	ChatJID    string    `json:"chat_jid"`
	ChatTitle  string    `json:"chat_title,omitempty"`
	ID         string    `json:"id"`
	SenderJID  string    `json:"sender_jid"`
	SenderName string    `json:"sender_name,omitempty"`
	FromMe     bool      `json:"from_me"`
	SentAt     time.Time `json:"sent_at"`
	Body       string    `json:"body,omitempty"`
	MediaKind  string    `json:"media_kind,omitempty"`
	MediaMime  string    `json:"media_mime,omitempty"`
	MediaName  string    `json:"media_name,omitempty"`
	MediaSize  int64     `json:"media_size,omitempty"`
	MediaRef   []byte    `json:"-"`
	MediaFile  string    `json:"media_file,omitempty"`
}

// PutMessage inserts or refreshes a message. Its seq and any saved media file
// are preserved when the message is seen again (e.g. in a later history sync).
func (s *Store) PutMessage(ctx context.Context, m *Message) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO message (chat_jid, msg_id, sender_jid, sender_name, from_me, sent_at, body,
			media_kind, media_mime, media_name, media_size, media_ref, media_file)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (chat_jid, msg_id) DO UPDATE SET
			sender_jid  = excluded.sender_jid,
			sender_name = CASE WHEN excluded.sender_name <> '' THEN excluded.sender_name ELSE message.sender_name END,
			body        = excluded.body,
			media_kind  = excluded.media_kind,
			media_mime  = excluded.media_mime,
			media_name  = excluded.media_name,
			media_size  = excluded.media_size,
			media_ref   = COALESCE(excluded.media_ref, message.media_ref),
			media_file  = CASE WHEN message.media_file <> '' THEN message.media_file ELSE excluded.media_file END`,
		m.ChatJID, m.ID, m.SenderJID, m.SenderName, m.FromMe, unix(m.SentAt), m.Body,
		m.MediaKind, m.MediaMime, m.MediaName, m.MediaSize, m.MediaRef, m.MediaFile)
	return err
}

// SetMediaFile records where a message's media was saved locally.
func (s *Store) SetMediaFile(ctx context.Context, chatJID, msgID, path string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE message SET media_file = ? WHERE chat_jid = ? AND msg_id = ?`, path, chatJID, msgID)
	return err
}

// ---- Reading ----

// ErrNotFound is returned when a requested chat or message does not exist.
var ErrNotFound = errors.New("not found")

const chatSelect = `
	SELECT c.jid, c.title, c.is_group, c.last_activity,
		COALESCE((SELECT CASE WHEN m.body <> '' THEN m.body ELSE '[' || m.media_kind || ']' END
		          FROM message m WHERE m.chat_jid = c.jid ORDER BY m.sent_at DESC, m.seq DESC LIMIT 1), '')
	FROM chat c`

func scanChat(row interface{ Scan(...any) error }) (Chat, error) {
	var c Chat
	var activity int64
	err := row.Scan(&c.JID, &c.Title, &c.IsGroup, &activity, &c.Preview)
	c.LastActivity = fromUnix(activity)
	return c, err
}

// Chats lists chats, most recently active first, optionally filtered by a
// substring of the title or JID.
func (s *Store) Chats(ctx context.Context, query string, groupsOnly *bool, limit, offset int) ([]Chat, error) {
	var where []string
	var args []any
	if query != "" {
		where = append(where, `(c.title LIKE ? ESCAPE '\' OR c.jid LIKE ? ESCAPE '\')`)
		args = append(args, like(query), like(query))
	}
	if groupsOnly != nil {
		where = append(where, `c.is_group = ?`)
		args = append(args, *groupsOnly)
	}
	q := chatSelect
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += ` ORDER BY c.last_activity DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	chats := []Chat{}
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		chats = append(chats, c)
	}
	return chats, rows.Err()
}

// Chat returns one chat by JID.
func (s *Store) Chat(ctx context.Context, jid string) (Chat, error) {
	c, err := scanChat(s.db.QueryRowContext(ctx, chatSelect+` WHERE c.jid = ?`, jid))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

const messageSelect = `
	SELECT m.seq, m.chat_jid, COALESCE(c.title, ''), m.msg_id, m.sender_jid, m.sender_name, m.from_me,
		m.sent_at, m.body, m.media_kind, m.media_mime, m.media_name, m.media_size, m.media_ref, m.media_file
	FROM message m LEFT JOIN chat c ON c.jid = m.chat_jid`

func scanMessage(row interface{ Scan(...any) error }) (Message, error) {
	var m Message
	var sent int64
	err := row.Scan(&m.Seq, &m.ChatJID, &m.ChatTitle, &m.ID, &m.SenderJID, &m.SenderName, &m.FromMe,
		&sent, &m.Body, &m.MediaKind, &m.MediaMime, &m.MediaName, &m.MediaSize, &m.MediaRef, &m.MediaFile)
	m.SentAt = fromUnix(sent)
	return m, err
}

func (s *Store) queryMessages(ctx context.Context, q string, args ...any) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	msgs := []Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// MessageFilter narrows a message search. Zero values mean "no filter".
type MessageFilter struct {
	ChatJID   string
	SenderJID string // full JID or bare user part
	Text      string
	MediaOnly bool
	After     time.Time
	Before    time.Time
	Limit     int
	Offset    int
}

// Messages returns matching messages, newest first.
func (s *Store) Messages(ctx context.Context, f MessageFilter) ([]Message, error) {
	var where []string
	var args []any
	if f.ChatJID != "" {
		where = append(where, `m.chat_jid = ?`)
		args = append(args, f.ChatJID)
	}
	if f.SenderJID != "" {
		user, _, _ := strings.Cut(f.SenderJID, "@")
		where = append(where, `(m.sender_jid = ? OR m.sender_jid LIKE ?)`)
		args = append(args, user, user+"@%")
	}
	if f.Text != "" {
		where = append(where, `m.body LIKE ? ESCAPE '\'`)
		args = append(args, like(f.Text))
	}
	if f.MediaOnly {
		where = append(where, `m.media_kind <> ''`)
	}
	if !f.After.IsZero() {
		where = append(where, `m.sent_at > ?`)
		args = append(args, unix(f.After))
	}
	if !f.Before.IsZero() {
		where = append(where, `m.sent_at < ?`)
		args = append(args, unix(f.Before))
	}
	q := messageSelect
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += ` ORDER BY m.sent_at DESC, m.seq DESC LIMIT ? OFFSET ?`
	args = append(args, f.Limit, f.Offset)
	return s.queryMessages(ctx, q, args...)
}

// Message returns one message.
func (s *Store) Message(ctx context.Context, chatJID, msgID string) (Message, error) {
	m, err := scanMessage(s.db.QueryRowContext(ctx, messageSelect+` WHERE m.chat_jid = ? AND m.msg_id = ?`, chatJID, msgID))
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// FindMessage looks a message up by ID alone, for callers that lost the chat.
func (s *Store) FindMessage(ctx context.Context, msgID string) (Message, error) {
	m, err := scanMessage(s.db.QueryRowContext(ctx, messageSelect+` WHERE m.msg_id = ? LIMIT 1`, msgID))
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// Around returns up to n messages on each side of the given message, in
// chronological order, with the target included.
func (s *Store) Around(ctx context.Context, chatJID, msgID string, n int) ([]Message, error) {
	target, err := s.Message(ctx, chatJID, msgID)
	if err != nil {
		return nil, err
	}
	// (sent_at, seq) gives a total order even when timestamps collide
	older, err := s.queryMessages(ctx, messageSelect+`
		WHERE m.chat_jid = ? AND (m.sent_at < ? OR (m.sent_at = ? AND m.seq < ?))
		ORDER BY m.sent_at DESC, m.seq DESC LIMIT ?`,
		chatJID, unix(target.SentAt), unix(target.SentAt), target.Seq, n)
	if err != nil {
		return nil, err
	}
	newer, err := s.queryMessages(ctx, messageSelect+`
		WHERE m.chat_jid = ? AND (m.sent_at > ? OR (m.sent_at = ? AND m.seq > ?))
		ORDER BY m.sent_at ASC, m.seq ASC LIMIT ?`,
		chatJID, unix(target.SentAt), unix(target.SentAt), target.Seq, n)
	if err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(older)+1+len(newer))
	for i := len(older) - 1; i >= 0; i-- {
		out = append(out, older[i])
	}
	out = append(out, target)
	return append(out, newer...), nil
}

// ---- helpers ----

func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func fromUnix(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(v, 0)
}

// like builds a case-insensitive (for ASCII) substring pattern, escaping wildcards.
func like(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}
