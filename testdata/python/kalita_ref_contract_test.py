
# kalita_ref_contract_test.py
#
# Verifies reference integrity:
#  - 409 with code='ref_not_found' when ref id doesn't exist
#  - 201 when ref id is valid
#
# Usage:
#   pip install requests
#   python kalita_ref_contract_test.py --base-url http://localhost:8080
#
import os, sys, json, argparse, random, string
try:
    import requests
except ImportError:
    print("Please install requests: pip install requests", file=sys.stderr)
    sys.exit(2)

def pretty(o): return json.dumps(o, ensure_ascii=False, indent=2)
def rand_suffix(n=6):
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
    if "errors" not in body or not body["errors"]:
        raise SystemExit(f"No 'errors' array in response:\n{pretty(body)}")
    print("Errors:", pretty(body["errors"]))
    if code and body["errors"][0].get("code") != code:
        raise SystemExit(f"Expected code='{code}', got '{body['errors'][0].get('code')}'")
    if field and body["errors"][0].get("field") != field:
        raise SystemExit(f"Expected field='{field}', got '{body['errors'][0].get('field')}'")

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base-url", default=os.getenv("KALITA_BASE_URL", "http://localhost:8080"))
    args = ap.parse_args()
    base = args.base_url.rstrip("/")

    proj_url = f"{base}/api/core/project"
    user_url = f"{base}/api/core/user"

    # --- 1) BAD REF: use clearly non-existent id (random ULID-like) ---
    bad_id = "01BADBADBADBADBADBADBADBA"
    bad_project = {"name": "ref-"+rand_suffix(), "status": "Draft", "manager_id": bad_id}
    print("POST", proj_url, "-> bad ref", bad_project)
    r = requests.post(proj_url, json=bad_project, timeout=10)
    expect_status(r, 409, "Create project with bad manager_id")
    assert_error(r, code="ref_not_found", field="manager_id")
    print("✅ ref_not_found contract OK")

    # --- 2) GOOD REF: create manager user, then project ---
    u_payload = {"name": "Mgr "+rand_suffix(), "email": f"mgr_{rand_suffix()}@example.com", "role": "Manager"}
    print("POST", user_url, "->", u_payload)
    ru = requests.post(user_url, json=u_payload, timeout=10)
    expect_status(ru, 201, "Create user")
    manager_id = ru.json().get("id")
    if not manager_id:
        raise SystemExit("No id in user create response")

    good_project = {"name": "ok-"+rand_suffix(), "status": "Draft", "manager_id": manager_id}
    print("POST", proj_url, "->", good_project)
    rp = requests.post(proj_url, json=good_project, timeout=10)
    expect_status(rp, 201, "Create project with valid manager_id")
    print("Created project:", pretty(rp.json()))
    print("\nRef contract tests passed ✅")

if __name__ == "__main__":
    main()
