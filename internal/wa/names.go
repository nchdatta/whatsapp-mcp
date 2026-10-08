package wa

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"
)

type groupMeta struct {
	name    string
	lidMode bool // members are addressed by LID rather than phone number
	// members maps both the phone number and the LID user part of every
	// participant to the JID to mention them by
	members map[string]types.JID
	fetched time.Time
}

const groupMetaTTL = 30 * time.Minute

// group returns cached group metadata, fetching it when stale.
func (s *Service) group(ctx context.Context, jid types.JID) *groupMeta {
	if v, ok := s.groups.Load(jid.String()); ok {
		if g := v.(*groupMeta); time.Since(g.fetched) < groupMetaTTL {
			return g
		}
	}
	if !s.Client.IsLoggedIn() {
		return nil
	}
	info, err := s.Client.GetGroupInfo(ctx, jid)
	if err != nil {
		s.log.Debug().Err(err).Str("group", jid.String()).Msg("Group info unavailable")
		return nil
	}
	g := &groupMeta{name: info.Name, lidMode: info.AddressingMode == types.AddressingModeLID,
		members: map[string]types.JID{}, fetched: time.Now()}
	for _, p := range info.Participants {
		for _, id := range []types.JID{p.JID, p.PhoneNumber, p.LID} {
			if !id.IsEmpty() {
				g.members[id.User] = p.JID.ToNonAD()
			}
		}
	}
	s.groups.Store(jid.String(), g)
	return g
}

// chatTitle looks up a display title for a chat, or "" if none is known.
func (s *Service) chatTitle(ctx context.Context, chat types.JID) string {
	if chat.Server == types.GroupServer {
		if g := s.group(ctx, chat); g != nil {
			return g.name
		}
		return ""
	}
	return s.contactName(ctx, chat)
}

// contactName returns the best name the session knows for a user, or "".
func (s *Service) contactName(ctx context.Context, user types.JID) string {
	if !s.LoggedIn() || user.IsEmpty() {
		return ""
	}
	candidates := []types.JID{user.ToNonAD()}
	switch user.Server {
	case types.HiddenUserServer:
		if pn, err := s.Client.Store.LIDs.GetPNForLID(ctx, user); err == nil && !pn.IsEmpty() {
			candidates = append(candidates, pn)
		}
	case types.DefaultUserServer:
		if lid, err := s.Client.Store.LIDs.GetLIDForPN(ctx, user); err == nil && !lid.IsEmpty() {
			candidates = append(candidates, lid)
		}
	}
	for _, j := range candidates {
		c, err := s.Client.Store.Contacts.GetContact(ctx, j)
		if err != nil || !c.Found {
			continue
		}
		for _, n := range []string{c.FullName, c.FirstName, c.BusinessName, c.PushName} {
			if n != "" {
				return n
			}
		}
	}
	return ""
}

// DisplayName labels a sender for output: "Name (+phone)", "+phone" or the raw ID.
func (s *Service) DisplayName(ctx context.Context, jidStr, storedName string) string {
	if v, ok := s.names.Load(jidStr); ok {
		return v.(string)
	}
	jid, err := types.ParseJID(jidStr)
	if err != nil {
		return jidStr
	}
	name := s.contactName(ctx, jid)
	if name == "" {
		name = storedName
	}

	phone := ""
	switch jid.Server {
	case types.DefaultUserServer:
		phone = "+" + jid.User
	case types.HiddenUserServer:
		if s.LoggedIn() {
			if pn, err := s.Client.Store.LIDs.GetPNForLID(ctx, jid); err == nil && !pn.IsEmpty() {
				phone = "+" + pn.User
			}
		}
	}

	label := jid.User
	switch {
	case name != "" && phone != "":
		label = name + " (" + phone + ")"
	case name != "":
		label = name
	case phone != "":
		label = phone
	}
	s.names.Store(jidStr, label)
	return label
}

// ResolveChat maps a chat reference (JID, phone number, or contact or chat name)
// to a JID using only local data, for read operations that must work offline.
func (s *Service) ResolveChat(ctx context.Context, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("chat is empty")
	}
	if strings.Contains(ref, "@") {
		return ref, nil
	}
	if digits := nonDigits.ReplaceAllString(ref, ""); len(digits) >= 7 && len(digits) >= len(ref)/2 {
		pn := types.NewJID(digits, types.DefaultUserServer)
		if _, err := s.History.Chat(ctx, pn.String()); err == nil {
			return pn.String(), nil
		}
		if s.LoggedIn() {
			if lid, err := s.Client.Store.LIDs.GetLIDForPN(ctx, pn); err == nil && !lid.IsEmpty() {
				if _, err := s.History.Chat(ctx, lid.String()); err == nil {
					return lid.String(), nil
				}
			}
		}
		return "", fmt.Errorf("no chat with +%s in the local history", digits)
	}
	return s.chatByName(ctx, ref)
}

// chatByName finds the one chat or contact a name refers to: an exact chat
// title first, then partial chat titles and address book names.
func (s *Service) chatByName(ctx context.Context, name string) (string, error) {
	chats, err := s.History.Chats(ctx, name, nil, 20, 0)
	if err != nil {
		return "", err
	}
	var exact []string
	for _, c := range chats {
		if strings.EqualFold(c.Title, name) {
			exact = append(exact, c.JID)
		}
	}
	switch len(exact) {
	case 1:
		return exact[0], nil
	case 0:
	default:
		return "", fmt.Errorf("%d chats are named %q; pass the JID instead (list_chats shows them)", len(exact), name)
	}

	var jids, labels []string
	add := func(jid, label string) {
		if slices.Contains(jids, jid) {
			return
		}
		jids = append(jids, jid)
		labels = append(labels, fmt.Sprintf("%s [%s]", label, jid))
	}
	for _, c := range chats {
		add(c.JID, c.Title)
	}
	contacts, err := s.FindContacts(ctx, name, 20)
	if err != nil {
		return "", err
	}
	for _, c := range contacts {
		add(s.knownChat(ctx, c.JID), c.Name)
	}
	switch len(jids) {
	case 1:
		return jids[0], nil
	case 0:
		return "", fmt.Errorf("no chat or contact named %q; use a phone number or JID (list_chats and find_contacts show them)", name)
	default:
		if len(labels) > 10 {
			labels = append(labels[:10], "...")
		}
		return "", fmt.Errorf("%q matches several chats; ask the user which one and pass its JID: %s", name, strings.Join(labels, "; "))
	}
}

// knownChat maps a contact's phone number JID to the LID chat the history
// keeps it under, when that's where the conversation is.
func (s *Service) knownChat(ctx context.Context, jid string) string {
	if _, err := s.History.Chat(ctx, jid); err == nil || !s.LoggedIn() {
		return jid
	}
	pn, err := types.ParseJID(jid)
	if err != nil || pn.Server != types.DefaultUserServer {
		return jid
	}
	if lid, err := s.Client.Store.LIDs.GetLIDForPN(ctx, pn); err == nil && !lid.IsEmpty() {
		if _, err := s.History.Chat(ctx, lid.String()); err == nil {
			return lid.String()
		}
	}
	return jid
}

// Contact is a person known to the session or the local history.
type Contact struct {
	Name  string `json:"name,omitempty"`
	Phone string `json:"phone,omitempty"`
	JID   string `json:"jid"`
}

// FindContacts searches the address book synced from the phone, plus direct
// chats in the local history.
func (s *Service) FindContacts(ctx context.Context, query string, limit int) ([]Contact, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	qDigits := nonDigits.ReplaceAllString(q, "")
	seen := map[string]bool{}
	var out []Contact
	add := func(c Contact) {
		if len(out) >= limit || seen[c.JID] {
			return
		}
		hay := strings.ToLower(c.Name)
		if q == "" || strings.Contains(hay, q) || (qDigits != "" && strings.Contains(c.Phone, qDigits)) {
			seen[c.JID] = true
			out = append(out, c)
		}
	}

	if s.LoggedIn() {
		all, err := s.Client.Store.Contacts.GetAllContacts(ctx)
		if err != nil {
			return nil, err
		}
		for jid, info := range all {
			name := info.FullName
			for _, n := range []string{info.FirstName, info.BusinessName, info.PushName} {
				if name == "" {
					name = n
				}
			}
			c := Contact{Name: name, JID: jid.String()}
			if jid.Server == types.DefaultUserServer {
				c.Phone = "+" + jid.User
			}
			add(c)
		}
	}

	isGroup := false
	chats, err := s.History.Chats(ctx, "", &isGroup, 1000, 0)
	if err != nil {
		return nil, err
	}
	for _, ch := range chats {
		jid, _ := types.ParseJID(ch.JID)
		c := Contact{Name: ch.Title, JID: ch.JID}
		if jid.Server == types.DefaultUserServer {
			c.Phone = "+" + jid.User
		}
		add(c)
	}
	return out, nil
}
