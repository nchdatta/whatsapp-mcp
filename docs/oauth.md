# OAuth for remote access (planned)

Status: design notes, not implemented.

## Why

To use whatsapp-mcp from claude.ai (web and mobile), it has to run as a remote MCP server reachable over HTTPS. claude.ai custom connectors take a URL plus, optionally, OAuth. They don't support custom headers, so without OAuth the only option is a secret in the URL. That is weak: URLs end up in logs, browser history and screenshots, and the secret can't be scoped, expired or revoked per client.

OAuth fixes this. claude.ai sends the user through a login/consent page, receives a short-lived access token, and sends it as `Authorization: Bearer` on every request.

## What the MCP spec requires

The MCP authorization spec (2025-06-18 and later) builds on OAuth 2.1:

- **Protected resource metadata** (RFC 9728). The server answers unauthenticated requests with `401` and a `WWW-Authenticate: Bearer resource_metadata="https://HOST/.well-known/oauth-protected-resource"` header. That document names the authorization server.
- **Authorization server metadata** (RFC 8414) at `/.well-known/oauth-authorization-server`, listing the endpoints below.
- **Dynamic client registration** (RFC 7591) at `/register`. claude.ai registers itself as a client automatically.
- **Authorization code flow with PKCE (S256)**: `/authorize` → user approves → redirect with `code` → `/token` exchanges it for an access token (and a refresh token).
- **Audience binding** (RFC 8707): tokens are issued for this server's resource URL. The server rejects tokens issued for anything else.

claude.ai's callback URL is `https://claude.ai/api/mcp/auth_callback`. Registration should allow only known redirect URIs.

## Proposed design

Embed a minimal authorization server in the binary, so there's still nothing extra to install.

| Piece | Plan |
|---|---|
| Owner login | The `/authorize` page asks for a one-time approval code printed in the terminal running `serve` (or shown by `whatsapp-mcp approve`). There are no passwords to store. |
| Clients | Registered clients are stored in `history.db` (new `oauth_client` table). Unknown redirect URIs are rejected. |
| Tokens | Opaque random access tokens (1 h) and refresh tokens (30 days, rotated on use). Only their SHA-256 hashes are stored in an `oauth_token` table. |
| Revocation | `whatsapp-mcp clients` lists connected clients; `whatsapp-mcp revoke <id>` removes one and its tokens. |
| Scopes | Start with one scope, `whatsapp`. Possible later split: `read` vs `send`, so a connector can be read-only. |
| Transport | HTTPS only, terminated by a tunnel (Cloudflare Tunnel) or a reverse proxy. The issuer URL comes from a `--public-url` flag, because the server can't infer it from behind a proxy. |

Alternative: delegate to an external identity provider (Auth0, Cloudflare Access, Keycloak) and only validate JWTs here. That's less code, but it adds an account and setup to every install, so it's an option rather than the default.

## Libraries

- `github.com/modelcontextprotocol/go-sdk/auth` provides bearer-token middleware (`RequireBearerToken`) and protected-resource metadata helpers.
- The authorization server endpoints are small enough to write directly. `github.com/ory/fosite` is an option if they grow.

## Security checklist

- PKCE required (S256 only); `plain` rejected.
- Exact redirect URI match; no wildcards.
- Authorization codes are single use and expire in 60 s.
- Constant-time comparison of all secrets; only hashes are stored.
- Rate-limit `/authorize` approval attempts.
- Log every client registration, approval and revocation to `whatsapp-mcp.log`.
- Sending tools still rely on the model asking the user to confirm; OAuth controls *who* connects, not *what* they do.

## Open questions

- Should approval be required again for each new client, or only the first?
- Should there be a read-only mode for remote access by default, with sending opt-in?
- How long should refresh tokens live for a device that's offline for long periods?
