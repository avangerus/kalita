
# kalita_error_contract_test.py
#
# Verifies unified error format and status codes:
#  - 400 with code='required' when mandatory field is missing
#  - 409 with code='unique_violation' on unique constraint breach
#
# Usage:
#   pip install requests
#   python kalita_error_contract_test.py --base-url http://localhost:8080
#
import os, sys, json, argparse, random, string
try:
    import requests
except ImportError:
    print("Please install requests: pip install requests", file=sys.stderr)
    sys.exit(2)

def pretty(o): return json.dumps(o, ensure_ascii=False, indent=2)
def rand_suffix(n=6):
    import random, string
    return ''.join(random.choice(string.ascii_lowercase + string.digits) for _ in range(n))

def expect_status(r, code, where):
    if r.status_code != code:
        try: body = r.json()
        except Exception: body = r.text
        raise SystemExit(f"{where}: expected {code}, got {r.status_code}\n{body}")

def assert_error(r, code=None, field=None):
    try:
        body = r.json()
    except Exception:
        raise SystemExit(f"Response is not JSON: {r.text}")
    if "errors" not in body or not isinstance(body["errors"], list) or not body["errors"]:
        raise SystemExit(f"No 'errors' array in response:\n{pretty(body)}")
    found = body["errors"][0]
    # print for visibility
    print("Errors:", pretty(body["errors"]))
    if code and found.get("code") != code:
        raise SystemExit(f"Expected code='{code}', got '{found.get('code')}'")
    if field and found.get("field") != field:
        raise SystemExit(f"Expected field='{field}', got '{found.get('field')}'")

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base-url", default=os.getenv("KALITA_BASE_URL", "http://localhost:8080"))
    args = ap.parse_args()
    base = args.base_url.rstrip("/")

    # --- 1) REQUIRED: create Project without manager_id -> 400 required ---
    proj_url = f"{base}/api/core/project"
    bad_project = {"name": "err-"+rand_suffix(), "status": "Draft"}  # manager_id omitted on purpose
    print("POST", proj_url, "->", bad_project)
    r = requests.post(proj_url, json=bad_project, timeout=10)
    expect_status(r, 400, "Create project missing manager_id")
    assert_error(r, code="required", field="manager_id")
    print("✅ Required error contract OK")

    # --- 2) UNIQUE: create two Users with same email -> second is 409 unique_violation ---
    user_url = f"{base}/api/core/user"
    email = f"uniq_{rand_suffix()}@example.com"
    u1 = {"name": "U1 "+rand_suffix(), "email": email, "role": "Manager"}
    u2 = {"name": "U2 "+rand_suffix(), "email": email, "role": "Manager"}

    print("POST", user_url, "->", u1)
    r1 = requests.post(user_url, json=u1, timeout=10)
    expect_status(r1, 201, "Create first user")

    print("POST", user_url, "->", u2, "(duplicate email)")
    r2 = requests.post(user_url, json=u2, timeout=10)
    expect_status(r2, 409, "Create second user (expect unique violation)")
    assert_error(r2, code="unique_violation", field="email")
    print("✅ Unique violation error contract OK")

    print("\nAll error contract tests passed ✅")

if __name__ == "__main__":
    main()
