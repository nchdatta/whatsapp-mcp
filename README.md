# whatsapp-mcp

[![CI](https://github.com/nchdatta/whatsapp-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/nchdatta/whatsapp-mcp/actions/workflows/ci.yml)

Use your WhatsApp from **Claude Desktop**. Read and search your chats, look at photos people send you, and send messages, files and voice notes. It works with any other [MCP](https://modelcontextprotocol.io) client too.

It's one self-contained binary. Your messages stay on your computer, encrypted, and only reach Claude when it calls a tool.

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

- copies the binary to a per-user folder that's on your PATH: `%LOCALAPPDATA%\Programs\whatsapp-mcp\` on Windows (added to your user PATH automatically), `~/.local/bin/` on macOS and Linux,
- adds it to Claude Desktop's `claude_desktop_config.json`. It finds the file for regular and Microsoft Store installs, keeps your other settings, and saves a `.bak` copy first.

On Windows you can also download `whatsapp-mcp-windows-amd64.exe` (or `-arm64`) from the release and **double-click it**. Builds exist for Windows, macOS and Linux on both amd64 and arm64.

To update, run `whatsapp-mcp update` (or the install command again). Claude Desktop can stay open; it offers to restart it. Claude also mentions when a new version is out. To pin a version, set `WHATSAPP_MCP_VERSION=v1.2.3` before running the installer.

Something not working? Run `whatsapp-mcp doctor`. It checks the Claude Desktop config, the program, the link and recent errors, and says how to fix each problem.

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

The installer offers to restart (or start) Claude Desktop for you; press Enter. To do it by hand, quit it completely, including from the system tray (Windows) or menu bar (macOS), then start it again. `whatsapp` should appear under **Settings > Developer**. Try *"What did the family group talk about today?"*

Optional: install [ffmpeg](https://ffmpeg.org/download.html) to send any audio file as a voice note. Without it, only `.ogg`/`.opus` files can be sent as voice notes; anything can still be sent as a regular file.

### Other MCP clients

Run `whatsapp-mcp serve` over stdio. For example, in Claude Code:

```sh
claude mcp add --scope user whatsapp -- /path/to/whatsapp-mcp serve
```

Only use one client at a time with the same data directory; see [How it works](#how-it-works).

### claude.ai (web and mobile)

claude.ai can't start programs on your computer, so it connects to a remote MCP server over HTTPS instead.

> **Security:** the connector URL contains a secret token that gives full access to your WhatsApp: reading every chat and sending as you. Don't share it or put it in screenshots. If it leaks, run `whatsapp-mcp token --rotate` and restart `serve --http`. Proper OAuth is planned; see [docs/oauth.md](docs/oauth.md).

1. Quit Claude Desktop (both would use the same WhatsApp session), then start the HTTP server:
   ```sh
   whatsapp-mcp serve --http
   ```
   It prints the local URL, `http://127.0.0.1:8080/mcp/<token>`.
2. In another terminal, give it a public HTTPS address, for example with [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/):
   ```sh
   cloudflared tunnel --url http://127.0.0.1:8080
   ```
   This prints a `https://<random>.trycloudflare.com` address. Quick tunnels get a new address each run; set up a named tunnel for a stable one.
3. In claude.ai, open **Settings > Connectors > Add custom connector**, and enter `https://<random>.trycloudflare.com/mcp/<token>`. The connector then also works in the Claude mobile apps.

It only works while your computer and both commands are running. `whatsapp-mcp token` shows the token again. Other HTTP clients can send `Authorization: Bearer <token>` to `/mcp` instead of putting the token in the path.

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
| `wait_for_messages` | Waits for new incoming messages (checks every 3 s) and returns them, with saved attachment paths. Call it in a loop to watch |
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
| `whatsapp-mcp serve [--http [ADDR]]` | The MCP server; Claude Desktop runs this. `--http` serves remote clients such as claude.ai, on 127.0.0.1:8080 unless you give an address |
| `whatsapp-mcp away on ["text"] \| off \| status` | Fixed fallback reply when nobody answers in time; see [Away message](#away-message) |
| `whatsapp-mcp token [--rotate]` | Show, or replace, the token that protects `serve --http` |
| `whatsapp-mcp version` | Print the version |

All commands accept `--data DIR` (or `WHATSAPP_MCP_DATA`).

### Data directory

| OS | Default |
|---|---|
| Windows | `%APPDATA%\whatsapp-mcp` |
| macOS | `~/Library/Application Support/whatsapp-mcp` |
| Linux | `~/.config/whatsapp-mcp` |

| File | Contents | At rest |
|---|---|---|
| `session.db` | Your WhatsApp device keys | Encrypted |
| `history.db` | Chats and messages | Encrypted |
| `media/` | Saved attachments (`*.enc`) | Encrypted |
| `http-token` | Secret for `serve --http` | Encrypted |
| `key` | The encryption key, itself protected by the OS (Windows and Linux; on macOS it's in the Keychain instead) | See below |
| `whatsapp-mcp.log` | Logs: chat IDs and errors, no message text. Check here first when something goes wrong | Plain text |

#### Encryption

Everything above is encrypted with a random key created on first run: databases with Adiantum (through the SQLite storage layer), attachments and the token with AES-256-GCM. Data from older versions is encrypted automatically on the first start.

The key is protected by the operating system:

- **Windows:** encrypted with DPAPI, so only your Windows account on this PC can unlock it. A copy of the data folder is useless on another PC or account.
- **macOS:** stored in your login Keychain.
- **Linux:** a file readable only by you. This protects copies of the databases, but not someone who can read your whole data folder; use full-disk encryption for that.

When Claude opens an attachment (`get_attachment`, or `wait_for_messages` showing a path), a readable copy is written to a temporary folder that's deleted when whatsapp-mcp exits.

It doesn't protect against programs running as you while you're logged in, since they can ask the OS for the key just like whatsapp-mcp does. Full-disk encryption (BitLocker / Device encryption, FileVault) is still recommended.

If the key is lost (for example a Windows profile reset), the data can't be recovered: link WhatsApp again, and recent history will sync back.

### Auto-reply assistant

Claude can watch for new messages and answer them, in Claude Desktop or in claude.ai (through `serve --http`), with no script.

**1. Allow it, in your own words.** Claude only sends messages under your name when *you* have clearly allowed it; permission can't come from whatsapp-mcp itself. To avoid typing it every time, create a **Project** in Claude Desktop (for example "WhatsApp assistant") and put your permission in its **project instructions**, for example:

> I authorize you to reply automatically, on my behalf and without asking me, to every incoming WhatsApp message in chats of this project, including groups, until I say stop.

**2. Start it.** In a chat in that project, say **"watch my WhatsApp"**, or click **+ > whatsapp > Watch WhatsApp** (set `groups` to `no` to skip groups). Say **"stop"** to end it.

Claude keeps replies short, tags senders in groups for important replies, and declines anything about your computer, accounts or money. It's still Claude's judgment per message: a reply that looks risky can be held back. The away message below covers those.

Claude calls `wait_for_messages`, which returns new messages like this:

```
Rahim (+8801…) in Family - 10:21 AM: [image: C:\Users\…\Temp\whatsapp-mcp-view-…\3EB0….jpg] look!  [chat: 1203…@g.us] [id: 3EB0…] [tag: @8801…]
```

It replies, then calls `wait_for_messages` again with the returned cursor. It keeps going for as long as the chat keeps running; when Claude stops (long conversations end at some point), say *"continue"*.

> **Use with care:** automated replies can get an account banned, and anyone who messages you can try to give Claude instructions. Consider limiting it to certain chats or leaving groups out (`skip_groups`).

### Away message

A fixed fallback reply, so nobody goes without an answer, even when Claude isn't watching, holds a reply back, or is slow:

```sh
whatsapp-mcp away on "Hi! I'm not available right now. I'll get back to you soon."
whatsapp-mcp away on --delay 90 --groups     # change settings, keep the text
whatsapp-mcp away status
whatsapp-mcp away off
```

When a chat's latest message is from someone else and nobody (Claude, or you on your phone) has answered within `--delay` seconds (default 60), whatsapp-mcp sends your text. At most once per chat every `--cooldown` (default `3h`); direct chats only unless you add `--groups`. It only answers messages from the last 30 minutes, never an old backlog, and runs while whatsapp-mcp is running (Claude Desktop open, or `serve --http`).

## Project layout

```
cmd/whatsapp-mcp/    CLI entry point (install, serve, login, status, logout)
internal/config/     data directory and logging
internal/desktop/    Claude Desktop config (install / uninstall)
internal/store/      SQLite schema and queries
internal/wa/         WhatsApp connection: sync, names, sending, attachments, linking
internal/mcpserver/  MCP tool definitions
internal/secret/     encryption at rest: OS-protected key, file encryption
internal/audio/      voice notes: ffmpeg conversion, duration and waveform
scripts/             install.ps1 / install.sh (release installers), setup.ps1 / setup.sh (from source)
```

## Build

Requires Go 1.26 or newer. No C compiler is needed: SQLite, with encryption, is pure Go.

```sh
go build -o whatsapp-mcp ./cmd/whatsapp-mcp
go test ./...
```

Releases: push a tag such as `v1.2.3`. [`.github/workflows/release.yml`](.github/workflows/release.yml) tests the code, builds all six platform binaries with the version embedded, and publishes them with `SHA256SUMS` as a GitHub Release.

The binaries are unsigned, so Windows SmartScreen and macOS Gatekeeper will ask for confirmation on first run.

## Troubleshooting

- **`'whatsapp-mcp' is not recognized`** (Windows): open a *new* terminal after installing; already-open ones keep their old PATH. Or run it by its full path: `%LOCALAPPDATA%\Programs\whatsapp-mcp\whatsapp-mcp.exe`.
- **Unlink or switch accounts**: run `whatsapp-mcp unlink` (add `--delete-data` to also remove local messages), or ask Claude to "unlink my WhatsApp". Then link again. You can also remove the device on your phone under **Linked devices**; whatsapp-mcp notices and shows as not linked.
- **`whatsapp` doesn't appear in Claude Desktop**: make sure Claude Desktop fully quit before restarting. If the entry is missing from the config, quit Claude Desktop and run `whatsapp-mcp install` again. Details are in Claude Desktop's MCP logs (**Settings > Developer > Open Logs Folder**).
- **"no WhatsApp account is linked"**: ask Claude to link it, or run `whatsapp-mcp login`.
- **"the installed whatsapp-mcp is in use"**: Claude Desktop is running it. Quit Claude Desktop (including from the tray), then install again.
- **Unlinked after a while**: WhatsApp drops linked devices that stay offline for about two weeks. Link again.
- **Breaks after a WhatsApp update**: run `go get go.mau.fi/whatsmeow@latest`, then rebuild.
- **Anything else**: check `whatsapp-mcp.log` in the data directory.

## License

[MIT](LICENSE)
