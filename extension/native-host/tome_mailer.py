#!/usr/bin/env python3
"""Tome mail helper (Chrome native-messaging host, macOS).

Receives {to, subject, filename, data(base64)} from the extension, writes the
file to a temp dir, and opens Mail.app with it attached so the user can review
and hit Send. Also answers {ping: true} so the extension can detect it.

Native-messaging framing: 4-byte little-endian length + UTF-8 JSON, both ways.
"""
import base64
import json
import os
import struct
import subprocess
import sys
import tempfile


def read_msg():
    raw = sys.stdin.buffer.read(4)
    if len(raw) < 4:
        sys.exit(0)
    (length,) = struct.unpack("<I", raw)
    return json.loads(sys.stdin.buffer.read(length))


def send_msg(obj):
    data = json.dumps(obj).encode("utf-8")
    sys.stdout.buffer.write(struct.pack("<I", len(data)) + data)
    sys.stdout.buffer.flush()


def as_escape(s):
    """Escape for an AppleScript string literal."""
    return s.replace("\\", "\\\\").replace('"', '\\"')


def compose(to, subject, path):
    script = '''tell application "Mail"
    set newMessage to make new outgoing message with properties {subject:"%s", content:"Delivered by Tome.\\n", visible:true}
    tell newMessage
        make new to recipient at end of to recipients with properties {address:"%s"}
        make new attachment with properties {file name:(POSIX file "%s")} at after the last paragraph of content
    end tell
    activate
end tell''' % (as_escape(subject), as_escape(to), as_escape(path))
    subprocess.run(["osascript", "-e", script], check=True,
                   capture_output=True, text=True)


def main():
    msg = read_msg()
    try:
        if msg.get("ping"):
            send_msg({"ok": True, "pong": True})
            return
        filename = os.path.basename(msg.get("filename") or "article.pdf")
        to = msg["to"]
        subject = msg.get("subject") or filename
        data = base64.b64decode(msg["data"])

        # Mail attaches by reference, so the file must persist on disk.
        outdir = os.path.join(tempfile.gettempdir(), "tome")
        os.makedirs(outdir, exist_ok=True)
        path = os.path.join(outdir, filename)
        with open(path, "wb") as f:
            f.write(data)

        compose(to, subject, path)
        send_msg({"ok": True, "path": path})
    except subprocess.CalledProcessError as e:
        send_msg({"ok": False, "error": "osascript: " + (e.stderr or "").strip()})
    except Exception as e:  # report anything else back to the extension
        send_msg({"ok": False, "error": str(e)})


if __name__ == "__main__":
    main()
