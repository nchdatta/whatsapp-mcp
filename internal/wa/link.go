package wa

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

// Link pairs this device with a WhatsApp account, showing a QR code on out
// (or a pairing code when phone is set), then stays connected while the
// initial history arrives.
func (s *Service) Link(ctx context.Context, phone string, out io.Writer) error {
	if s.LoggedIn() {
		return fmt.Errorf("already linked to +%s; run `whatsapp-mcp logout` first to switch accounts", s.Client.Store.ID.User)
	}

	activity := make(chan struct{}, 1)
	handler := s.Client.AddEventHandler(func(evt any) {
		if _, ok := evt.(*events.HistorySync); ok {
			select {
			case activity <- struct{}{}:
			default:
			}
		}
	})
	defer s.Client.RemoveEventHandler(handler)

	qr, err := s.Client.GetQRChannel(ctx)
	if err != nil {
		return err
	}
	if err := s.Client.Connect(); err != nil {
		return err
	}

	codeShown := false
	for item := range qr {
		switch {
		case item.Event == whatsmeow.QRChannelEventCode && phone != "":
			if codeShown {
				continue
			}
			code, err := s.Client.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome (whatsapp-mcp)")
			if err != nil {
				return fmt.Errorf("requesting pairing code: %w", err)
			}
			codeShown = true
			fmt.Fprintf(out, "\nOn your phone: WhatsApp > Linked devices > Link a device > Link with phone number instead\nCode: %s\n\n", code)
		case item.Event == whatsmeow.QRChannelEventCode:
			fmt.Fprintln(out, "\nScan with WhatsApp > Linked devices > Link a device:")
			qrterminal.GenerateHalfBlock(item.Code, qrterminal.L, out)
		case item == whatsmeow.QRChannelSuccess:
			fmt.Fprintln(out, "\nLinked. Receiving recent history, keep this open...")
			return s.waitForQuiet(ctx, activity)
		case item == whatsmeow.QRChannelTimeout:
			return errors.New("timed out before the code was scanned; run login again")
		case item.Error != nil:
			return fmt.Errorf("linking failed: %w", item.Error)
		default:
			return fmt.Errorf("linking failed: %s", item.Event)
		}
	}
	return errors.New("linking ended unexpectedly")
}

// waitForQuiet returns once history sync has been idle for a while.
func (s *Service) waitForQuiet(ctx context.Context, activity <-chan struct{}) error {
	const idle = 20 * time.Second
	timer := time.NewTimer(idle)
	defer timer.Stop()
	hardStop := time.After(5 * time.Minute)
	for {
		select {
		case <-activity:
			timer.Reset(idle)
		case <-timer.C:
			return nil
		case <-hardStop:
			return nil
		case <-ctx.Done():
			return nil // Ctrl+C after linking is fine
		}
	}
}

// Unlink logs this device out of WhatsApp and deletes the local session.
func (s *Service) Unlink(ctx context.Context) error {
	if !s.LoggedIn() {
		return errors.New("no account is linked")
	}
	if err := s.Client.Connect(); err == nil {
		if err := s.Client.Logout(ctx); err == nil {
			return nil
		}
	}
	// Offline or rejected: forget the session locally anyway
	return s.Client.Store.Delete(ctx)
}
