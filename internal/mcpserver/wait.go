package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nchdatta/whatsapp-mcp/internal/store"
)

const (
	waitPoll    = 3 * time.Second
	waitDefault = 50
	waitMax     = 120
	// Messages older than this when they're first stored come from history
	// sync after a reconnect, not from someone writing now
	waitFresh = 10 * time.Minute
)

type waitArgs struct {
	Cursor        int64 `json:"cursor,omitempty" jsonschema:"The cursor returned by the previous call. Omit on the first call to start from now"`
	TimeoutSecond int   `json:"timeout_seconds,omitempty" jsonschema:"How long to wait for a message (default 50, max 120)"`
	SkipGroups    bool  `json:"skip_groups,omitempty" jsonschema:"Ignore group messages"`
}

// waitForMessages blocks until new incoming messages arrive or the timeout
// passes, so a client can watch WhatsApp by calling it in a loop.
func (t *tools) waitForMessages(ctx context.Context, _ *mcp.CallToolRequest, a waitArgs) (*mcp.CallToolResult, any, error) {
	cursor := a.Cursor
	if cursor <= 0 {
		var err error
		if cursor, err = t.svc.History.LastSeq(ctx); err != nil {
			return fail(err)
		}
	}
	deadline := time.Now().Add(time.Duration(clamp(a.TimeoutSecond, waitDefault, waitMax)) * time.Second)

	for {
		msgs, err := t.svc.History.Since(ctx, cursor, 200)
		if err != nil {
			return fail(err)
		}
		var fresh []store.Message
		for _, m := range msgs {
			cursor = m.Seq
			if m.FromMe || time.Since(m.SentAt) > waitFresh {
				continue
			}
			if a.SkipGroups && strings.HasSuffix(m.ChatJID, "@g.us") {
				continue
			}
			fresh = append(fresh, m)
		}
		if len(fresh) > 0 {
			var b strings.Builder
			fmt.Fprintf(&b, "cursor: %d\n%d new message(s). Reply with send_text to the [chat: ...] JID, then call wait_for_messages again with this cursor.\n", cursor, len(fresh))
			for _, m := range fresh {
				b.WriteString(t.watchLine(ctx, m) + "\n")
			}
			return ok(b.String())
		}
		if time.Now().After(deadline) {
			return ok(fmt.Sprintf("cursor: %d\nNo new messages. Call wait_for_messages again with this cursor to keep watching.", cursor))
		}
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		case <-time.After(waitPoll):
		}
	}
}

// watchLine formats one message as
// "Name (+phone) in Group - 10:21 AM: [image: C:\...\file.jpg] caption  [chat: JID] [id: ID]".
func (t *tools) watchLine(ctx context.Context, m store.Message) string {
	body := m.Body
	if m.MediaKind != "" {
		tag := fmt.Sprintf("[%s id=%s: not saved yet, use get_attachment]", m.MediaKind, m.ID)
		if path := t.savedFile(ctx, m); path != "" {
			tag = fmt.Sprintf("[%s: %s]", m.MediaKind, path)
		}
		body = strings.TrimSpace(tag + " " + body)
	}
	group := ""
	if strings.HasSuffix(m.ChatJID, "@g.us") {
		title := m.ChatTitle
		if title == "" {
			title = m.ChatJID
		}
		group = " in " + title
	}
	return fmt.Sprintf("%s%s - %s: %s  [chat: %s] [id: %s]",
		t.svc.DisplayName(ctx, m.SenderJID, m.SenderName), group,
		m.SentAt.Local().Format("3:04 PM"), strings.ReplaceAll(body, "\n", "\n    "), m.ChatJID, m.ID)
}

// savedFile waits briefly for an incoming attachment to finish auto-saving.
func (t *tools) savedFile(ctx context.Context, m store.Message) string {
	for i := 0; i < 15; i++ {
		if cur, err := t.svc.History.Message(ctx, m.ChatJID, m.ID); err == nil && cur.MediaFile != "" {
			return cur.MediaFile
		}
		select {
		case <-ctx.Done():
			return ""
		case <-time.After(time.Second):
		}
	}
	return ""
}
