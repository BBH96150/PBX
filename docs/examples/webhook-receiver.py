#!/usr/bin/env python3
"""
A complete, runnable webhook receiver for the SIP platform.

It verifies the HMAC-SHA256 signature on every delivery (rejecting forgeries)
and dispatches the events the platform sends. Copy it, set WEBHOOK_SECRET to the
signing secret shown when you create the endpoint in the portal
(Tenant -> Webhooks), and point your endpoint URL at this server.

  pip install flask           # only dependency
  WEBHOOK_SECRET=... python3 webhook-receiver.py
  # expose it publicly (e.g. `ngrok http 8088`) and register that HTTPS URL.

Only public HTTPS URLs are accepted by the platform, and redirects are not
followed — terminate TLS in front of this (a tunnel like ngrok does that).
"""
import hashlib
import hmac
import os
import sys

from flask import Flask, request

SECRET = os.environ.get("WEBHOOK_SECRET", "")
if not SECRET:
    sys.exit("set WEBHOOK_SECRET to your endpoint's signing secret")

app = Flask(__name__)


def signature_ok(raw_body: bytes, header: str) -> bool:
    expected = "sha256=" + hmac.new(SECRET.encode(), raw_body, hashlib.sha256).hexdigest()
    # constant-time compare avoids leaking the secret via timing.
    return hmac.compare_digest(expected, header or "")


@app.post("/webhook")
def webhook():
    raw = request.get_data()  # the EXACT bytes — sign/verify over these, not a re-encode
    if not signature_ok(raw, request.headers.get("X-Webhook-Signature", "")):
        return ("bad signature", 401)

    event = request.headers.get("X-Webhook-Event", "")
    payload = request.get_json(silent=True) or {}
    data = payload.get("data", {})

    # Dispatch. Events: call.completed, trunk.down, trunk.up, voicemail.new, test.ping
    if event == "call.completed":
        print(f"call {data.get('direction')} {data.get('from')} -> {data.get('to')} "
              f"{data.get('duration_sec')}s {data.get('disposition')}")
    elif event in ("trunk.down", "trunk.up"):
        print(f"trunk {data.get('trunk')} {data.get('prev_state')} -> {data.get('state')}")
    elif event == "voicemail.new":
        print(f"new voicemail for ext {data.get('extension_id')} from {data.get('caller_id_num')}")
    elif event == "test.ping":
        print("test ping received — endpoint is wired up correctly")
    else:
        print(f"unhandled event: {event}")

    # Return 2xx quickly; the platform retries on non-2xx / timeout.
    return ("", 204)


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=int(os.environ.get("PORT", "8088")))
