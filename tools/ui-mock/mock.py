#!/usr/bin/env python3
"""Serve ui.html with a fake API, so the settings panel can be viewed and screenshotted
in any browser without Windows. Not part of the app.

    python3 tools/ui-mock/mock.py [port]     then open http://<host>:<port>/?k=x
"""
import json, sys, pathlib, http.server

ROOT = pathlib.Path(__file__).resolve().parents[2]
HTML = (ROOT / "ui.html").read_text(encoding="utf-8")
STATE = {
    "version": "0.1.0", "go": "go1.26.11", "host": "DESK-PC",
    "data_dir": r"C:\Users\you\AppData\Local\nodoff",
    "general": {"open_settings_on_start": False, "poll_seconds": 30, "off_mode": "standby",
                "start_at_login": True, "start_entry_stale": False},
    "mqtt": {"enabled": True, "host": "homeassistant.local", "port": 1883, "tls": False, "username": "nodoff",
             "node_name": "Desk PC", "base_topic": "nodoff/desk-pc", "discovery": True, "discovery_prefix": "homeassistant"},
    "password_set": True,
    "mqtt_status": {"connected": True},
    "monitors": [
        {"id": "len65fa_v9k27xqa", "model": "LEN L28u-30", "serial": "V9K27XQA", "position": "left", "resolution": "3840×2160",
         "primary": True, "present": True, "can_brightness": True, "can_power": True, "brightness": 90, "power": "ON",
         "power_code": 1, "exposed": True, "name": "", "default_name": "LEN L28u-30 (left)", "mccs": "2.2"},
        {"id": "len65fa_v9k31mtd", "model": "LEN L28u-30", "serial": "V9K31MTD", "position": "right", "resolution": "3840×2160",
         "primary": False, "present": True, "can_brightness": True, "can_power": True, "brightness": 90, "power": "OFF",
         "power_code": 4, "exposed": False, "name": "Right screen", "default_name": "LEN L28u-30 (right)", "mccs": "2.2"},
        {"id": "dela0a1_abc123", "model": "DELL U2717D", "serial": "ABC123", "present": False, "can_brightness": False,
         "can_power": False, "brightness": -1, "power": "", "power_code": 0, "exposed": False, "name": "",
         "default_name": "DELL U2717D", "last_seen": "2026-11-01T10:00:00Z"},
    ],
}

class H(http.server.BaseHTTPRequestHandler):
    def _json(self, obj, code=200):
        b = json.dumps(obj).encode()
        self.send_response(code); self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(b))); self.end_headers(); self.wfile.write(b)
    def do_GET(self):
        if self.path.startswith("/?") or self.path == "/":
            b = HTML.encode(); self.send_response(200); self.send_header("Content-Type", "text/html; charset=utf-8")
            self.end_headers(); self.wfile.write(b)
        elif self.path == "/api/state":
            self._json(STATE)
        elif self.path == "/logo.png" and (ROOT / "docs/logo-128.png").exists():
            b = (ROOT / "docs/logo-128.png").read_bytes(); self.send_response(200)
            self.send_header("Content-Type", "image/png"); self.end_headers(); self.wfile.write(b)
        else:
            self.send_response(404); self.end_headers()
    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        body = json.loads(self.rfile.read(n) or b"{}")
        if self.path == "/api/device":
            for m in STATE["monitors"]:
                if m["id"] == body.get("id"):
                    for k in ("exposed", "name"):
                        if k in body: m[k] = body[k]
        elif self.path == "/api/device/test":
            return self._json({"ok": True, "power_code": 1})
        self._json(STATE)
    def log_message(self, *a): pass

http.server.ThreadingHTTPServer(("0.0.0.0", int(sys.argv[1]) if len(sys.argv) > 1 else 8791), H).serve_forever()
