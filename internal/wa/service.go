// Package wa wraps the WhatsApp client: it keeps the local history in sync
// and exposes the operations the MCP tools need.
package wa

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/nchdatta/whatsapp-mcp/internal/secret"
	"github.com/nchdatta/whatsapp-mcp/internal/store"
)

// Service owns the WhatsApp connection and the local history.
type Service struct {
	Client  *whatsmeow.Client
	History *store.Store
	Keys    *secret.Keys

	dataDir   string
	mediaDir  string
	log       zerolog.Logger
	container *sqlstore.Container

	viewMu  sync.Mutex
	viewDir string // decrypted copies of attachments, removed on Close

	names  sync.Map // user JID string -> display name
	groups sync.Map // group JID string -> *groupMeta
	pair   pairing
}

// AutoSaveLimit is the largest incoming attachment saved automatically.
const AutoSaveLimit = 100 << 20

// Open loads the session and history from dataDir. It does not connect.
// Databases and attachments from older, unencrypted versions are encrypted
// on the way.
func Open(ctx context.Context, dataDir string, log zerolog.Logger) (*Service, error) {
	keys, err := secret.Load(dataDir)
	if err != nil {
		return nil, err
	}
	sessionPath := filepath.Join(dataDir, "session.db")
	for _, path := range []string{sessionPath, filepath.Join(dataDir, store.FileName)} {
		if store.IsPlaintext(path) {
			log.Info().Str("file", filepath.Base(path)).Msg("Encrypting database")
			if err := store.Encrypt(ctx, path, keys.DBHex); err != nil {
				return nil, err
			}
		}
	}

	container, err := sqlstore.New(ctx, store.Driver, store.DSN(sessionPath, keys.DBHex), waLog.Zerolog(log.With().Str("module", "session").Logger()))
	if err != nil {
		return nil, fmt.Errorf("open session: %w", err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		container.Close()
		return nil, fmt.Errorf("load device: %w", err)
	}
	history, err := store.Open(ctx, dataDir, keys.DBHex)
	if err != nil {
		container.Close()
		return nil, fmt.Errorf("open history: %w", err)
	}

	// The name shown under Linked devices on the phone
	waStore.DeviceProps.Os = proto.String("whatsapp-mcp")
	waStore.DeviceProps.PlatformType = waCompanionReg.DeviceProps_CHROME.Enum()

	s := &Service{
		Client:    whatsmeow.NewClient(device, quietLog{waLog.Zerolog(log.With().Str("module", "whatsapp").Logger())}),
		History:   history,
		Keys:      keys,
		dataDir:   dataDir,
		mediaDir:  filepath.Join(dataDir, "media"),
		log:       log,
		container: container,
	}
	s.Client.AddEventHandler(s.onEvent)
	go s.encryptOldMedia(context.Background())

	return s, nil
}

func (s *Service) Close() {
	s.Client.Disconnect()
	s.History.Close()
	s.container.Close()
	s.removeViewDir()
}

func (s *Service) DataDir() string { return s.dataDir }

// LoggedIn reports whether a WhatsApp account is linked.
func (s *Service) LoggedIn() bool { return s.Client.Store.ID != nil }

var ErrNotLoggedIn = errors.New("no WhatsApp account is linked yet: call the link_whatsapp tool (or run `whatsapp-mcp login` in a terminal)")

// Online waits briefly for the connection, for operations that need the network.
func (s *Service) Online(ctx context.Context) error {
	if !s.LoggedIn() {
		return ErrNotLoggedIn
	}
	deadline := time.Now().Add(15 * time.Second)
	for !s.Client.IsLoggedIn() {
		if time.Now().After(deadline) {
			return fmt.Errorf("not connected to WhatsApp yet; details in %s", filepath.Join(s.dataDir, "whatsapp-mcp.log"))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return nil
}

// Status summarises the connection for humans.
func (s *Service) Status() map[string]any {
	st := map[string]any{
		"linked":    s.LoggedIn(),
		"connected": s.Client.IsLoggedIn(),
		"data_dir":  s.dataDir,
	}
	if p := s.PairingStatus(); p.Phase != PairIdle && !s.LoggedIn() {
		st["linking"] = p
	}
	if id := s.Client.Store.ID; id != nil {
		st["account"] = "+" + id.User
		if name := s.Client.Store.PushName; name != "" {
			st["name"] = name
		}
	}
	return st
}
