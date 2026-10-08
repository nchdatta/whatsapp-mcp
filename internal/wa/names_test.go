package wa

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nchdatta/whatsapp-mcp/internal/secret"
	"github.com/nchdatta/whatsapp-mcp/internal/store"
)

func TestResolveChatByName(t *testing.T) {
	ctx := context.Background()
	keys := secret.ForTest()
	history, err := store.Open(ctx, t.TempDir(), keys.DBHex)
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	s := &Service{History: history}

	now := time.Now()
	for jid, title := range map[string]string{
		"111@s.whatsapp.net": "Rahim Uddin",
		"222@s.whatsapp.net": "Karim",
		"333@s.whatsapp.net": "Karim Hasan",
		"444@g.us":           "Family",
	} {
		if err := history.PutChat(ctx, jid, title, strings.HasSuffix(jid, "@g.us"), now); err != nil {
			t.Fatal(err)
		}
	}

	for ref, want := range map[string]string{
		"rahim":  "111@s.whatsapp.net", // one partial match
		"Karim":  "222@s.whatsapp.net", // exact title wins over partial
		"family": "444@g.us",
	} {
		if got, err := s.ResolveChat(ctx, ref); err != nil || got != want {
			t.Errorf("ResolveChat(%q) = %q, %v; want %q", ref, got, err, want)
		}
	}

	if _, err := s.ResolveChat(ctx, "rim"); err == nil || !strings.Contains(err.Error(), "several") {
		t.Errorf("ambiguous name: got %v", err)
	}
	if _, err := s.ResolveChat(ctx, "nobody"); err == nil {
		t.Error("unknown name resolved")
	}
}
