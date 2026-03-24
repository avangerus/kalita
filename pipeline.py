#!/usr/bin/env python3
"""
Kalita Pipeline — финальная версия
Читает sprints.yaml + context.json → промпт → codex exec → PR → automерж → GitHub Project

Usage:
    python pipeline.py run          # одна задача
    python pipeline.py run --all    # до конца
    python pipeline.py inbox "текст"
    python pipeline.py status
"""

import subprocess, json, yaml, requests, os, shlex, argparse, time
from datetime import datetime
from pathlib import Path

# ── Config ────────────────────────────────────────────────────────────────────

REPO_DIR       = Path(os.environ.get("REPO_DIR", str(Path.home() / "kalita")))
PLAN_DIR       = REPO_DIR / "plans"
CONTEXT_FILE   = PLAN_DIR / "context.json"
SPRINTS_FILE   = PLAN_DIR / "sprints.yaml"
ARCH_FILE      = REPO_DIR / "doc" / "ARCHITECTURE.md"
CLAUDE_MD      = REPO_DIR / "CLAUDE.md"

OPENROUTER_KEY = os.environ.get("OPENROUTER_KEY", "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")
GITHUB_TOKEN   = os.environ.get("GITEA_TOKEN", "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")
GITHUB_REPO    = os.environ.get("GITEA_REPO", "avangerus/kalita")
GITHUB_PROJECT = os.environ.get("GITHUB_PROJECT_ID", "PVT_kwHOAi_bD84BSoqx")
NTFY_URL       = os.environ.get("NTFY_URL", "")
NTFY_TOPIC     = os.environ.get("NTFY_TOPIC", "kalita-pipeline")

PROMPT_MODEL   = "qwen/qwen3.5-flash"
GH_HEADERS     = {"Authorization": f"token {GITHUB_TOKEN}", "Accept": "application/vnd.github.v3+json"}
GH_GQL         = {"Authorization": f"bearer {GITHUB_TOKEN}"}

# ── Helpers ───────────────────────────────────────────────────────────────────

def load_context() -> dict:
    if CONTEXT_FILE.exists():
        return json.loads(CONTEXT_FILE.read_text())
    return {"summaries": [], "decisions": [], "inbox": []}

def save_context(ctx: dict):
    CONTEXT_FILE.write_text(json.dumps(ctx, ensure_ascii=False, indent=2))

def load_sprints() -> dict:
    return yaml.safe_load(SPRINTS_FILE.read_text())

def save_sprints(data: dict):
    SPRINTS_FILE.write_text(yaml.dump(data, allow_unicode=True, default_flow_style=False))

def get_current_task(sprints: dict):
    for sprint in sprints["sprints"]:
        for task in sprint["tasks"]:
            if not task.get("done", False):
                return sprint, task
    return None, None

def notify(message: str, title: str = "Kalita Pipeline"):
    if not NTFY_URL:
        return
    try:
        requests.post(f"{NTFY_URL}/{NTFY_TOPIC}",
            data=message.encode(),
            headers={"Title": title, "Priority": "default"}, timeout=5)
    except Exception:
        pass

# ── Prompt builder ────────────────────────────────────────────────────────────

def build_codex_prompt(task: dict, sprint: dict, context: dict) -> str:
    if not OPENROUTER_KEY:
        return task["intent"]

    recent    = context["summaries"][-3:]
    decisions = context.get("decisions", [])
    inbox     = context.get("inbox", [])
    claude_md = CLAUDE_MD.read_text() if CLAUDE_MD.exists() else ""
    arch_md   = ARCH_FILE.read_text() if ARCH_FILE.exists() else ""

    summaries_text = "\n".join(
        f"[{s['task_id']}] {s['what_was_done']}"
        + (f"\n  Проблемы: {s['problems']}" if s.get('problems') and s['problems'] != 'none' else "")
        for s in recent
    ) or "Нет предыдущих изменений"

    system = (
        "Ты формулируешь точные задачи для Codex CLI (Go-проект Kalita).\n"
        f"CLAUDE.md:\n{claude_md[:2500]}\n\n"
        f"АРХИТЕКТУРА:\n{arch_md[:1500] if arch_md else 'см. CLAUDE.md'}\n\n"
        "Верни только промпт для Codex, без объяснений и markdown."
    )
    user = (
        f"СПРИНТ: {sprint['name']}\nЦЕЛЬ: {sprint.get('goal','')}\n\n"
        f"ЗАДАЧА {task['id']}:\n{task['intent']}\n\n"
        f"ЧТО СДЕЛАНО:\n{summaries_text}\n\n"
        f"РЕШЕНИЯ (нельзя нарушать):\n" + "\n".join(f"- {d}" for d in decisions) + "\n\n"
        f"ЗАМЕЧАНИЯ:\n" + ("\n".join(f"- {i}" for i in inbox) or "Нет")
    )

    try:
        r = requests.post("https://openrouter.ai/api/v1/chat/completions",
            headers={"Authorization": f"Bearer {OPENROUTER_KEY}", "Content-Type": "application/json"},
            json={"model": PROMPT_MODEL, "messages": [
                {"role": "system", "content": system},
                {"role": "user",   "content": user}
            ], "max_tokens": 1500}, timeout=30)
        return r.json()["choices"][0]["message"]["content"].strip()
    except Exception as e:
        print(f"OpenRouter error: {e} — используем raw intent")
        return task["intent"]

# ── Codex runner ──────────────────────────────────────────────────────────────

def get_changed_files() -> list[str]:
    r1 = subprocess.run(["git", "diff", "--name-only"], cwd=REPO_DIR, capture_output=True, text=True)
    r2 = subprocess.run(["git", "diff", "--cached", "--name-only"], cwd=REPO_DIR, capture_output=True, text=True)
    r3 = subprocess.run(["git", "diff", "--name-only", "HEAD~1", "HEAD"],
                        cwd=REPO_DIR, capture_output=True, text=True)
    files = set(r1.stdout.splitlines() + r2.stdout.splitlines() + r3.stdout.splitlines())
    return [f for f in sorted(files) if f.strip()]

def run_codex(prompt: str, branch: str):
    # Переключаемся на ветку
    subprocess.run(["git", "checkout", "-B", branch],
                   cwd=REPO_DIR, check=True, capture_output=True)

    print(f"\n{'='*60}")
    print(f"CODEX PROMPT:\n{prompt[:400]}...")
    print(f"{'='*60}\n")

    # codex exec — неинтерактивный режим, сам выходит после работы
    quoted = shlex.quote(prompt)
    result = subprocess.run(
        f"codex exec {quoted}",
        shell=True, cwd=REPO_DIR, timeout=1800
    )

    # Коммитим если что-то изменилось
    subprocess.run(["git", "add", "-A"], cwd=REPO_DIR, capture_output=True)
    commit = subprocess.run(
        ["git", "commit", "-m", f"feat: codex task {branch.split('/')[-1]}"],
        cwd=REPO_DIR, capture_output=True, text=True
    )

    changed = get_changed_files()
    success = bool(changed) or commit.returncode == 0

    # Пушим
    subprocess.run(["git", "push", "origin", branch, "--force"],
                   cwd=REPO_DIR, capture_output=True)

    summary = f"Изменены файлы:\n" + "\n".join(changed) if changed else "Нет изменений"
    return summary, success

# ── GitHub API ────────────────────────────────────────────────────────────────

def create_pr(branch: str, title: str, body: str) -> tuple[str, int]:
    """Создаёт PR, возвращает (url, pr_number)"""
    if not GITHUB_TOKEN or not GITHUB_REPO:
        return None, None
    try:
        r = requests.post(f"https://api.github.com/repos/{GITHUB_REPO}/pulls",
            headers=GH_HEADERS,
            json={"title": title, "body": body, "head": branch, "base": "main"},
            timeout=15)
        pr = r.json()
        url = pr.get("html_url")
        num = pr.get("number")
        if url:
            print(f"PR создан: {url}")
        else:
            print(f"PR: {pr.get('message', pr)}")
        return url, num
    except Exception as e:
        print(f"PR creation failed: {e}")
        return None, None

def merge_pr(pr_number: int) -> bool:
    """Мержит PR в main"""
    if not pr_number:
        return False
    try:
        r = requests.put(
            f"https://api.github.com/repos/{GITHUB_REPO}/pulls/{pr_number}/merge",
            headers=GH_HEADERS,
            json={"merge_method": "squash"},
            timeout=15)
        ok = r.status_code == 200
        if ok:
            print(f"PR #{pr_number} смержен в main")
        else:
            print(f"Merge failed: {r.json().get('message')}")
        return ok
    except Exception as e:
        print(f"Merge error: {e}")
        return False

def close_issue(task_id: str) -> bool:
    """Закрывает GitHub issue для задачи"""
    if not GITHUB_TOKEN or not GITHUB_REPO:
        return False
    try:
        # Ищем issue по task_id в заголовке
        r = requests.get(
            f"https://api.github.com/repos/{GITHUB_REPO}/issues",
            headers=GH_HEADERS,
            params={"state": "open", "per_page": 50},
            timeout=10)
        for issue in r.json():
            if f"[{task_id}]" in issue.get("title", ""):
                requests.patch(
                    f"https://api.github.com/repos/{GITHUB_REPO}/issues/{issue['number']}",
                    headers=GH_HEADERS,
                    json={"state": "closed"},
                    timeout=10)
                print(f"Issue #{issue['number']} закрыт")
                return True
    except Exception as e:
        print(f"Close issue error: {e}")
    return False

def move_project_card(issue_title: str, status: str):
    """Двигает карточку в GitHub Project (Backlog/In Progress/Done)"""
    if not GITHUB_PROJECT or not GITHUB_TOKEN:
        return
    try:
        # Находим item в проекте по title
        query = """
        query($project: ID!) {
          node(id: $project) {
            ... on ProjectV2 {
              items(first: 50) {
                nodes {
                  id
                  content { ... on Issue { title } }
                }
              }
              fields(first: 10) {
                nodes {
                  ... on ProjectV2SingleSelectField {
                    id name
                    options { id name }
                  }
                }
              }
            }
          }
        }"""
        r = requests.post("https://api.github.com/graphql",
            headers=GH_GQL,
            json={"query": query, "variables": {"project": GITHUB_PROJECT}},
            timeout=10)
        data = r.json().get("data", {}).get("node", {})

        # Находим поле Status и нужный option
        status_field = None
        status_option = None
        for field in data.get("fields", {}).get("nodes", []):
            if field.get("name") == "Status":
                status_field = field["id"]
                for opt in field.get("options", []):
                    if opt["name"].lower() == status.lower():
                        status_option = opt["id"]

        if not status_field or not status_option:
            return

        # Находим item
        for item in data.get("items", {}).get("nodes", []):
            content = item.get("content", {})
            if content and issue_title in content.get("title", ""):
                mutation = """
                mutation($project: ID!, $item: ID!, $field: ID!, $value: String!) {
                  updateProjectV2ItemFieldValue(input: {
                    projectId: $project, itemId: $item,
                    fieldId: $field, value: {singleSelectOptionId: $value}
                  }) { projectV2Item { id } }
                }"""
                requests.post("https://api.github.com/graphql",
                    headers=GH_GQL,
                    json={"query": mutation, "variables": {
                        "project": GITHUB_PROJECT, "item": item["id"],
                        "field": status_field, "value": status_option
                    }}, timeout=10)
                print(f"Карточка '{content['title']}' -> {status}")
                return
    except Exception as e:
        print(f"Project card error: {e}")

# ── Claude review on milestone ────────────────────────────────────────────────

def run_claude_review(sprint: dict) -> str:
    print(f"\nMILESTONE: {sprint['name']} — Claude review...")

    review_prompt = (
        f"Ты архитектор проекта Kalita. Завершён спринт: {sprint['name']}\n"
        f"Цель: {sprint.get('goal', '')}\n\n"
        f"Задачи:\n" + "\n".join(f"- {t['id']}: {t['intent'][:80]}" for t in sprint['tasks'])
        + f"\n\nВыполни:\n"
        "1. Обнови doc/ARCHITECTURE.md если появились новые компоненты\n"
        "2. Проверь инварианты из CLAUDE.md\n"
        f"3. Добавь секцию '## Sprint {sprint['id']} — Done' с кратким описанием\n"
        "Правь файлы напрямую."
    )

    subprocess.run(["claude", "--print", review_prompt],
                   cwd=REPO_DIR, timeout=300)

    subprocess.run(["git", "add", "doc/"], cwd=REPO_DIR, capture_output=True)
    subprocess.run(["git", "commit", "-m", f"docs: claude review sprint {sprint['id']}"],
                   cwd=REPO_DIR, capture_output=True)
    subprocess.run(["git", "push", "origin", "main"], cwd=REPO_DIR, capture_output=True)
    return "done"

# ── Main loop ─────────────────────────────────────────────────────────────────

def run_one_task() -> bool:
    sprints_data = load_sprints()
    context      = load_context()

    sprint, task = get_current_task(sprints_data)
    if not sprint:
        print("Все задачи выполнены!")
        notify("Все задачи выполнены! Kalita готова.", "Pipeline Done")
        return False

    task_id = task["id"]
    branch  = f"feature/task-{task_id}"
    title   = f"[{task_id}] {sprint['name']}"

    print(f"\nЗадача {task_id}: {task['intent'][:80]}...")
    notify(f"Начинаю задачу {task_id}", "Pipeline")

    # Двигаем карточку → In Progress
    move_project_card(title, "In Progress")

    # Генерируем промпт
    print("-> Промпт через OpenRouter...")
    prompt = build_codex_prompt(task, sprint, context)

    # Запускаем Codex
    print("-> Codex exec...")
    summary, success = run_codex(prompt, branch)

    # Сохраняем summary
    context["summaries"].append({
        "task_id":       task_id,
        "done_at":       datetime.now().isoformat(),
        "what_was_done": summary[:500],
        "problems":      "none" if success else "no changes",
        "files_changed": []
    })
    context["summaries"] = context["summaries"][-10:]
    context["inbox"] = []
    save_context(context)

    if not success:
        notify(f"Задача {task_id} — нет изменений.", "Pipeline Warning")
        print(f"Codex не внёс изменений. Пропускаем.")
        return False

    # Помечаем done
    for sp in sprints_data["sprints"]:
        for t in sp["tasks"]:
            if t["id"] == task_id:
                t["done"] = True
    save_sprints(sprints_data)

    # PR → merge → close issue
    pr_body = f"**Sprint {sprint['id']}:** {sprint['name']}\n\n**Task {task_id}**\n\n{summary}"
    pr_url, pr_num = create_pr(branch, title, pr_body)

    # Пауза чтобы GitHub обработал PR
    if pr_num:
        time.sleep(3)
        merged = merge_pr(pr_num)
        if merged:
            # Синхронизируем локальный main
            subprocess.run(["git", "checkout", "main"], cwd=REPO_DIR, capture_output=True)
            subprocess.run(["git", "pull", "origin", "main"], cwd=REPO_DIR, capture_output=True)

    close_issue(task_id)

    # Двигаем карточку → Done
    move_project_card(title, "Done")

    msg = f"Задача {task_id} готова"
    if pr_url:
        msg += f"\nPR: {pr_url}"
    notify(msg, "Pipeline")
    print(f"\nЗадача {task_id} выполнена и смержена в main")

    # Проверяем веху → Claude review
    sprint_obj = next(s for s in sprints_data["sprints"] if s["id"] == sprint["id"])
    if sprint.get("milestone") and all(t.get("done") for t in sprint_obj["tasks"]):
        run_claude_review(sprint)
        notify(f"Спринт {sprint['id']} завершён. Claude сделал ревью.", "Milestone")

    return True

# ── Commands ──────────────────────────────────────────────────────────────────

def cmd_status():
    sprints_data = load_sprints()
    context      = load_context()
    total = done = 0
    for sprint in sprints_data["sprints"]:
        print(f"\nСпринт {sprint['id']}: {sprint['name']}")
        for task in sprint["tasks"]:
            total += 1
            if task.get("done"):
                done += 1
            icon = "✅" if task.get("done") else "⬜"
            print(f"  {icon} [{task['id']}] {task['intent'][:70]}...")
    print(f"\nПрогресс: {done}/{total} задач")
    if context.get("inbox"):
        print(f"\nInbox ({len(context['inbox'])} замечаний):")
        for item in context["inbox"]:
            print(f"  - {item}")

def cmd_inbox(message: str):
    context = load_context()
    context["inbox"].append(message)
    save_context(context)
    print(f"Добавлено в inbox: {message}")

def cmd_run(run_all: bool):
    if run_all:
        while run_one_task():
            pass
    else:
        run_one_task()

# ── Entry point ───────────────────────────────────────────────────────────────

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Kalita Pipeline")
    sub    = parser.add_subparsers(dest="command")

    run_p = sub.add_parser("run")
    run_p.add_argument("--all", action="store_true")

    inbox_p = sub.add_parser("inbox")
    inbox_p.add_argument("message")

    sub.add_parser("status")

    args = parser.parse_args()

    if args.command == "run":
        cmd_run(args.all)
    elif args.command == "inbox":
        cmd_inbox(args.message)
    elif args.command == "status":
        cmd_status()
    else:
        parser.print_help()