#!/usr/bin/env python3
# -*- coding: utf-8 -*-

"""
Composite-unique test for olga.ExchangeRate with enum Currency.code.
Uses USD/EUR for currency codes.

Run: python kalita_unique_test.py
"""

import sys, time, requests

BASE_URL = "http://localhost:8080"
HEADERS  = {"Content-Type": "application/json"}
TIMEOUT  = 30

MOD = "olga"
CURRENCY = "currency"          # or "Currency" if your API is case-sensitive
EXCHANGE_RATE = "exchangerate" # or "ExchangeRate"

def u(*parts): return "/".join(p.strip("/") for p in parts)
def post(mod, ent, payload): return requests.post(u(BASE_URL,"api",mod,ent), headers=HEADERS, json=payload, timeout=TIMEOUT)
def delete(mod, ent, rec_id): return requests.delete(u(BASE_URL,"api",mod,ent,rec_id), timeout=TIMEOUT)
def ensure_meta(obj, where):
    for k in ("id","version","created_at","updated_at"):
        if k not in obj: raise AssertionError(f"{where}: missing {k}")

def create_currency(code, name=None, symbol=None):
    payload = {"code": code}
    if name:   payload["name"] = name
    if symbol: payload["symbol"] = symbol
    r = post(MOD, CURRENCY, payload)
    return r

def main():
    # 1) создаём две валюты с enum-кодами
    r1 = create_currency("USD", name="US Dollar", symbol="$")
    if r1.status_code not in (200,201):
        print("Currency(base=USD) CREATE failed:", r1.status_code, r1.text, file=sys.stderr)
        print("Если у тебя другой enum (например, RUB/EUR), замени коды в скрипте.", file=sys.stderr)
        sys.exit(1)
    base = r1.json(); ensure_meta(base, "Currency USD")
    base_id = base["id"]

    r2 = create_currency("EUR", name="Euro", symbol="€")
    if r2.status_code not in (200,201):
        delete(MOD, CURRENCY, base_id)
        print("Currency(quote=EUR) CREATE failed:", r2.status_code, r2.text, file=sys.stderr)
        print("Если у тебя другой enum (например, RUB/EUR), замени коды в скрипте.", file=sys.stderr)
        sys.exit(1)
    quote = r2.json(); ensure_meta(quote, "Currency EUR")
    quote_id = quote["id"]
    print(f"[OK] Currencies created: USD={base_id}, EUR={quote_id}")

    # 2) создаём ExchangeRate (base, quote, date)
    rate_payload = {
        "base":  base_id,
        "quote": quote_id,
        "rate":  90.5,
        "date":  "2025-01-01"
    }
    r3 = post(MOD, EXCHANGE_RATE, rate_payload)
    if r3.status_code not in (200,201):
        delete(MOD, CURRENCY, quote_id)
        delete(MOD, CURRENCY, base_id)
        print("ExchangeRate CREATE failed:", r3.status_code, r3.text, file=sys.stderr); sys.exit(1)
    rate1 = r3.json(); ensure_meta(rate1, "ExchangeRate #1")
    rate1_id = rate1["id"]
    print(f"[OK] ExchangeRate #1 created: {rate1_id}")

    # 3) дубликат (тот же base,quote,date) -> 409 unique_violation
    r4 = post(MOD, EXCHANGE_RATE, dict(rate_payload))
    if r4.status_code != 409:
        delete(MOD, EXCHANGE_RATE, rate1_id)
        delete(MOD, CURRENCY, quote_id)
        delete(MOD, CURRENCY, base_id)
        print("Expected 409 unique_violation, got:", r4.status_code, r4.text, file=sys.stderr); sys.exit(1)
    try: body = r4.json()
    except: body = {}
    errs = (body.get("errors") or []) if isinstance(body, dict) else []
    if not any(isinstance(e, dict) and e.get("code") == "unique_violation" for e in errs):
        delete(MOD, EXCHANGE_RATE, rate1_id)
        delete(MOD, CURRENCY, quote_id)
        delete(MOD, CURRENCY, base_id)
        print("409 returned, but missing code=unique_violation. Body:", body, file=sys.stderr); sys.exit(1)
    print("[OK] Duplicate rejected with 409 unique_violation")

    # 4) другая дата -> 201
    rate_payload2 = dict(rate_payload); rate_payload2["date"] = "2025-01-02"
    r5 = post(MOD, EXCHANGE_RATE, rate_payload2)
    if r5.status_code not in (200,201):
        delete(MOD, EXCHANGE_RATE, rate1_id)
        delete(MOD, CURRENCY, quote_id)
        delete(MOD, CURRENCY, base_id)
        print("ExchangeRate #2 CREATE failed (should succeed):", r5.status_code, r5.text, file=sys.stderr); sys.exit(1)
    rate2 = r5.json(); ensure_meta(rate2, "ExchangeRate #2")
    rate2_id = rate2["id"]
    print(f"[OK] ExchangeRate #2 created: {rate2_id} (different date)")

    print("\nComposite unique test passed ✅")

    # cleanup
    delete(MOD, EXCHANGE_RATE, rate2_id)
    delete(MOD, EXCHANGE_RATE, rate1_id)
    time.sleep(0.1)
    delete(MOD, CURRENCY, quote_id)
    delete(MOD, CURRENCY, base_id)

if __name__ == "__main__":
    main()
