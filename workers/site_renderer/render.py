#!/usr/bin/env python3
"""Site renderer — a kalita worker agent that turns Published content into a
public website. It reads (SiteSettings, Page, Service, Article) over MCP with a
READ-ONLY role and writes static HTML to disk; the node owns the model and the
governance, this worker owns nothing but the HTML.

The `on schedule every 2m: agent SiteBot: regen()` automation in the content
pack fires a regen task; this worker also renders once at startup. Output is a
plain folder of static files — serve it with any web server, fully decoupled
from the node so the public site stays fast and needs no auth.

After rendering, an optional deploy step PUBLISHES the staging output to where
the site is actually served — closing "edit content -> live on hosting" without
the node ever facing the internet. Two targets cover the dominant cheap-hosting
patterns, both kept deliberately dumb (the renderer owns no credentials beyond
what the environment hands it):
  KSITE_DEPLOY=dir:/var/www/site   mirror into a web root  (the rsync/SFTP shape)
  KSITE_DEPLOY=git:/path/to/repo   commit + push a repo    (Netlify/Pages shape)

Env:
  KALITA_URL   (default http://127.0.0.1:8095)
  KALITA_TOKEN (or bootstrap: KALITA_BOOTSTRAP_SECRET + KALITA_WORKER_ID + KALITA_WORKER_ROLE=SiteBot)
  KSITE_OUT    staging output dir (default ./site)
  KSITE_DEPLOY optional publish target (dir:<path> | git:<path>); empty = stage only

Zero dependencies: stdlib only.
"""
import html
import json
import os
import re
import shutil
import subprocess
import sys
import time
import urllib.request
import urllib.error

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from bootstrap import get_token  # noqa: E402

NODE = os.environ.get("KALITA_URL", "http://127.0.0.1:8095")
TOKEN = get_token()
OUT = os.environ.get("KSITE_OUT", os.path.join(os.getcwd(), "site"))
DEPLOY = os.environ.get("KSITE_DEPLOY", "")


class ToolError(Exception):
    def __init__(self, payload):
        super().__init__(payload.get("message", str(payload)))
        self.payload = payload


def http(url, body=None, headers=None, method=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method,
                                 headers={"Content-Type": "application/json", **(headers or {})})
    with urllib.request.urlopen(req, timeout=60) as resp:
        return json.load(resp)


def tool(name, args=None):
    rpc = http(NODE + "/mcp", {
        "jsonrpc": "2.0", "id": 1, "method": "tools/call",
        "params": {"name": name, "arguments": args or {}},
    }, {"Authorization": f"Bearer {TOKEN}"})
    result = rpc["result"]
    decoded = json.loads(result["content"][0]["text"]) if result.get("content") else {}
    if result.get("isError"):
        raise ToolError(decoded)
    return decoded


def published(entity, sort=None):
    """Published records of a content entity, in the actor's read scope."""
    args = {"entity": entity, "where": "status = Published"}
    if sort:
        args["sort"] = sort
    return [r["values"] for r in (tool("query", args).get("records") or [])]


def settings():
    rows = tool("query", {"entity": "SiteSettings"}).get("records") or []
    return rows[0]["values"] if rows else {}


# --- tiny markdown -> HTML (paragraphs, headings, lists, bold, links) ---------
def md(text):
    if not text:
        return ""
    out, lst = [], False
    for raw in html.escape(text).split("\n"):
        line = raw.rstrip()
        if line.startswith("- "):
            if not lst:
                out.append("<ul>"); lst = True
            out.append(f"<li>{_inline(line[2:])}</li>")
            continue
        if lst:
            out.append("</ul>"); lst = False
        if not line:
            continue
        m = re.match(r"(#{1,3})\s+(.*)", line)
        if m:
            h = len(m.group(1))
            out.append(f"<h{h}>{_inline(m.group(2))}</h{h}>")
        else:
            out.append(f"<p>{_inline(line)}</p>")
    if lst:
        out.append("</ul>")
    return "\n".join(out)


def _inline(s):
    s = re.sub(r"\*\*(.+?)\*\*", r"<strong>\1</strong>", s)
    s = re.sub(r"\[(.+?)\]\((.+?)\)", r'<a href="\2">\1</a>', s)
    return s


def e(s):
    return html.escape(str(s or ""))


# --- page shell ---------------------------------------------------------------
def shell(cfg, nav, title, body):
    color = cfg.get("primary_color") or "#0b5"
    site = cfg.get("site_name") or "Site"
    links = "".join(f'<a href="{e(slug_href(p["slug"]))}">{e(p["title"])}</a>' for p in nav)
    foot = md(cfg.get("footer") or "")
    return f"""<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{e(title)} — {e(site)}</title>
<meta name="description" content="{e(cfg.get('tagline'))}">
<style>
  :root {{ --c: {e(color)}; }}
  * {{ box-sizing: border-box; }}
  body {{ margin: 0; font: 16px/1.6 -apple-system, Segoe UI, Roboto, sans-serif; color: #1a1a1a; }}
  header {{ display: flex; align-items: center; gap: 24px; padding: 18px 32px; border-bottom: 1px solid #eee; }}
  header .brand {{ font-weight: 700; font-size: 20px; color: var(--c); }}
  header nav {{ display: flex; gap: 18px; margin-left: auto; }}
  header nav a {{ color: #444; text-decoration: none; }}
  header nav a:hover {{ color: var(--c); }}
  main {{ max-width: 960px; margin: 0 auto; padding: 48px 32px; }}
  .hero {{ font-size: 40px; line-height: 1.15; font-weight: 800; margin: 0 0 16px; }}
  .grid {{ display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 20px; margin: 24px 0; }}
  .card {{ border: 1px solid #eee; border-radius: 12px; padding: 20px; }}
  .card h3 {{ margin: 0 0 6px; }}
  .price {{ color: var(--c); font-weight: 700; }}
  .muted {{ color: #777; }}
  footer {{ border-top: 1px solid #eee; padding: 32px; color: #777; margin-top: 48px; }}
  a {{ color: var(--c); }}
</style></head>
<body>
<header>
  <span class="brand">{e(site)}</span>
  <nav>{links}</nav>
</header>
<main>{body}</main>
<footer>{foot or e(site)}</footer>
</body></html>"""


def slug_href(slug):
    return "index.html" if slug in ("/", "", "home", "index") else f"{slug}.html"


def services_section(cfg):
    rows = published("Service", sort=["order"])
    if not rows:
        return ""
    cards = []
    for s in rows:
        price = f'<div class="price">from {e(s["price_from"])}</div>' if s.get("price_from") else ""
        cards.append(f"""<div class="card">
          <h3>{e(s.get('icon'))} {e(s['name'])}</h3>
          <p class="muted">{e(s.get('summary'))}</p>{price}
        </div>""")
    return f'<h2>Services</h2><div class="grid">{"".join(cards)}</div>'


def articles_section(cfg):
    rows = published("Article", sort=["-published_at"])
    if not rows:
        return ""
    items = []
    for a in rows[:6]:
        items.append(f"""<div class="card">
          <h3><a href="{e(slug_href('blog/' + a['slug']))}">{e(a['title'])}</a></h3>
          <p class="muted">{e(a.get('excerpt'))}</p>
        </div>""")
    return f'<h2>Latest</h2><div class="grid">{"".join(items)}</div>'


def regen():
    cfg = settings()
    pages = published("Page", sort=["menu_order"])
    nav = [p for p in pages if (p.get("menu_order") or 0) > 0]
    # rebuild staging from scratch so a page that was unpublished simply isn't
    # there this run — the deploy mirror then removes it from the live site too
    shutil.rmtree(OUT, ignore_errors=True)
    os.makedirs(OUT, exist_ok=True)

    home = next((p for p in pages if p["slug"] in ("/", "home", "index")), pages[0] if pages else None)
    written = 0
    for p in pages:
        body = ""
        if p.get("hero"):
            body += f'<h1 class="hero">{e(p["hero"])}</h1>'
        body += md(p.get("body"))
        if home and p["slug"] == home["slug"]:
            body += services_section(cfg) + articles_section(cfg)
        fname = slug_href(p["slug"])
        path = os.path.join(OUT, fname)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            f.write(shell(cfg, nav, p.get("title", ""), body))
        written += 1

    if not home:  # no pages yet — still emit a placeholder so the site exists
        with open(os.path.join(OUT, "index.html"), "w", encoding="utf-8") as f:
            f.write(shell(cfg, nav, cfg.get("site_name", "Site"),
                          '<p class="muted">No published pages yet.</p>'))
        written = 1

    note = f"{written} pages -> {OUT}"
    dep = deploy(OUT)
    if dep:
        note += f", published {dep}"
    print(f"[render] {note}", flush=True)
    return note


def _mirror(src, dst):
    """Make dst match src exactly: copy every file, then delete what's no longer
    in src (so an unpublished page disappears from the live site). A .git tree in
    dst is left untouched — it's the deploy mechanism, not site content."""
    os.makedirs(dst, exist_ok=True)
    keep = set()
    for root, _, files in os.walk(src):
        rel = os.path.relpath(root, src)
        outdir = dst if rel == "." else os.path.join(dst, rel)
        os.makedirs(outdir, exist_ok=True)
        for f in files:
            d = os.path.join(outdir, f)
            shutil.copy2(os.path.join(root, f), d)
            keep.add(os.path.normpath(d))
    for root, dirs, files in os.walk(dst):
        dirs[:] = [d for d in dirs if d != ".git"]
        for f in files:
            p = os.path.normpath(os.path.join(root, f))
            if p not in keep:
                os.remove(p)


def _git(repo, *args, check=True, capture=False):
    return subprocess.run(["git", "-C", repo, *args], check=check,
                          capture_output=capture, text=True)


def deploy(src):
    """Publish staged output to where the site is served. Returns a short label
    or None. dir: mirrors into a web root; git: commits and (if a remote exists)
    pushes — the Pages/Netlify pattern."""
    if not DEPLOY:
        return None
    kind, _, path = DEPLOY.partition(":")
    if kind == "dir":
        _mirror(src, path)
        return f"dir:{path}"
    if kind == "git":
        _mirror(src, path)
        _git(path, "add", "-A")
        if _git(path, "diff", "--cached", "--quiet", check=False).returncode != 0:
            _git(path, "commit", "-m", "site: publish")
            if _git(path, "remote", capture=True).stdout.strip():
                _git(path, "push")
            return f"git:{path} (committed)"
        return f"git:{path} (no change)"
    print(f"[warn] unknown KSITE_DEPLOY target {DEPLOY!r}; staged only", flush=True)
    return None


def main():
    me = tool("describe_system")
    print(f"site renderer up: pack={me.get('pack')} role={me.get('your_role')} out={OUT}")
    try:
        regen()  # initial build so the site exists immediately
    except (ToolError, urllib.error.URLError) as ex:
        print(f"[warn] initial render failed: {ex}")
    while True:
        try:
            tasks = tool("wait_for_task", {"timeout_sec": 25}).get("tasks") or []
            for task in tasks:
                try:
                    tool("take_task", {"task_id": task["id"]})
                except ToolError:
                    continue
                if task.get("action") == "regen":
                    try:
                        note = regen()
                        tool("complete_task", {"task_id": task["id"], "result": note})
                    except (OSError, ToolError, urllib.error.URLError) as ex:
                        tool("fail_task", {"task_id": task["id"], "reason": str(ex)})
                else:
                    tool("complete_task", {"task_id": task["id"], "result": "not a renderer task"})
        except (ToolError, urllib.error.URLError) as ex:
            print(f"[warn] {ex}; retrying in 5s")
            time.sleep(5)


if __name__ == "__main__":
    main()
