#!/usr/bin/env python3
"""
Kalita Pipeline — AI-assisted development orchestrator
Читает sprints.yaml + context.json → генерирует промпты → запускает Codex

Usage:
    python pipeline.py run          # запустить следующую задачу
    python pipeline.py run --all    # гнать до конца или до ошибки
    python pipeline.py inbox "текст замечания"
    python pipeline.py status
"""

import subprocess
import json
import yaml
import requests
import os
import shlex
import argparse
from datetime import datetime
from pathlib import Path

# ── Config ───────────────────────────────────────────────────────────────────

REPO_DIR      = Path(os.environ.get("REPO_DIR", str(Path.home() / "kalita")))
PLAN_DIR      = REPO_DIR / "plans"
CONTEXT_FILE  = PLAN_DIR / "context.json"
SPRINTS_FILE  = PLAN_DIR / "sprints.yaml"
ARCH_FILE     = REPO_DIR / "doc" / "ARCHITECTURE.md"
CLAUDE_MD     = REPO_DIR / "CLAUDE.md"

OPENROUTER_KEY = os.environ.get("OPENROUTER_KEY", "")
GITHUB_TOKEN   = os.environ.get("GITEA_TOKEN", "")
GITHUB_REPO    = os.environ.get("GITEA_REPO", "")
NTFY_URL       = os.environ.get("NTFY_URL", "")
NTFY_TOPIC     = os.environ.get("NTFY_TOPIC", "kalita-pipeline")

PROMPT_MODEL    = "qwen/qwen3.5-plus-02-15"
PROMPT_MODEL_FB = "qwen/qwen-2.5-72b-instruct"

# ── Context helpers ───────────────────────────────────────────────────────────

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

# ── Notifications ─────────────────────────────────────────────────────────────

def notify(message: str, title: str = "Kalita Pipeline"):
    if not NTFY_URL:
        return
    try:
        requests.post(
            f"{NTFY_URL}/{NTFY_TOPIC}",
            data=message.encode(),
            headers={"Title": title, "Priority": "default"},
            timeout=5
        )
    except Exception:
        pass

# ── Prompt builder ────────────────────────────────────────────────────────────

def build_codex_prompt(task: dict, sprint: dict, context: dict) -> str:
    if not OPENROUTER_KEY:
        print("OPENROUTER_KEY не задан — используем raw intent")
        return task["intent"]

    recent_summaries = context["summaries"][-3:]
    inbox_items      = context.get("inbox", [])
    decisions        = context.get("decisions", [])
    claude_md        = CLAUDE_MD.read_text() if CLAUDE_MD.exists() else ""
    arch_md          = ARCH_FILE.read_text() if ARCH_FILE.exists() else ""

    summaries_text = "\n".join([
        f"[{s['task_id']}] {s['what_was_done']}"
        + (f"\n  Проблемы: {s['problems']}" if s.get('problems') and s['problems'] != 'none' else "")
        for s in recent_summaries
    ]) or "Нет предыдущих изменений"

    decisions_text = "\n".join(f"- {d}" for d in decisions) or "Нет решений"
    inbox_text     = "\n".join(f"- {i}" for i in inbox_items) or "Нет замечаний"

    system_prompt = (
        "Ты технический ассистент который формулирует точные задачи для Codex CLI.\n"
        "Codex — AI-кодер который читает промпт и вносит изменения в Go-проект.\n"
        "Проект: Kalita — enterprise runtime для AI-агентов.\n\n"
        f"ПРАВИЛА ПРОЕКТА (из CLAUDE.md):\n{claude_md[:3000]}\n\n"
        f"АРХИТЕКТУРА:\n{arch_md[:2000] if arch_md else 'см. CLAUDE.md'}\n\n"
        "Верни только текст промпта для Codex, без объяснений."
    )

    user_prompt = (
        f"СПРИНТ: {sprint['name']}\n"
        f"ЦЕЛЬ: {sprint.get('goal', '')}\n\n"
        f"ЗАДАЧА {task['id']}:\n{task['intent']}\n\n"
        f"ЧТО УЖЕ СДЕЛАНО:\n{summaries_text}\n\n"
        f"РЕШЕНИЯ (нельзя нарушать):\n{decisions_text}\n\n"
        f"МОИ ЗАМЕЧАНИЯ:\n{inbox_text}"
    )

    try:
        response = requests.post(
            "https://openrouter.ai/api/v1/chat/completions",
            headers={
                "Authorization": f"Bearer {OPENROUTER_KEY}",
                "Content-Type": "application/json"
            },
            json={
                "model": PROMPT_MODEL,
                "messages": [
                    {"role": "system", "content": system_prompt},
                    {"role": "user",   "content": user_prompt}
                ],
                "max_tokens": 1500
            },
            timeout=30
        )
        return response.json()["choices"][0]["message"]["content"].strip()
    except Exception as e:
        print(f"OpenRouter error: {e}, falling back to raw intent")
        return task["intent"]

# ── Codex runner ──────────────────────────────────────────────────────────────

def get_git_changes() -> str:
    """Возвращает список изменённых файлов через git"""
    r1 = subprocess.run(
        ["git", "diff", "--name-only"],
        cwd=REPO_DIR, capture_output=True, text=True
    )
    r2 = subprocess.run(
        ["git", "diff", "--cached", "--name-only"],
        cwd=REPO_DIR, capture_output=True, text=True
    )
    r3 = subprocess.run(
        ["git", "diff", "--name-only", "HEAD~1", "HEAD"],
        cwd=REPO_DIR, capture_output=True, text=True
    )
    files = set(
        r1.stdout.strip().splitlines() +
        r2.stdout.strip().splitlines() +
        r3.stdout.strip().splitlines()
    )
    return "\n".join(sorted(files))

def run_codex(prompt: str, branch: str):
    """Запускает Codex интерактивно, возвращает (summary, success)"""

    # Переключаемся на feature ветку
    subprocess.run(
        ["git", "checkout", "-B", branch],
        cwd=REPO_DIR, check=True, capture_output=True
    )

    print(f"\n{'='*60}")
    print(f"CODEX PROMPT:\n{prompt[:500]}...")
    print(f"{'='*60}\n")
    print("Codex запущен. Работает...\n")

    # Пишем промпт в файл — Codex умеет читать из stdin или файла
    prompt_file = Path("/tmp/kalita_prompt.txt")
    prompt_file.write_text(prompt, encoding="utf-8")

    # Запускаем Codex напрямую в терминале — он интерактивный
    # Промпт передаём как аргумент через shlex.quote
    quoted = shlex.quote(prompt)
    result = subprocess.run(
        f"codex --auto-approve {quoted}",
        shell=True,
        cwd=REPO_DIR,
        timeout=1800
        # НЕТ capture_output — Codex видит терминал и работает
    )

    # Определяем успех по git изменениям, не по exit code
    changes = get_git_changes()
    success = bool(changes.strip())

    if success:
        summary = f"Изменены файлы:\n{changes}"
        # Добавляем все изменения в коммит если Codex не закоммитил
        subprocess.run(["git", "add", "-A"], cwd=REPO_DIR, capture_output=True)
        subprocess.run(
            ["git", "commit", "--allow-empty", "-m", f"feat: task via codex"],
            cwd=REPO_DIR, capture_output=True
        )
    else:
        summary = "Codex не внёс изменений"

    # Пушим в любом случае
    subprocess.run(
        ["git", "push", "origin", branch, "--force"],
        cwd=REPO_DIR, capture_output=True
    )

    return summary, success

# ── GitHub PR ─────────────────────────────────────────────────────────────────

def create_pr(branch: str, title: str, body: str):
    if not GITHUB_TOKEN or not GITHUB_REPO:
        print("GITEA_TOKEN или GITEA_REPO не заданы — пропускаем PR")
        return None
    try:
        r = requests.post(
            f"https://api.github.com/repos/{GITHUB_REPO}/pulls",
            headers={
                "Authorization": f"token {GITHUB_TOKEN}",
                "Accept": "application/vnd.github.v3+json"
            },
            json={
                "title": title,
                "body":  body,
                "head":  branch,
                "base":  "main"
            },
            timeout=15
        )
        pr = r.json()
        url = pr.get("html_url")
        if url:
            print(f"PR создан: {url}")
        else:
            print(f"PR ответ: {pr.get('message', pr)}")
        return url
    except Exception as e:
        print(f"PR creation failed: {e}")
        return None

# ── Claude review on milestone ────────────────────────────────────────────────

def run_claude_review(sprint: dict) -> str:
    print(f"\nMILESTONE: {sprint['name']} — запускаем Claude review...")

    review_prompt = (
        f"Ты архитектор проекта Kalita.\n\n"
        f"Завершён спринт: {sprint['name']}\n"
        f"Цель: {sprint.get('goal', '')}\n\n"
        "Задачи:\n"
        + "\n".join(f"- {t['id']}: {t['intent'][:100]}" for t in sprint['tasks'])
        + "\n\nВыполни:\n"
        "1. Проверь doc/ARCHITECTURE.md — обнови если нужно\n"
        "2. Проверь инварианты из CLAUDE.md\n"
        f"3. Добавь секцию '## Sprint {sprint['id']} — Done' с описанием\n"
        "Правь файлы напрямую."
    )

    result = subprocess.run(
        ["claude", "--print", review_prompt],
        capture_output=True, text=True,
        cwd=REPO_DIR, timeout=300
    )

    subprocess.run(["git", "add", "doc/"], cwd=REPO_DIR, capture_output=True)
    subprocess.run(
        ["git", "commit", "-m", f"docs: claude review after sprint {sprint['id']}"],
        cwd=REPO_DIR, capture_output=True
    )
    subprocess.run(["git", "push", "origin", "main"], cwd=REPO_DIR, capture_output=True)

    return result.stdout

# ── Main pipeline step ────────────────────────────────────────────────────────

def run_one_task():
    sprints_data = load_sprints()
    context      = load_context()

    sprint, task = get_current_task(sprints_data)
    if not sprint:
        print("Все задачи выполнены!")
        notify("Все задачи выполнены! Kalita готова.", "Pipeline Done")
        return False

    task_id = task["id"]
    branch  = f"feature/task-{task_id}"
    print(f"\nЗадача {task_id}: {task['intent'][:80]}...")
    notify(f"Начинаю задачу {task_id}", "Pipeline")

    print("-> Генерирую промпт через OpenRouter...")
    prompt = build_codex_prompt(task, sprint, context)

    print("-> Запускаю Codex...")
    summary, success = run_codex(prompt, branch)

    context["summaries"].append({
        "task_id":       task_id,
        "done_at":       datetime.now().isoformat(),
        "what_was_done": summary[:500],
        "problems":      "none" if success else "Codex не внёс изменений",
        "files_changed": []
    })
    context["summaries"] = context["summaries"][-10:]
    context["inbox"] = []
    save_context(context)

    if not success:
        notify(f"Задача {task_id} — Codex не внёс изменений.", "Pipeline Warning")
        print(f"Codex не внёс изменений для задачи {task_id}")
        print("Проверь промпт или запусти вручную: codex '<промпт>'")
        return False

    # Помечаем done
    for sp in sprints_data["sprints"]:
        for t in sp["tasks"]:
            if t["id"] == task_id:
                t["done"] = True
    save_sprints(sprints_data)

    pr_body = f"**Sprint {sprint['id']}:** {sprint['name']}\n\n**Task {task_id}**\n\n{summary[:1000]}"
    pr_url  = create_pr(branch, f"[{task_id}] {sprint['name']}", pr_body)

    msg = f"Задача {task_id} готова"
    if pr_url:
        msg += f"\nPR: {pr_url}"
    notify(msg, "Pipeline")
    print(f"\nЗадача {task_id} выполнена")

    sprint_obj   = next(s for s in sprints_data["sprints"] if s["id"] == sprint["id"])
    sprint_tasks = sprint_obj["tasks"]
    if sprint.get("milestone") and all(t.get("done") for t in sprint_tasks):
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
            print(f"  {'OK' if task.get('done') else '--'} [{task['id']}] {task['intent'][:70]}...")

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
