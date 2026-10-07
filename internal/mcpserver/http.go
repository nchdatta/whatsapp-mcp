package mcpserver

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nchdatta/whatsapp-mcp/internal/wa"
)

// RunHTTP serves MCP over Streamable HTTP on addr until ctx ends. Every
// request must carry token, either in the path (/mcp/<token>, for clients
// such as claude.ai that only take a URL) or as "Authorization: Bearer <token>"
// on /mcp.
func RunHTTP(ctx context.Context, svc *wa.Service, version, addr, token string) error {
	if len(token) < 32 {
		return errors.New("refusing to serve HTTP without a strong token")
	}
	server := newServer(svc, version)
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			SessionTimeout: 30 * time.Minute,
			// Tunnels and reverse proxies forward public Host headers to
			// localhost; the token is what protects the endpoint
			DisableLocalhostProtection: true,
		})

	srv := &http.Server{
		Addr:              addr,
		Handler:           authorize(token, mcpHandler),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	}
}

// authorize lets through only requests that present the token, and rewrites
// /mcp/<token> to /mcp so the token never reaches the MCP handler.
func authorize(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got string
		switch {
		case strings.HasPrefix(r.URL.Path, "/mcp/"):
			got = strings.TrimPrefix(r.URL.Path, "/mcp/")
		case r.URL.Path == "/mcp":
			got, _ = strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		default:
			http.NotFound(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path, r2.URL.RawPath = "/mcp", ""
		r2.Header.Del("Authorization")
		next.ServeHTTP(w, r2)
	})
}
