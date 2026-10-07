package mcpserver

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nchdatta/whatsapp-mcp/internal/store"
)

type (
	noArgs struct{}

	listChatsArgs struct {
		Query      string `json:"query,omitempty" jsonschema:"Only chats whose title or JID contains this"`
		GroupsOnly *bool  `json:"groups_only,omitempty" jsonschema:"true: only groups; false: only direct chats; omit for both"`
		Limit      int    `json:"limit,omitempty" jsonschema:"Max chats (default 25, max 200)"`
		Offset     int    `json:"offset,omitempty" jsonschema:"Skip this many chats, for paging"`
	}

	readChatArgs struct {
		Chat   string `json:"chat" jsonschema:"Chat JID, phone number, or exact chat title"`
		Limit  int    `json:"limit,omitempty" jsonschema:"Number of most recent messages (default 40, max 300)"`
		Before string `json:"before,omitempty" jsonschema:"Only messages before this date/time, to page back through history"`
	}

	searchArgs struct {
		Text      string `json:"text,omitempty" jsonschema:"Text the message must contain"`
		Chat      string `json:"chat,omitempty" jsonschema:"Limit to one chat (JID, phone number or exact title)"`
		Sender    string `json:"sender,omitempty" jsonschema:"Limit to a sender (JID or phone number digits)"`
		After     string `json:"after,omitempty" jsonschema:"Only messages after this date/time"`
		Before    string `json:"before,omitempty" jsonschema:"Only messages before this date/time"`
		MediaOnly bool   `json:"media_only,omitempty" jsonschema:"Only messages with attachments"`
		Limit     int    `json:"limit,omitempty" jsonschema:"Max results (default 30, max 200)"`
		Offset    int    `json:"offset,omitempty" jsonschema:"Skip this many results, for paging"`
	}

	contextArgs struct {
		Chat      string `json:"chat" jsonschema:"Chat JID (shown in search results)"`
		MessageID string `json:"message_id" jsonschema:"The message ID"`
		Around    int    `json:"around,omitempty" jsonschema:"Messages to show on each side (default 6, max 50)"`
	}

	findContactsArgs struct {
		Query string `json:"query" jsonschema:"Part of a name or phone number"`
	}

	sendTextArgs struct {
		To   string `json:"to" jsonschema:"Recipient: phone number with country code, JID, or exact chat title"`
		Text string `json:"text" jsonschema:"Message text. Write @<phone number> to mention someone in a group"`
	}

	sendFileArgs struct {
		To        string `json:"to" jsonschema:"Recipient: phone number with country code, JID, or exact chat title"`
		Path      string `json:"path" jsonschema:"Absolute path of the local file to send"`
		Caption   string `json:"caption,omitempty" jsonschema:"Caption for images, videos and documents"`
		VoiceNote bool   `json:"voice_note,omitempty" jsonschema:"Send audio as a voice note (converted with ffmpeg if not already Ogg Opus)"`
	}

	attachmentArgs struct {
		Chat      string `json:"chat,omitempty" jsonschema:"Chat JID of the message (optional, speeds up lookup)"`
		MessageID string `json:"message_id" jsonschema:"ID of the message with the attachment"`
	}
)

func (t *tools) register(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "whatsapp_status", Description: "Show whether a WhatsApp account is linked and connected."}, t.status)
	mcp.AddTool(s, &mcp.Tool{Name: "list_chats", Description: "List chats, most recently active first, with a preview of the last message."}, t.listChats)
	mcp.AddTool(s, &mcp.Tool{Name: "read_chat", Description: "Read the latest messages of one chat, oldest first."}, t.readChat)
	mcp.AddTool(s, &mcp.Tool{Name: "search_messages", Description: "Search messages across all chats by text, chat, sender, date range or attachments. Newest first."}, t.search)
	mcp.AddTool(s, &mcp.Tool{Name: "message_context", Description: "Show the conversation around a specific message."}, t.messageContext)
	mcp.AddTool(s, &mcp.Tool{Name: "find_contacts", Description: "Find people by name or phone number in the address book and chat history."}, t.findContacts)
	mcp.AddTool(s, &mcp.Tool{Name: "send_text", Description: "Send a text message. Confirm the recipient and text with the user first."}, t.sendText)
	mcp.AddTool(s, &mcp.Tool{Name: "send_file", Description: "Send a local file: image, video, audio, voice note or any document. Confirm with the user first."}, t.sendFile)
	mcp.AddTool(s, &mcp.Tool{Name: "get_attachment", Description: "Download a message's attachment and return its local path. Images are also returned for you to view."}, t.attachment)
}

func (t *tools) status(ctx context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
	return asJSON(t.svc.Status())
}

func (t *tools) listChats(ctx context.Context, _ *mcp.CallToolRequest, a listChatsArgs) (*mcp.CallToolResult, any, error) {
	chats, err := t.svc.History.Chats(ctx, a.Query, a.GroupsOnly, clamp(a.Limit, 25, 200), max(a.Offset, 0))
	if err != nil {
		return fail(err)
	}
	return asJSON(chats)
}

func (t *tools) readChat(ctx context.Context, _ *mcp.CallToolRequest, a readChatArgs) (*mcp.CallToolResult, any, error) {
	jid, err := t.svc.ResolveChat(ctx, a.Chat)
	if err != nil {
		return fail(err)
	}
	before, err := parseTime("before", a.Before)
	if err != nil {
		return fail(err)
	}
	msgs, err := t.svc.History.Messages(ctx, store.MessageFilter{ChatJID: jid, Before: before, Limit: clamp(a.Limit, 40, 300)})
	if err != nil {
		return fail(err)
	}
	header := jid
	if c, err := t.svc.History.Chat(ctx, jid); err == nil && c.Title != "" {
		header = fmt.Sprintf("%s [%s]", c.Title, jid)
	}
	return ok(header + "\n" + t.render(ctx, reverse(msgs), false, ""))
}

func (t *tools) search(ctx context.Context, _ *mcp.CallToolRequest, a searchArgs) (*mcp.CallToolResult, any, error) {
	f := store.MessageFilter{Text: a.Text, SenderJID: a.Sender, MediaOnly: a.MediaOnly, Limit: clamp(a.Limit, 30, 200), Offset: max(a.Offset, 0)}
	var err error
	if a.Chat != "" {
		if f.ChatJID, err = t.svc.ResolveChat(ctx, a.Chat); err != nil {
			return fail(err)
		}
	}
	if f.After, err = parseTime("after", a.After); err != nil {
		return fail(err)
	}
	if f.Before, err = parseTime("before", a.Before); err != nil {
		return fail(err)
	}
	msgs, err := t.svc.History.Messages(ctx, f)
	if err != nil {
		return fail(err)
	}
	return ok(t.render(ctx, msgs, f.ChatJID == "", ""))
}

func (t *tools) messageContext(ctx context.Context, _ *mcp.CallToolRequest, a contextArgs) (*mcp.CallToolResult, any, error) {
	jid, err := t.svc.ResolveChat(ctx, a.Chat)
	if err != nil {
		return fail(err)
	}
	msgs, err := t.svc.History.Around(ctx, jid, a.MessageID, clamp(a.Around, 6, 50))
	if err != nil {
		return fail(fmt.Errorf("message %s in %s: %w", a.MessageID, jid, err))
	}
	return ok(t.render(ctx, msgs, false, a.MessageID))
}

func (t *tools) findContacts(ctx context.Context, _ *mcp.CallToolRequest, a findContactsArgs) (*mcp.CallToolResult, any, error) {
	contacts, err := t.svc.FindContacts(ctx, a.Query, 50)
	if err != nil {
		return fail(err)
	}
	return asJSON(contacts)
}

func (t *tools) sendText(ctx context.Context, _ *mcp.CallToolRequest, a sendTextArgs) (*mcp.CallToolResult, any, error) {
	if a.Text == "" {
		return fail(fmt.Errorf("text is empty"))
	}
	id, err := t.svc.SendText(ctx, a.To, a.Text)
	if err != nil {
		return fail(err)
	}
	return ok("Sent (message id " + id + ")")
}

func (t *tools) sendFile(ctx context.Context, _ *mcp.CallToolRequest, a sendFileArgs) (*mcp.CallToolResult, any, error) {
	if err := fileExists(a.Path); err != nil {
		return fail(err)
	}
	id, err := t.svc.SendFile(ctx, a.To, a.Path, a.Caption, a.VoiceNote)
	if err != nil {
		return fail(err)
	}
	return ok("Sent (message id " + id + ")")
}

func (t *tools) attachment(ctx context.Context, _ *mcp.CallToolRequest, a attachmentArgs) (*mcp.CallToolResult, any, error) {
	chat := a.Chat
	if chat == "" {
		m, err := t.svc.History.FindMessage(ctx, a.MessageID)
		if err != nil {
			return fail(fmt.Errorf("message %s: %w", a.MessageID, err))
		}
		chat = m.ChatJID
	} else if jid, err := t.svc.ResolveChat(ctx, chat); err == nil {
		chat = jid
	}
	path, err := t.svc.SaveMedia(ctx, chat, a.MessageID)
	if err != nil {
		return fail(err)
	}
	res := text("Saved to " + path)
	if mt := imageMIME(path); mt != "" {
		if data, err := os.ReadFile(path); err == nil && len(data) <= inlineImageLimit {
			res.Content = append(res.Content, &mcp.ImageContent{Data: data, MIMEType: mt})
		}
	}
	return res, nil, nil
}
