#!/usr/bin/env python3
"""
A minimal stand-in for Flutterwave's API, for local testing without a real
Flutterwave account. Implements just the two endpoints this app calls:

  POST /payments                        -> returns a fake checkout link
  GET  /transactions/{id}/verify         -> echoes back the last payment as
                                             a "successful" transaction

Point the API at it with FLW_BASE_URL=http://127.0.0.1:9099 in your .env,
then use scripts/payment_flow_test.py to drive a real payment through the
whole stack against this stub.

Usage:
    python3 scripts/stub_flutterwave_server.py
"""
import json
from http.server import BaseHTTPRequestHandler, HTTPServer

PORT = 9099
STATE = {"last_tx_ref": None, "last_amount": None, "tx_id": 555001}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        pass  # keep stdout quiet — this is a test fixture, not a server to watch

    def do_POST(self):
        if self.path == "/payments":
            length = int(self.headers.get("Content-Length", 0))
            body = json.loads(self.rfile.read(length))
            STATE["last_tx_ref"] = body["tx_ref"]
            STATE["last_amount"] = body["amount"]
            self._send(200, {
                "status": "success",
                "message": "Payment link created",
                "data": {"link": f"https://stub-checkout.example/pay/{body['tx_ref']}"},
            })
        else:
            self._send(404, {"status": "error", "message": "not found"})

    def do_GET(self):
        if self.path.startswith("/transactions/") and self.path.endswith("/verify"):
            self._send(200, {
                "status": "success",
                "data": {
                    "id": STATE["tx_id"],
                    "tx_ref": STATE["last_tx_ref"],
                    "flw_ref": "FLWREF123",
                    "status": "successful",
                    "amount": float(STATE["last_amount"]) if STATE["last_amount"] is not None else 0.0,
                    "currency": "NGN",
                },
            })
        else:
            self._send(404, {"status": "error", "message": "not found"})

    def _send(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    server = HTTPServer(("127.0.0.1", PORT), Handler)
    print(f"stub Flutterwave server listening on http://127.0.0.1:{PORT}")
    server.serve_forever()
