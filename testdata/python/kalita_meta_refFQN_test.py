#!/usr/bin/env python3
import requests

BASE = "http://localhost:8080/api/meta"

def ok(flag, msg):
    print(("🟩 " if flag else "🟥 ") + msg)

def main():
    r = requests.get(BASE)
    ok(r.status_code == 200, f"/api/meta HTTP {r.status_code}")
    if r.status_code != 200:
        return

    entities = r.json()
    checked = 0
    missing = []

    for ent in entities:
        mod, name = ent["module"], ent["entity"]
        r2 = requests.get(f"{BASE}/{mod}/{name}")
        if r2.status_code != 200:
            continue
        data = r2.json()
        for f in data.get("fields", []):
            if f["type"] == "ref" or (f["type"] == "array" and f.get("elemType") == "ref"):
                checked += 1
                if not f.get("refFQN"):
                    missing.append(f"{mod}.{name}.{f['name']} (ref={f.get('ref')})")

    ok(checked > 0, f"нашли {checked} ссылочных полей")
    ok(len(missing) == 0, "все ref/ref[] имеют refFQN")
    if missing:
        print("    отсутствует refFQN у:", ", ".join(missing))

if __name__ == "__main__":
    main()
