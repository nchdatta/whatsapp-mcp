package wa

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mdp/qrterminal/v3"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/nchdatta/whatsapp-mcp/internal/secret"
)

// Link pairs this device with a WhatsApp account, showing a QR code on out
// (or a pairing code when phone is set), then stays connected while the
// initial history arrives.
func (s *Service) Link(ctx context.Context, phone string, out io.Writer) error {
	phone = nonDigits.ReplaceAllString(phone, "")
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
			fmt.Fprintln(out, "\nScan with WhatsApp > Settings > Linked devices > Link a device:")
			qrterminal.GenerateHalfBlock(item.Code, qrterminal.L, out)
			// Some consoles draw the block characters badly; offer an image too
			if png, err := qrcode.Encode(item.Code, qrcode.Medium, 384); err == nil {
				qrFile := filepath.Join(s.dataDir, "link-qr.png")
				if os.WriteFile(qrFile, png, 0o600) == nil {
					fmt.Fprintln(out, "Hard to scan? Open", qrFile)
				}
			}
		case item == whatsmeow.QRChannelSuccess:
			os.Remove(filepath.Join(s.dataDir, "link-qr.png"))
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

// ErrNotLinked is returned by Unlink when there is nothing to unlink.
var ErrNotLinked = errors.New("no WhatsApp account is linked")

// Unlink removes this device from the WhatsApp account (it disappears from
// Linked devices on the phone) and deletes the local session. It reports
// whether WhatsApp confirmed the removal; when offline, only the local
// session is deleted and the phone may still list the device.
func (s *Service) Unlink(ctx context.Context) (remote bool, err error) {
	if !s.LoggedIn() {
		return false, ErrNotLinked
	}
	if !s.Client.IsConnected() {
		if err := s.Client.Connect(); err != nil {
			s.log.Warn().Err(err).Msg("Offline; removing the session locally only")
		}
	}
	if s.Client.IsConnected() {
		if err := s.Client.Logout(ctx); err == nil {
			s.forget()
			return true, nil
		} else {
			s.log.Warn().Err(err).Msg("WhatsApp rejected the logout; removing the session locally")
		}
	}
	s.Client.Disconnect()
	if err := s.Client.Store.Delete(ctx); err != nil {
		return false, err
	}
	s.forget()
	return false, nil
}

// forget drops cached names from the previous account.
func (s *Service) forget() {
	s.names.Clear()
	s.groups.Clear()
	s.pair.set(func(p *Pairing) { *p = Pairing{Phase: PairIdle} })
}

// DeleteLocalData removes the session, message history and saved attachments from
// the data directory. Call it only after Close.
func DeleteLocalData(dataDir string) error {
	var errs []error
	for _, name := range []string{"history.db", "history.db-wal", "history.db-shm",
		"session.db", "session.db-wal", "session.db-shm", "link-qr.png", "http-token", "media"} {
		if err := os.RemoveAll(filepath.Join(dataDir, name)); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		// Only once everything it protects is gone
		errs = append(errs, secret.Delete(dataDir))
	}
	return errors.Join(errs...)
}
