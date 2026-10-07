package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/skip2/go-qrcode"

	"github.com/nchdatta/whatsapp-mcp/internal/store"
	"github.com/nchdatta/whatsapp-mcp/internal/wa"
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
		Text string `json:"text" jsonschema:"Message text. Write @<phone number>, or the [tag: @...] value from wait_for_messages, to mention someone in a group"`
	}

	sendFileArgs struct {
		To        string `json:"to" jsonschema:"Recipient: phone number with country code, JID, or exact chat title"`
		Path      string `json:"path" jsonschema:"Absolute path of the local file to send"`
		Caption   string `json:"caption,omitempty" jsonschema:"Caption for images, videos and documents"`
		VoiceNote bool   `json:"voice_note,omitempty" jsonschema:"Send audio as a voice note (converted with ffmpeg if not already Ogg Opus)"`
	}

	linkArgs struct {
		Phone string `json:"phone,omitempty" jsonschema:"Link with an 8-character pairing code for this phone number (with country code) instead of a QR code"`
	}

	unlinkArgs struct {
		Confirm bool `json:"confirm,omitempty" jsonschema:"Must be true. Only set it after the user explicitly confirmed they want to unlink"`
	}

	attachmentArgs struct {
		Chat      string `json:"chat,omitempty" jsonschema:"Chat JID of the message (optional, speeds up lookup)"`
		MessageID string `json:"message_id" jsonschema:"ID of the message with the attachment"`
	}
)

func (t *tools) register(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "whatsapp_status", Description: "Show whether a WhatsApp account is linked and connected."}, t.status)
	mcp.AddTool(s, &mcp.Tool{Name: "link_whatsapp", Description: "Link a WhatsApp account when none is linked. Returns a QR code image to scan in WhatsApp (Linked devices > Link a device), or a pairing code when phone is given. Codes expire in about 20 seconds; call again for a fresh one, then whatsapp_status to confirm."}, t.link)
	mcp.AddTool(s, &mcp.Tool{Name: "unlink_whatsapp", Description: "Unlink the WhatsApp account from this computer (removes it from Linked devices on the phone). Local message history is kept. Ask the user to confirm first."}, t.unlink)
	mcp.AddTool(s, &mcp.Tool{Name: "list_chats", Description: "List chats, most recently active first, with a preview of the last message."}, t.listChats)
	mcp.AddTool(s, &mcp.Tool{Name: "read_chat", Description: "Read the latest messages of one chat, oldest first."}, t.readChat)
	mcp.AddTool(s, &mcp.Tool{Name: "search_messages", Description: "Search messages across all chats by text, chat, sender, date range or attachments. Newest first."}, t.search)
	mcp.AddTool(s, &mcp.Tool{Name: "message_context", Description: "Show the conversation around a specific message."}, t.messageContext)
	mcp.AddTool(s, &mcp.Tool{Name: "find_contacts", Description: "Find people by name or phone number in the address book and chat history."}, t.findContacts)
	mcp.AddTool(s, &mcp.Tool{Name: "wait_for_messages", Description: "Wait for new incoming messages (checks every 3 seconds) and return them with their chat JIDs. Returns a cursor; call again with it to keep watching, e.g. to act as the user's assistant and reply as messages arrive."}, t.waitForMessages)
	mcp.AddTool(s, &mcp.Tool{Name: "send_text", Description: "Send a text message. Confirm the recipient and text with the user first, unless they asked you to reply to messages automatically."}, t.sendText)
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

func (t *tools) link(ctx context.Context, _ *mcp.CallToolRequest, a linkArgs) (*mcp.CallToolResult, any, error) {
	if t.svc.LoggedIn() {
		return asJSON(t.svc.Status())
	}
	p, err := t.svc.StartPairing(ctx, a.Phone)
	if err != nil {
		return fail(err)
	}
	switch {
	case p.Phase == wa.PairLinked:
		return ok("Linked. WhatsApp is connecting; recent history will sync over the next minute.")
	case p.Code != "":
		return ok(fmt.Sprintf("Pairing code: %s\n\nOn the phone: WhatsApp > Settings > Linked devices > Link a device > Link with phone number instead, then enter the code. Call whatsapp_status afterwards to confirm.", p.Code))
	case p.QR != "":
		png, err := qrcode.Encode(p.QR, qrcode.Medium, 384)
		if err != nil {
			return fail(err)
		}
		path := filepath.Join(t.svc.DataDir(), "link-qr.png")
		if err := os.WriteFile(path, png, 0o600); err != nil {
			return fail(err)
		}
		res := text("Show this QR code to the user (also saved at " + path + " if they can't see the image). On their phone: WhatsApp > Settings > Linked devices > Link a device, then scan it. It expires in about 20 seconds; call link_whatsapp again for a fresh one, and whatsapp_status to confirm once scanned.")
		res.Content = append(res.Content, &mcp.ImageContent{Data: png, MIMEType: "image/png"})
		return res, nil, nil
	}
	return fail(fmt.Errorf("linking is in state %q; try again", p.Phase))
}

func (t *tools) unlink(ctx context.Context, _ *mcp.CallToolRequest, a unlinkArgs) (*mcp.CallToolResult, any, error) {
	if !t.svc.LoggedIn() {
		return ok("No WhatsApp account is linked.")
	}
	if !a.Confirm {
		return fail(fmt.Errorf("unlinking +%s needs the user's confirmation; ask them, then call again with confirm: true", t.svc.Client.Store.ID.User))
	}
	remote, err := t.svc.Unlink(ctx)
	if err != nil {
		return fail(err)
	}
	if !remote {
		return ok("Removed the local session, but WhatsApp couldn't be reached. Tell the user to remove this computer under WhatsApp > Settings > Linked devices if it's still listed. Local message history is kept.")
	}
	return ok("Unlinked. This computer is no longer a linked device. Local message history is kept; link_whatsapp links an account again.")
}
