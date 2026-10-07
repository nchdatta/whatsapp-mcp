package wa

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Away is the fallback auto-reply: when an incoming message gets no reply
// (from Claude or from you) within Delay, whatsapp-mcp sends Message itself.
// It is turned on only by the user, with `whatsapp-mcp away on`.
type Away struct {
	Enabled  bool          `json:"enabled"`
	Message  string        `json:"message"`
	Delay    time.Duration `json:"delay"`
	Cooldown time.Duration `json:"cooldown"` // at most one away reply per chat in this time
	Groups   bool          `json:"groups"`
}

// DefaultAway is used for fields that were never set.
var DefaultAway = Away{
	Message:  "Hi! I'm not available right now. I'll get back to you soon.",
	Delay:    time.Minute,
	Cooldown: 3 * time.Hour,
}

const awayKey = "away"

// awayWindow limits away replies to recent messages, so turning it on doesn't
// answer an old backlog.
const awayWindow = 30 * time.Minute

// AwaySettings returns the current away settings.
func (s *Service) AwaySettings(ctx context.Context) (Away, error) {
	a := DefaultAway
	v, err := s.History.Setting(ctx, awayKey)
	if err != nil || v == "" {
		return a, err
	}
	err = json.Unmarshal([]byte(v), &a)
	return a, err
}

// SetAway stores away settings; serve picks them up within a few seconds.
func (s *Service) SetAway(ctx context.Context, a Away) error {
	if a.Enabled && a.Message == "" {
		return errors.New("the away message is empty")
	}
	if a.Delay < 10*time.Second {
		return errors.New("the delay must be at least 10 seconds")
	}
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return s.History.SetSetting(ctx, awayKey, string(b))
}

// RunAway sends away replies while ctx lives. serve runs it.
func (s *Service) RunAway(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		a, err := s.AwaySettings(ctx)
		if err != nil || !a.Enabled || !s.Client.IsConnected() || !s.Client.IsLoggedIn() {
			continue
		}
		now := time.Now()
		chats, err := s.History.Unanswered(ctx, now.Add(-awayWindow), now.Add(-a.Delay), now.Add(-a.Cooldown), a.Groups)
		if err != nil {
			s.log.Warn().Err(err).Msg("Checking for unanswered messages failed")
			continue
		}
		for _, chat := range chats {
			// Mark first: a failed send must not turn into a retry loop
			if err := s.History.MarkAwaySent(ctx, chat, now); err != nil {
				continue
			}
			if _, err := s.SendText(ctx, chat, a.Message); err != nil {
				s.log.Warn().Err(err).Str("chat", chat).Msg("Sending away reply failed")
				continue
			}
			s.log.Info().Str("chat", chat).Msg("Sent away reply")
		}
	}
}
