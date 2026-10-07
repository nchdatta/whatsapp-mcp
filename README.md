# whatsapp-mcp

[![CI](https://github.com/nchdatta/whatsapp-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/nchdatta/whatsapp-mcp/actions/workflows/ci.yml)

Use your WhatsApp from **Claude Desktop**. Read and search your chats, look at photos people send you, and send messages, files and voice notes. It works with any other [MCP](https://modelcontextprotocol.io) client too.

It's one self-contained binary. Your messages stay on your computer, in a local SQLite database, and only reach Claude when it calls a tool.

> **Use at your own risk.** This links as a WhatsApp Web device through the unofficial [whatsmeow](https://github.com/tulir/whatsmeow) library. Automated or bulk messaging can get an account banned.
>
> **Prompt injection:** anything in your chats is text the model reads. A malicious message could try to make the model leak data or send messages. Keep tool approval on for sending.

## Quick start

**1. Install**

| Platform | Command |
|---|---|
| Windows (PowerShell) | `irm https://raw.githubusercontent.com/nchdatta/whatsapp-mcp/main/scripts/install.ps1 \| iex` |
| macOS / Linux | `curl -fsSL https://raw.githubusercontent.com/nchdatta/whatsapp-mcp/main/scripts/install.sh \| sh` |

The installer downloads the right build for your system from the [latest release](https://github.com/nchdatta/whatsapp-mcp/releases/latest), verifies its checksum, and runs `whatsapp-mcp install`. That command:

- copies the binary to a per-user folder: `%LOCALAPPDATA%\Programs\whatsapp-mcp\` on Windows, `~/.local/bin/` on macOS and Linux,
- adds it to Claude Desktop's `claude_desktop_config.json`. It finds the file for regular and Microsoft Store installs, keeps your other settings, and saves a `.bak` copy first.

On Windows you can also download `whatsapp-mcp-windows-amd64.exe` (or `-arm64`) from the release and **double-click it**. Builds exist for Windows, macOS and Linux on both amd64 and arm64.

To update, run the same command again; quit Claude Desktop first. To pin a version, set `WHATSAPP_MCP_VERSION=v1.2.3` before running the installer.

<details>
<summary>From source (requires Go)</summary>

```sh
# Windows
powershell -ExecutionPolicy Bypass -File scripts\setup.ps1
# macOS / Linux
sh scripts/setup.sh
```

Both build `bin/whatsapp-mcp` and run `install`.
</details>

**2. Link your WhatsApp**

At the end of the install, the installer asks:

```
Link your WhatsApp now?
  Press Enter to show a QR code, type your phone number (with country code) for a pairing code, or type n to skip:
```

- **QR code:** press Enter and scan it on your phone under **WhatsApp > Settings > Linked devices > Link a device**. If the terminal draws it badly, open the `link-qr.png` it points to.
- **Pairing code:** type your number, then enter the 8-character code on your phone under **Link with phone number instead**.

Keep the window open until it says it's done, so recent history can sync.

Skipped it? Run `whatsapp-mcp login` later, or ask Claude in Claude Desktop to *"link my WhatsApp"*. It shows the QR code in the chat.

**3. Restart Claude Desktop**

Quit it completely, including from the system tray (Windows) or menu bar (macOS), then start it again. `whatsapp` should appear under **Settings > Developer**. Try *"What did the family group talk about today?"*

Optional: install [ffmpeg](https://ffmpeg.org/download.html) to send any audio file as a voice note. Without it, only `.ogg`/`.opus` files can be sent as voice notes; anything can still be sent as a regular file.

### Other MCP clients

Run `whatsapp-mcp serve` over stdio. For example, in Claude Code:

```sh
claude mcp add --scope user whatsapp -- /path/to/whatsapp-mcp serve
```

Only use one client at a time with the same data directory; see [How it works](#how-it-works).

## Tools

| Tool | What it does |
|---|---|
| `whatsapp_status` | Shows whether an account is linked and connected |
| `link_whatsapp` | Links an account: returns a QR code image, or a pairing code when given `phone` |
| `unlink_whatsapp` | Unlinks the account, after you confirm. Local history is kept |
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

- Claude Desktop starts `whatsapp-mcp serve` when it launches, and stops it when it quits. It talks MCP over stdio. While it runs, it stays connected to WhatsApp and records new messages.
- Incoming attachments up to 100 MB are saved automatically. Larger ones download when you ask for them.
- Messages that arrive while Claude Desktop is closed are filled in by WhatsApp's history sync on the next start. That is usually complete, but not guaranteed.
- Run only one `serve` per data directory. If two apps share a session (for example Claude Desktop and Claude Code), WhatsApp disconnects one of them. Give the second app its own folder with `--data`.

### Commands

| Command | |
|---|---|
| `whatsapp-mcp install [--here] [--data DIR]` | Copy to the per-user programs folder and add to Claude Desktop (`--here`: register in place) |
| `whatsapp-mcp uninstall` | Remove from Claude Desktop, optionally unlinking and deleting local data |
| `whatsapp-mcp login [--phone N]` | Link an account from a terminal |
| `whatsapp-mcp status` | Show the linked account |
| `whatsapp-mcp unlink [--delete-data]` | Remove this computer from your WhatsApp linked devices; `--delete-data` also deletes local messages and attachments (alias: `logout`) |
| `whatsapp-mcp serve` | The MCP server; Claude Desktop runs this |
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
cmd/whatsapp-mcp/    CLI entry point (install, serve, login, status, logout)
internal/config/     data directory and logging
internal/desktop/    Claude Desktop config (install / uninstall)
internal/store/      SQLite schema and queries
internal/wa/         WhatsApp connection: sync, names, sending, attachments, linking
internal/mcpserver/  MCP tool definitions
internal/audio/      voice notes: ffmpeg conversion, duration and waveform
scripts/             install.ps1 / install.sh (release installers), setup.ps1 / setup.sh (from source), wa_watch.py
```

## Build

Requires Go 1.26 or newer. No C compiler is needed: SQLite is pure Go.

```sh
go build -o whatsapp-mcp ./cmd/whatsapp-mcp
go test ./...
```

Releases: push a tag such as `v1.2.3`. [`.github/workflows/release.yml`](.github/workflows/release.yml) tests the code, builds all six platform binaries with the version embedded, and publishes them with `SHA256SUMS` as a GitHub Release.

The binaries are unsigned, so Windows SmartScreen and macOS Gatekeeper will ask for confirmation on first run.

## Troubleshooting

- **`'whatsapp-mcp' is not recognized`** (Windows): open a *new* terminal after installing; already-open ones keep their old PATH. Or run it by its full path: `%LOCALAPPDATA%Programswhatsapp-mcpwhatsapp-mcp.exe`.
- **Unlink or switch accounts**: run `whatsapp-mcp unlink` (add `--delete-data` to also remove local messages), or ask Claude to "unlink my WhatsApp". Then link again. You can also remove the device on your phone under **Linked devices**; whatsapp-mcp notices and shows as not linked.
- **`whatsapp` doesn't appear in Claude Desktop**: make sure Claude Desktop fully quit before restarting. If the entry is missing from the config, quit Claude Desktop and run `whatsapp-mcp install` again. Details are in Claude Desktop's MCP logs (**Settings > Developer > Open Logs Folder**).
- **"no WhatsApp account is linked"**: ask Claude to link it, or run `whatsapp-mcp login`.
- **"the installed whatsapp-mcp is in use"**: Claude Desktop is running it. Quit Claude Desktop (including from the tray), then install again.
- **Unlinked after a while**: WhatsApp drops linked devices that stay offline for about two weeks. Link again.
- **Breaks after a WhatsApp update**: run `go get go.mau.fi/whatsmeow@latest`, then rebuild.
- **Anything else**: check `whatsapp-mcp.log` in the data directory.

## License

[MIT](LICENSE)
