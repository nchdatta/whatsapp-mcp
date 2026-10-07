package wa

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
)

// Pairing phases reported by PairingStatus.
const (
	PairIdle    = "idle"
	PairWaiting = "waiting" // QR/pairing code shown, waiting for the phone
	PairLinked  = "linked"
	PairFailed  = "failed"
)

// Pairing is a snapshot of an in-progress (or finished) background link.
type Pairing struct {
	Phase   string    `json:"phase"`
	QR      string    `json:"-"`                      // current QR payload; rotates every ~20s
	Code    string    `json:"pairing_code,omitempty"` // 8-character code when linking by phone number
	Error   string    `json:"error,omitempty"`
	Updated time.Time `json:"-"`
}

type pairing struct {
	mu      sync.Mutex
	state   Pairing
	phone   string
	running bool
	changed chan struct{} // closed and replaced on every update
}

func (p *pairing) set(update func(*Pairing)) {
	p.mu.Lock()
	update(&p.state)
	p.state.Updated = time.Now()
	if p.changed != nil {
		close(p.changed)
	}
	p.changed = make(chan struct{})
	p.mu.Unlock()
}

func (p *pairing) snapshot() (Pairing, <-chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.changed == nil {
		p.changed = make(chan struct{})
	}
	return p.state, p.changed
}

// PairingStatus reports the background link state without changing it.
func (s *Service) PairingStatus() Pairing {
	st, _ := s.pair.snapshot()
	if st.Phase == "" {
		st.Phase = PairIdle
	}
	if s.LoggedIn() {
		st.Phase = PairLinked
	}
	return st
}

// StartPairing links an account without blocking, for use while serving.
// It returns once a QR code (or, with phone, a pairing code) is ready.
// Calling it again while waiting returns the current, freshest code.
func (s *Service) StartPairing(ctx context.Context, phone string) (Pairing, error) {
	if s.LoggedIn() {
		return Pairing{Phase: PairLinked}, nil
	}

	s.pair.mu.Lock()
	alreadyRunning := s.pair.running
	switchToPhone := alreadyRunning && phone != "" && s.pair.phone == ""
	if !alreadyRunning {
		s.pair.running = true
		s.pair.phone = phone
	}
	s.pair.mu.Unlock()

	if switchToPhone {
		// QR flow is already running; add a pairing code to it
		if err := s.requestPairCode(ctx, phone); err != nil {
			return Pairing{}, err
		}
	}
	if !alreadyRunning {
		s.pair.set(func(p *Pairing) { *p = Pairing{Phase: PairWaiting} })
		// The QR channel lives as long as the pairing attempt, not this request
		qr, err := s.Client.GetQRChannel(context.Background())
		if err != nil {
			s.failPairing(err)
			s.pair.mu.Lock()
			s.pair.running = false
			s.pair.mu.Unlock()
			return Pairing{}, err
		}
		if err := s.Client.Connect(); err != nil {
			s.failPairing(err)
			s.pair.mu.Lock()
			s.pair.running = false
			s.pair.mu.Unlock()
			return Pairing{}, err
		}
		go s.runPairing(qr, phone)
	}

	// Wait until there is something to show (or the attempt ends)
	deadline := time.After(20 * time.Second)
	for {
		st, changed := s.pair.snapshot()
		if st.Phase != PairWaiting || st.Code != "" || (phone == "" && st.QR != "") {
			if st.Phase == PairFailed {
				return st, errors.New(st.Error)
			}
			return st, nil
		}
		select {
		case <-changed:
		case <-deadline:
			return st, errors.New("WhatsApp did not provide a code in time; try again")
		case <-ctx.Done():
			return st, ctx.Err()
		}
	}
}

func (s *Service) runPairing(qr <-chan whatsmeow.QRChannelItem, phone string) {
	defer func() {
		s.pair.mu.Lock()
		s.pair.running = false
		s.pair.phone = ""
		s.pair.mu.Unlock()
	}()

	codeRequested := false
	for item := range qr {
		switch {
		case item.Event == whatsmeow.QRChannelEventCode:
			s.pair.set(func(p *Pairing) { p.QR = item.Code })
			if phone != "" && !codeRequested {
				codeRequested = true
				if err := s.requestPairCode(context.Background(), phone); err != nil {
					s.failPairing(err)
					return
				}
			}
		case item == whatsmeow.QRChannelSuccess:
			s.log.Info().Msg("WhatsApp account linked")
			os.Remove(filepath.Join(s.dataDir, "link-qr.png"))
			s.pair.set(func(p *Pairing) { *p = Pairing{Phase: PairLinked} })
			return // whatsmeow reconnects with the new session by itself
		case item == whatsmeow.QRChannelTimeout:
			s.failPairing(errors.New("the code expired before it was scanned; call link_whatsapp again"))
			s.Client.Disconnect()
			return
		case item.Error != nil:
			s.failPairing(item.Error)
			s.Client.Disconnect()
			return
		default:
			s.failPairing(fmt.Errorf("linking failed: %s", item.Event))
			s.Client.Disconnect()
			return
		}
	}
}

func (s *Service) requestPairCode(ctx context.Context, phone string) error {
	digits := nonDigits.ReplaceAllString(phone, "")
	if len(digits) < 7 {
		return fmt.Errorf("%q is not a phone number with country code", phone)
	}
	code, err := s.Client.PairPhone(ctx, digits, true, whatsmeow.PairClientChrome, "Chrome (whatsapp-mcp)")
	if err != nil {
		return fmt.Errorf("requesting pairing code: %w", err)
	}
	s.pair.mu.Lock()
	s.pair.phone = digits
	s.pair.mu.Unlock()
	s.pair.set(func(p *Pairing) { p.Code = code })
	return nil
}

func (s *Service) failPairing(err error) {
	s.log.Warn().Err(err).Msg("Linking failed")
	s.pair.set(func(p *Pairing) { *p = Pairing{Phase: PairFailed, Error: err.Error()} })
}
