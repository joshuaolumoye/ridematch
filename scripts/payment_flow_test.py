#!/usr/bin/env python3
"""
End-to-end smoke test for the driver subscription payment flow: checkout
link creation, the webhook (with signature verification and server-side
re-verification against Flutterwave), and idempotency on a retried
webhook.

Runs against a REAL running instance of this API and a stub Flutterwave
server (scripts/stub_flutterwave_server.py) — there's no way to exercise
the actual Flutterwave API from an automated test without a live account,
so this proves the whole request/response contract end-to-end (routing,
signature checking, amount/currency verification, idempotent crediting)
against a server standing in for Flutterwave's documented behavior. The
underlying business logic also has unit test coverage with a fake gateway
in internal/service/payment_service_test.go (`go test ./internal/service/...`).

Setup:
    1. python3 scripts/stub_flutterwave_server.py   (leave running)
    2. In your .env: FLW_BASE_URL=http://127.0.0.1:9099
                      FLW_WEBHOOK_SECRET_HASH=test-webhook-secret-123 (or your own — must match WEBHOOK_SECRET below)
       Restart `go run ./cmd/api` after changing .env.
    3. API_LOG_PATH=/tmp/api.log WEBHOOK_SECRET=test-webhook-secret-123 \\
       python3 scripts/payment_flow_test.py
"""
import os
import subprocess
import time

import requests

API_BASE = os.environ.get("API_BASE_URL", "http://localhost:8080")
API = f"{API_BASE}/api/v1"
API_LOG_PATH = os.environ.get("API_LOG_PATH", "/tmp/api.log")
WEBHOOK_SECRET = os.environ.get("WEBHOOK_SECRET", "test-webhook-secret-123")

DB_USER = os.environ.get("DB_USER", "ridematch")
DB_PASSWORD = os.environ.get("DB_PASSWORD", "ridematch_dev_pw")
DB_HOST = os.environ.get("DB_HOST", "127.0.0.1")
DB_NAME = os.environ.get("DB_NAME", "ridematch")

RUN_ID = str(int(time.time() * 1000))[-8:]


def otp_login(phone):
    requests.post(f"{API}/auth/otp/request", json={"phone": phone})
    time.sleep(0.15)
    with open(API_LOG_PATH) as f:
        lines = f.readlines()
    code = None
    for line in reversed(lines):
        if f"SMS -> {phone}" in line and "code is" in line:
            code = line.split("code is")[1].strip().split(".")[0].strip()
            break
    if code is None:
        raise RuntimeError(f"couldn't find an OTP code for {phone} in {API_LOG_PATH}")
    r = requests.post(f"{API}/auth/otp/verify", json={"phone": phone, "code": code}).json()
    if not r.get("success"):
        raise RuntimeError(f"OTP verify failed for {phone}: {r}")
    return r["data"]


def auth_headers(token):
    return {"Authorization": f"Bearer {token}"}


def main():
    driver = otp_login(f"+23480{RUN_ID}")
    dtoken = driver["tokens"]["access_token"]

    dr = requests.post(f"{API}/driver/register", headers=auth_headers(dtoken), json={
        "vehicle_type": "car", "plate_number": f"PAY{RUN_ID}",
        "vehicle_photo_url": "https://example.com/v.jpg", "id_document_url": "https://example.com/id.jpg",
    }).json()
    print("driver registered:", dr["data"]["verification_status"])
    assert dr["data"]["has_active_subscription"] is False

    checkout = requests.post(f"{API}/driver/subscription/checkout", headers=auth_headers(dtoken),
                              json={"days": 3}).json()
    print("checkout:", checkout["data"])
    assert checkout["success"], checkout
    assert checkout["data"]["days"] == 3
    tx_ref = checkout["data"]["tx_ref"]

    profile = requests.get(f"{API}/driver/profile", headers=auth_headers(dtoken)).json()
    assert profile["data"]["has_active_subscription"] is False, "must not be active before the webhook fires"
    print("confirmed: no subscription granted before webhook fires")

    event = {"event": "charge.completed", "data": {
        "id": 555001, "tx_ref": tx_ref, "status": "successful",
        "amount": checkout["data"]["amount_kobo"] / 100, "currency": "NGN",
    }}

    bad = requests.post(f"{API_BASE}/webhooks/flutterwave", headers={"verif-hash": "wrong"}, json=event)
    assert bad.status_code == 401, f"expected 401 for bad signature, got {bad.status_code}"
    print("wrong signature correctly rejected (401)")

    ok = requests.post(f"{API_BASE}/webhooks/flutterwave", headers={"verif-hash": WEBHOOK_SECRET}, json=event)
    assert ok.status_code == 200, f"webhook failed: {ok.status_code} {ok.text}"
    print("webhook processed")

    profile2 = requests.get(f"{API}/driver/profile", headers=auth_headers(dtoken)).json()
    assert profile2["data"]["has_active_subscription"] is True
    print("subscription active after webhook:", profile2["data"]["subscription_active_until"])

    subprocess.run(["mysql", "-u", DB_USER, f"-p{DB_PASSWORD}", "-h", DB_HOST, DB_NAME, "-e",
                     f"UPDATE driver_profiles SET verification_status='approved' WHERE plate_number='PAY{RUN_ID}';"],
                    check=True)
    online = requests.post(f"{API}/driver/online", headers=auth_headers(dtoken),
                            json={"latitude": 6.5, "longitude": 3.4}).json()
    assert online["success"], "driver should be able to go online now (verified + subscribed)"
    print("driver can go online after payment")

    replay = requests.post(f"{API_BASE}/webhooks/flutterwave", headers={"verif-hash": WEBHOOK_SECRET}, json=event)
    assert replay.status_code == 200
    profile3 = requests.get(f"{API}/driver/profile", headers=auth_headers(dtoken)).json()
    assert profile3["data"]["subscription_active_until"] == profile2["data"]["subscription_active_until"], \
        "a retried webhook must not grant a second round of days"
    print("confirmed: replayed webhook did not double-credit")

    print("\nALL PAYMENT FLOW TESTS PASSED")


if __name__ == "__main__":
    main()
