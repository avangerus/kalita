import argparse
import requests
import sys

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--module", required=True)
    parser.add_argument("--entity", required=True)
    args = parser.parse_args()

    base = args.base_url.rstrip("/")
    meta_url = f"{base}/api/meta/{args.module}/{args.entity}"
    print(f"GET {meta_url}")
    r = requests.get(meta_url, timeout=5)
    if r.status_code != 200:
        print(f"❌ meta endpoint failed: {r.status_code}")
        sys.exit(1)

    meta = r.json()
    print("Meta:", meta)

    # 1) displayField должен быть непустой строкой
    df = meta.get("displayField")
    if not isinstance(df, str) or not df.strip():
        print("❌ displayField missing or invalid")
        sys.exit(1)
    print(f"✅ displayField = {df}")

    # 2) для ref-поля (например manager_id) должен быть refDisplayField
    ref_fields = [f for f in meta.get("fields", []) if f.get("ref")]
    if not ref_fields:
        print("❌ no ref fields in schema")
        sys.exit(1)

    for f in ref_fields:
        if not f.get("refDisplayField"):
            print(f"❌ refDisplayField missing for field {f['name']}")
            sys.exit(1)
        print(f"✅ field {f['name']} refDisplayField = {f['refDisplayField']}")

    print("Meta display test passed ✅")

if __name__ == "__main__":
    main()
