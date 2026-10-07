# whatsapp-mcp

Connect Claude (or any [MCP](https://modelcontextprotocol.io) client) to your personal WhatsApp account. Read and search your chats, look at photos people send you, and send messages, files and voice notes.

It's one self-contained binary. Your messages stay on your computer, in a local SQLite database, and only reach the model when it calls a tool.

> **Use at your own risk.** This links as a WhatsApp Web device through the unofficial [whatsmeow](https://github.com/tulir/whatsmeow) library. Automated or bulk messaging can get an account banned.
>
> **Prompt injection:** anything in your chats is text the model reads. A malicious message could try to make the model leak data or send messages. Keep tool approval on for sending.

## Quick start

1. **Get the binary.** Download it from the [releases page](https://github.com/nchdatta/whatsapp-mcp/releases), install it with `go install github.com/nchdatta/whatsapp-mcp/cmd/whatsapp-mcp@latest`, or [build it](#build).
2. **Link your account.** Run this once, in a terminal:
   ```sh
   whatsapp-mcp login
   ```
   Scan the QR code with WhatsApp (**Settings > Linked devices > Link a device**). If you can't scan, use a pairing code instead:
   ```sh
   whatsapp-mcp login --phone 15551234567
   ```
   Keep the terminal open until it says **Done**. Recent history syncs during that time.
3. **Add it to Claude.**

   Claude Code:
   ```sh
   claude mcp add whatsapp -- /path/to/whatsapp-mcp serve
   ```
   Claude Desktop (`claude_desktop_config.json`):
   ```json
   {
     "mcpServers": {
       "whatsapp": { "command": "C:\\path\\to\\whatsapp-mcp.exe", "args": ["serve"] }
     }
   }
   ```
4. Restart Claude and ask something like *"What did the family group talk about today?"*

Optional: install [ffmpeg](https://ffmpeg.org/download.html) to send any audio file as a voice note. Without it, only `.ogg`/`.opus` files can be sent as voice notes; anything can still be sent as a regular file.

## Tools

| Tool | What it does |
|---|---|
| `whatsapp_status` | Shows whether an account is linked and connected |
| `list_chats` | Lists chats by recent activity, with a last-message preview |
| `read_chat` | Shows the latest messages of a chat. Pass `before` to page back through older messages |
| `search_messages` | Searches by text, chat, sender, date range or attachments |
| `message_context` | Shows the conversation around one message |
| `find_contacts` | Finds people by name or number |
| `send_text` | Sends a message. Write `@<number>` to mention someone in a group |
| `send_file` | Sends an image, video, audio, document, or a voice note (`voice_note: true`) |
| `get_attachment` | Downloads an attachment. Images are shown to the model directly |

Chats can be referred to by JID, by phone number, or by exact chat title.

## How it works

- `whatsapp-mcp serve` is started by your MCP client and talks MCP over stdio. While it runs, it stays connected to WhatsApp and records new messages.
- Incoming attachments up to 100 MB are saved automatically. Larger ones download when you ask for them.
- Messages that arrive while the client is closed are filled in by WhatsApp's history sync on the next start. That is usually complete, but not guaranteed.
- Run only one `serve` per data directory. If two processes share a session, WhatsApp disconnects one of them.

### Commands

| Command | |
|---|---|
| `whatsapp-mcp login [--phone N]` | Link an account |
| `whatsapp-mcp serve` | MCP server (your client runs this) |
| `whatsapp-mcp status` | Show the linked account |
| `whatsapp-mcp logout` | Unlink and delete the session (history is kept) |
| `whatsapp-mcp version` | Print the version |

All commands accept `--data DIR` (or `WHATSAPP_MCP_DATA`).

### Data directory

| OS | Default |
|---|---|
| Windows | `%APPDATA%\whatsapp-mcp` |
| macOS | `~/Library/Application Support/whatsapp-mcp` |
| Linux | `~/.config/whatsapp-mcp` |

| File | Contents |
|---|---|
| `session.db` | Your WhatsApp device keys. **Anyone with this file can use your account.** |
| `history.db` | Chats and messages |
| `media/` | Saved attachments |
| `whatsapp-mcp.log` | Logs. Check here first when something goes wrong |

### Watching messages from a script

[`scripts/wa_watch.py`](scripts/wa_watch.py) prints each incoming message, including the saved attachment path, while `serve` is running. It uses only the Python standard library:

```sh
python scripts/wa_watch.py
```

`history.db` is plain SQLite. `message.seq` increases with every new message, so it is easy to tail from your own tools too.

## Project layout

```
cmd/whatsapp-mcp/    CLI entry point (login, serve, status, logout)
internal/config/     data directory and logging
internal/store/      SQLite schema and queries
internal/wa/         WhatsApp connection: sync, names, sending, attachments, linking
internal/mcpserver/  MCP tool definitions
internal/audio/      voice notes: ffmpeg conversion, duration and waveform
scripts/             optional helper scripts
```

## Build

Requires Go 1.26 or newer. No C compiler is needed: SQLite is pure Go.

```sh
go build -o whatsapp-mcp ./cmd/whatsapp-mcp
go test ./...
```

Release builds for all platforms:

```sh
for target in windows/amd64 windows/arm64 darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do
  os=${target%/*}; arch=${target#*/}; ext=; [ "$os" = windows ] && ext=.exe
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=1.0.0" \
    -o dist/whatsapp-mcp-$os-$arch$ext ./cmd/whatsapp-mcp
done
```

The binaries are unsigned, so Windows SmartScreen and macOS Gatekeeper will ask for confirmation on first run.

## Troubleshooting

- **"no WhatsApp account is linked"**: run `whatsapp-mcp login`, then restart your MCP client.
- **Unlinked after a while**: WhatsApp drops linked devices that stay offline for about two weeks. Run `login` again.
- **Breaks after a WhatsApp update**: run `go get go.mau.fi/whatsmeow@latest`, then rebuild.
- **Anything else**: check `whatsapp-mcp.log` in the data directory.

## License

[MIT](LICENSE)
