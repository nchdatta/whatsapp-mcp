# Security policy

whatsapp-mcp handles private chat data and should be run with care. This project is designed for local use: your WhatsApp session, history, and media stay on your computer unless you choose to expose the HTTP connector.

## Protect your WhatsApp session

- Keep your device account secure (screen lock, OS updates, malware protection).
- Review **WhatsApp > Settings > Linked devices** regularly and remove unknown devices.
- If a linked device is lost or compromised, run `whatsapp-mcp unlink` and link again.

## Protect encrypted local data

- whatsapp-mcp encrypts local databases and media at rest; still use full-disk encryption (BitLocker, FileVault, LUKS/device encryption).
- Restrict access to your user profile and backups that include the data directory.
- Do not copy the data directory to shared locations unless it is protected.

## Protect HTTP connector tokens (`serve --http`)

- Treat the connector URL and token as secrets; anyone with them can read chats and send messages as you.
- Never post tokens or connector URLs in screenshots, issues, logs, or chat.
- If a token may be exposed, run `whatsapp-mcp token --rotate` and restart `whatsapp-mcp serve --http`.
- Prefer short-lived tunnels and trusted networking when exposing the HTTP endpoint.

## Logs and diagnostics

- `whatsapp-mcp.log` is local and should be treated as sensitive operational data.
- Redact chat identifiers, paths, tokens, and personal metadata before sharing logs publicly.
- Never share QR codes, pairing codes, or raw session files.

## Reporting a vulnerability

If GitHub private vulnerability reporting is enabled for this repository, use **Security > Report a vulnerability** so details stay private.

If private reporting is not available, contact the maintainer through this repository and share only minimal details publicly (for example, open an issue without exploit details and ask for a private follow-up channel).
