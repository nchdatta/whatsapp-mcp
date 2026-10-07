package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Setting returns a stored setting, or "" when it isn't set.
func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM setting WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetSetting stores a setting.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO setting (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// Unanswered returns direct chats (and groups, when groups is set) whose
// latest message is an incoming one sent between since and before, and that
// haven't had an away reply after cooldownSince.
func (s *Store) Unanswered(ctx context.Context, since, before, cooldownSince time.Time, groups bool) ([]string, error) {
	kinds := `m.chat_jid LIKE '%@s.whatsapp.net' OR m.chat_jid LIKE '%@lid'`
	if groups {
		kinds += ` OR m.chat_jid LIKE '%@g.us'`
	}
	q := `
		SELECT m.chat_jid
		FROM message m
		LEFT JOIN away_sent a ON a.chat_jid = m.chat_jid
		WHERE m.sent_at >= ?
		  AND (` + kinds + `)
		  AND (a.sent_at IS NULL OR a.sent_at < ?)
		GROUP BY m.chat_jid
		HAVING MAX(CASE WHEN m.from_me = 0 THEN m.sent_at END) > COALESCE(MAX(CASE WHEN m.from_me = 1 THEN m.sent_at END), 0)
		   AND MAX(CASE WHEN m.from_me = 0 THEN m.sent_at END) <= ?`
	rows, err := s.db.QueryContext(ctx, q, unix(since), unix(cooldownSince), unix(before))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chats []string
	for rows.Next() {
		var jid string
		if err := rows.Scan(&jid); err != nil {
			return nil, err
		}
		chats = append(chats, jid)
	}
	return chats, rows.Err()
}

// MarkAwaySent records that a chat got an away reply.
func (s *Store) MarkAwaySent(ctx context.Context, chatJID string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO away_sent (chat_jid, sent_at) VALUES (?, ?)
		ON CONFLICT (chat_jid) DO UPDATE SET sent_at = excluded.sent_at`, chatJID, unix(at))
	return err
}
