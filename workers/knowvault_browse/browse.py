#!/usr/bin/env python3
"""KnowVault files-connector — read-only directory browse (a kalita capability worker).

The node owns permissions and audit; this worker is a dumb executor: it lists
subdirectories of a BACKEND-visible root so a human can pick a folder to index
(in Docker that root is the mounted volume, not the browser's machine). It never
writes anything and never decides who may see what — the node already enforced
that before proxying here.

Read-only capability `list_dirs(path) -> tree`:
  GET /list_dirs?path=<rel>  -> {shape:"tree", path, items:[{id,label,has_children,files}]}

SECURITY — hard sandbox to one configured root:
  KV_FILES_ROOT (required) — the only subtree ever exposed. Every request is
  resolved to a realpath and must stay under the root; '..', absolute escapes
  and symlink-escapes are refused. Nothing outside the root, ever.

Env: KV_FILES_ROOT (required), KV_BROWSE_LISTEN (default 127.0.0.1:8201)
Zero dependencies: stdlib only.
"""
import json
import os
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs

ROOT = os.environ.get("KV_FILES_ROOT")
LISTEN = os.environ.get("KV_BROWSE_LISTEN", "127.0.0.1:8201")


class Sandbox(Exception):
    """Requested path escapes the configured root."""


def resolve(root_real, rel):
    """Resolve <root>/<rel> to a realpath that is PROVABLY under the root.
    Raises Sandbox on any escape ('..', absolute, symlink out). The realpath +
    prefix check is the actual guard — string checks alone are not enough."""
    rel = (rel or "").replace("\\", "/").lstrip("/")          # treat as relative, always
    target = os.path.realpath(os.path.join(root_real, rel))
    if target != root_real and not target.startswith(root_real + os.sep):
        raise Sandbox(rel)
    return target


def list_dirs(root_real, rel):
    target = resolve(root_real, rel)
    if not os.path.isdir(target):
        raise Sandbox(rel)
    subdirs, files_here = [], 0
    with os.scandir(target) as it:
        for e in it:
            try:
                if e.is_dir(follow_symlinks=False):
                    sub = os.path.join(e.path, "")
                    has_children = any(c.is_dir(follow_symlinks=False)
                                       for c in os.scandir(e.path))
                    nfiles = sum(1 for c in os.scandir(e.path)
                                 if c.is_file(follow_symlinks=False))
                    relpath = os.path.relpath(e.path, root_real).replace("\\", "/")
                    subdirs.append({"id": relpath, "label": e.name,
                                    "has_children": has_children, "files": nfiles})
                elif e.is_file(follow_symlinks=False):
                    files_here += 1
            except OSError:
                continue
    subdirs.sort(key=lambda d: d["label"].lower())
    cur = os.path.relpath(target, root_real).replace("\\", "/")
    return {"shape": "tree", "path": "" if cur == "." else cur,
            "files_here": files_here, "items": subdirs}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def _send(self, code, obj):
        body = json.dumps(obj, ensure_ascii=False).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        u = urlparse(self.path)
        if u.path != "/list_dirs":
            self._send(404, {"error": "not found"})
            return
        root_real = os.path.realpath(ROOT)
        rel = (parse_qs(u.query).get("path") or [""])[0]
        try:
            self._send(200, list_dirs(root_real, rel))
        except Sandbox:
            self._send(400, {"error": "path outside root", "path": rel})
        except Exception as e:  # noqa: BLE001 — surface as error, never as empty ok
            self._send(500, {"error": str(e)})


def main():
    if not ROOT or not os.path.isdir(ROOT):
        sys.exit("KV_FILES_ROOT must be set to an existing directory (the browse sandbox root)")
    host, port = LISTEN.rsplit(":", 1)
    srv = ThreadingHTTPServer((host, int(port)), Handler)
    print(f"knowvault browse service on {LISTEN} -> root {os.path.realpath(ROOT)}")
    srv.serve_forever()


if __name__ == "__main__":
    main()
