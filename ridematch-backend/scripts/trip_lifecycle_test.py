#!/usr/bin/env python3
"""
End-to-end smoke test for the trip lifecycle: request -> negotiate ->
match -> pickup PIN -> complete, plus the WebSocket push, the
concurrency-safety guarantee, and a couple of negative paths.

Exercises, against a running instance of this API:
  1. OTP login for a passenger, a driver, and an admin.
  2. Driver registration, admin verification + subscription grant, going
     online.
  3. Passenger requests a trip while the driver is connected over
     WebSocket -> asserts the driver receives `trip.new_request` pushed
     in real time (not polled).
  4. Driver makes an offer while the passenger is connected -> asserts
     `trip.offer_received` is pushed.
  5. Passenger accepts the offer while both are connected -> asserts both
     get `trip.matched`, and captures the one-time pickup PIN.
  6. Confirms a wrong PIN is rejected (400).
  7. Fires 5 concurrent "confirm pickup" requests with the CORRECT PIN ->
     asserts exactly 1 succeeds, proving the compare-and-swap state
     transition is race-safe under concurrency.
  8. Completes the trip, then runs a second trip through cancellation.

Requires: `pip install requests websockets`. Assumes SMS_PROVIDER=console
(the local-dev default) so OTP codes can be read back out of the API's
log output — point API_LOG_PATH at wherever you're redirecting the
server's stdout/stderr.

Usage:
    API_BASE_URL=http://localhost:8080 API_LOG_PATH=/tmp/api.log \\
    DB_USER=ridematch DB_PASSWORD=ridematch_dev_pw DB_NAME=ridematch \\
    python3 scripts/trip_lifecycle_test.py
"""
import asyncio
import concurrent.futures
import json
import os
import subprocess
import time

import requests
import websockets

API_BASE = os.environ.get("API_BASE_URL", "http://localhost:8080")
API = f"{API_BASE}/api/v1"
WS = API_BASE.replace("http://", "ws://").replace("https://", "wss://") + "/ws"
API_LOG_PATH = os.environ.get("API_LOG_PATH", "/tmp/api.log")

DB_USER = os.environ.get("DB_USER", "ridematch")
DB_PASSWORD = os.environ.get("DB_PASSWORD", "ridematch_dev_pw")
DB_HOST = os.environ.get("DB_HOST", "127.0.0.1")
DB_NAME = os.environ.get("DB_NAME", "ridematch")

# Unique-per-run phone suffix so this script can be re-run without
# needing to truncate tables first. Numbers must match the API's strict
# Nigerian E.164 validator: +234[789][01]XXXXXXXX (10 digits total, so
# an 8-digit suffix after the fixed "8"+"0"/"1" prefix below).
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
        raise RuntimeError(
            f"couldn't find an OTP code for {phone} in {API_LOG_PATH} — "
            "is SMS_PROVIDER=console and is API_LOG_PATH pointed at the "
            "server's stdout?"
        )
    r = requests.post(f"{API}/auth/otp/verify", json={"phone": phone, "code": code}).json()
    if not r.get("success"):
        raise RuntimeError(f"OTP verify failed for {phone}: {r}")
    return r["data"]


def auth_headers(token):
    return {"Authorization": f"Bearer {token}"}


async def listen(token, out, n=1, timeout=5):
    uri = f"{WS}?token={token}"
    async with websockets.connect(uri) as ws:
        for _ in range(n):
            try:
                msg = await asyncio.wait_for(ws.recv(), timeout=timeout)
                out.append(json.loads(msg))
            except asyncio.TimeoutError:
                break


async def main():
    passenger = otp_login(f"+23480{RUN_ID}")
    driver = otp_login(f"+23481{RUN_ID}")
    ptoken = passenger["tokens"]["access_token"]
    dtoken = driver["tokens"]["access_token"]

    admin_phone = f"+23490{RUN_ID}"
    admin0 = otp_login(admin_phone)
    subprocess.run(
        ["mysql", "-u", DB_USER, f"-p{DB_PASSWORD}", "-h", DB_HOST, DB_NAME, "-e",
         f"UPDATE users SET role='admin' WHERE phone='{admin_phone}';"],
        check=True,
    )
    # Role is baked into the JWT at issuance, so refresh to get a token
    # that reflects the just-granted admin role.
    admin = requests.post(f"{API}/auth/token/refresh",
                           json={"refresh_token": admin0["tokens"]["refresh_token"]}).json()["data"]
    atoken = admin["access_token"]

    dr = requests.post(f"{API}/driver/register", headers=auth_headers(dtoken), json={
        "vehicle_type": "car", "plate_number": f"TST{RUN_ID}",
        "vehicle_photo_url": "https://example.com/v.jpg", "id_document_url": "https://example.com/id.jpg",
    }).json()
    driver_profile_id = dr["data"]["id"]
    print("driver registered:", dr["data"]["verification_status"])

    requests.patch(f"{API}/admin/drivers/{driver_profile_id}/verify",
                    headers=auth_headers(atoken), json={"approve": True})
    requests.patch(f"{API}/admin/drivers/{driver_profile_id}/subscription",
                    headers=auth_headers(atoken), json={"days": 1})
    requests.post(f"{API}/driver/online", headers=auth_headers(dtoken),
                  json={"latitude": 6.5244, "longitude": 3.3792})
    print("driver approved, subscribed, online")

    # --- Trip created while the driver is connected -> instant push ---
    driver_events = []
    listener = asyncio.create_task(listen(dtoken, driver_events, n=1, timeout=6))
    await asyncio.sleep(0.5)  # let the handshake settle before the trip fires

    t0 = time.time()
    trip = requests.post(f"{API}/trips", headers=auth_headers(ptoken), json={
        "vehicle_type": "car", "pickup_lat": 6.5250, "pickup_lng": 3.3800, "pickup_address": "Ikeja",
        "destination_lat": 6.6, "destination_lng": 3.35, "destination_address": "Yaba",
        "offered_price_kobo": 200000,
    }).json()
    trip_id = trip["data"]["id"]
    print("trip created:", trip["data"]["status"], trip_id)

    await listener
    print(f"driver WS push latency: {(time.time() - t0) * 1000:.1f}ms (includes 500ms settle sleep)")
    assert any(e["type"] == "trip.new_request" for e in driver_events), "driver did not receive trip.new_request push"

    # --- Offer made while the passenger is connected -> instant push ---
    passenger_events = []
    listener2 = asyncio.create_task(listen(ptoken, passenger_events, n=1, timeout=6))
    await asyncio.sleep(0.5)

    offer = requests.post(f"{API}/trips/{trip_id}/offers", headers=auth_headers(dtoken),
                           json={"price_kobo": 190000}).json()
    offer_id = offer["data"]["id"]
    print("offer made:", offer["data"]["price_kobo"])

    await listener2
    assert any(e["type"] == "trip.offer_received" for e in passenger_events), "passenger did not receive trip.offer_received push"

    # --- Accept while both connected -> both get trip.matched ---
    both = {"p": [], "d": []}
    lp = asyncio.create_task(listen(ptoken, both["p"], n=1, timeout=6))
    ld = asyncio.create_task(listen(dtoken, both["d"], n=1, timeout=6))
    await asyncio.sleep(0.5)

    accept = requests.post(f"{API}/trips/{trip_id}/offers/{offer_id}/accept", headers=auth_headers(ptoken)).json()
    pin = accept["data"]["pickup_pin"]
    print("matched, pickup pin:", pin)

    await lp
    await ld
    assert any(e["type"] == "trip.matched" for e in both["p"]), "passenger did not get trip.matched"
    assert any(e["type"] == "trip.matched" for e in both["d"]), "driver did not get trip.matched"

    # --- Negative: wrong PIN rejected ---
    wrong = requests.post(f"{API}/trips/{trip_id}/confirm-pickup", headers=auth_headers(dtoken), json={"pin": "0000"})
    assert wrong.status_code == 400, "wrong PIN should be rejected with 400"
    print("wrong PIN correctly rejected")

    # --- Concurrency: 5 simultaneous confirm-pickup calls with the
    # CORRECT pin -> exactly 1 should win the compare-and-swap ---
    def confirm():
        return requests.post(f"{API}/trips/{trip_id}/confirm-pickup", headers=auth_headers(dtoken), json={"pin": pin})

    with concurrent.futures.ThreadPoolExecutor(max_workers=5) as ex:
        results = list(ex.map(lambda _: confirm(), range(5)))
    successes = [r for r in results if r.status_code == 200]
    print(f"concurrent confirm-pickup: {len(successes)}/5 succeeded (expect exactly 1)")
    assert len(successes) == 1, "CAS should allow exactly one concurrent confirm-pickup to succeed"

    complete = requests.post(f"{API}/trips/{trip_id}/complete", headers=auth_headers(dtoken)).json()
    assert complete["data"]["status"] == "completed"
    print("trip completed")

    # --- Cancellation, on a fresh trip ---
    trip2 = requests.post(f"{API}/trips", headers=auth_headers(ptoken), json={
        "vehicle_type": "car", "pickup_lat": 6.5250, "pickup_lng": 3.3800,
        "destination_lat": 6.6, "destination_lng": 3.35, "offered_price_kobo": 100000,
    }).json()
    cancel = requests.post(f"{API}/trips/{trip2['data']['id']}/cancel", headers=auth_headers(ptoken),
                            json={"reason": "changed my mind"}).json()
    assert cancel["data"]["status"] == "cancelled"
    print("cancellation flow OK")

    print("\nALL TRIP LIFECYCLE TESTS PASSED")


if __name__ == "__main__":
    asyncio.run(main())
