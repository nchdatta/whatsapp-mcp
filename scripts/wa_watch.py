"""Print incoming WhatsApp messages as they arrive.

Tails history.db written by `whatsapp-mcp serve`. Attachments are saved
automatically by whatsapp-mcp; their local path is printed once available.
Uses the same data directory lookup as whatsapp-mcp ($WHATSAPP_MCP_DATA,
otherwise the per-user config directory).
"""
import os
import sqlite3
import sys
import time
from datetime import datetime


def data_dir():
    if os.environ.get("WHATSAPP_MCP_DATA"):
        return os.environ["WHATSAPP_MCP_DATA"]
    if sys.platform == "win32":
        base = os.environ.get("APPDATA", os.path.expanduser("~"))
    elif sys.platform == "darwin":
        base = os.path.expanduser("~/Library/Application Support")
    else:
        base = os.environ.get("XDG_CONFIG_HOME", os.path.expanduser("~/.config"))
    return os.path.join(base, "whatsapp-mcp")


DATA = data_dir().replace("\\", "/")
HISTORY = f"file:{DATA}/history.db?mode=ro"
SESSION = f"file:{DATA}/session.db?mode=ro"

NEW_MESSAGES = """
    SELECT m.seq, m.msg_id, m.sent_at, m.chat_jid, c.title, m.sender_jid, m.sender_name,
           m.body, m.media_kind
    FROM message m LEFT JOIN chat c ON c.jid = m.chat_jid
    WHERE m.seq > ? AND m.from_me = 0
    ORDER BY m.seq"""


def connect(uri):
    return sqlite3.connect(uri, uri=True, timeout=5)


def phone_and_name(sender_jid, stored_name):
    """Best-effort '+phone' and display name, using the session's LID map and contacts."""
    user, _, server = sender_jid.partition("@")
    phone, name = user, stored_name or None
    try:
        s = connect(SESSION)
        if server == "lid":
            row = s.execute("SELECT pn FROM whatsmeow_lid_map WHERE lid = ?", (user,)).fetchone()
            if row:
                phone = row[0]
        for jid in (f"{phone}@s.whatsapp.net", f"{user}@lid"):
            row = s.execute(
                "SELECT full_name, first_name, business_name, push_name FROM whatsmeow_contacts WHERE their_jid = ?",
                (jid,),
            ).fetchone()
            found = next((x for x in row or () if x), None)
            if found:
                name = found
                break
        s.close()
    except sqlite3.Error:
        pass
    return name or "Unknown", f"+{phone}"


def wait_for_file(chat_jid, msg_id, timeout=60):
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            h = connect(HISTORY)
            row = h.execute(
                "SELECT media_file FROM message WHERE chat_jid = ? AND msg_id = ?", (chat_jid, msg_id)
            ).fetchone()
            h.close()
            if row and row[0]:
                return row[0]
        except sqlite3.Error:
            pass
        time.sleep(1)
    return None


def clock(unix_seconds):
    return datetime.fromtimestamp(unix_seconds).strftime("%I:%M %p").lstrip("0")


def main():
    if not os.path.exists(f"{DATA}/history.db"):
        sys.exit(f"No history.db in {DATA}. Start `whatsapp-mcp serve` (or set WHATSAPP_MCP_DATA).")

    last = connect(HISTORY).execute("SELECT COALESCE(MAX(seq), 0) FROM message").fetchone()[0]
    while True:
        try:
            h = connect(HISTORY)
            rows = h.execute(NEW_MESSAGES, (last,)).fetchall()
            h.close()
            for seq, msg_id, sent_at, chat_jid, title, sender_jid, sender_name, body, media in rows:
                last = seq
                name, phone = phone_and_name(sender_jid, sender_name)
                if media:
                    path = wait_for_file(chat_jid, msg_id)
                    tag = f"[{media}: {path}]" if path else f"[{media}: not saved]"
                    body = f"{tag} {body}".strip()
                group = f" in {title or chat_jid}" if chat_jid.endswith("@g.us") else ""
                print(f"{name} ({phone}){group} - {clock(sent_at)}: {body}  [chat: {chat_jid}]", flush=True)
        except sqlite3.Error as e:
            print(f"watcher db error: {e}", file=sys.stderr, flush=True)
        time.sleep(3)


if __name__ == "__main__":
    main()
