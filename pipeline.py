#!/usr/bin/env python3
"""
Kalita Pipeline — AI-assisted development orchestrator
Читает sprints.yaml + context.json → генерирует промпты → запускает Codex
Один и тот же скрипт для всех проектов, меняются только артефакты.

Usage:
    python pipeline.py run          # запустить следующую задачу
    python pipeline.py run --all    # гнать до конца или до ошибки
    python pipeline.py inbox "текст замечания"   # добавить в inbox
    python pipeline.py status       # показать прогресс
"""

import subprocess
import json
import yaml
import requests
import os
import sys
import argparse
from datetime import datetime
from pathlib import Path

# ── Config ──────────────────────────────────────────────────────────────────

REPO_DIR      = Path(os.environ.get("REPO_DIR", "/repos/kalita"))
PLAN_DIR      = REPO_DIR / "plan"
CONTEXT_FILE  = PLAN_DIR / "context.json"
SPRINTS_FILE  = PLAN_DIR / "sprints.yaml"
ARCH_FILE     = REPO_DIR / "doc" / "ARCHITECTURE.md"
CLAUDE_MD     = REPO_DIR / "CLAUDE.md"

OPENROUTER_KEY  = os.environ["OPENROUTER_KEY"]
GITEA_URL       = os.environ.get("GITEA_URL", "http://nas-ip:3000")
GITEA_TOKEN     = os.environ.get("GITEA_TOKEN", "")
GITEA_REPO      = os.environ.get("GITEA_REPO", "me/kalita")
NTFY_URL        = os.environ.get("NTFY_URL", "http://nas-ip:8080")
NTFY_TOPIC      = os.environ.get("NTFY_TOPIC", "kalita-pipeline")

# OpenRouter: дешёвая быстрая модель для prompt building
PROMPT_MODEL    = "google/gemini-flash-1.5"
# Резервная если основная недоступна
PROMPT_MODEL_FB = "qwen/qwen-2.5-72b-instruct"

# ── Context helpers ──────────────────────────────────────────────────────────

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

def get_current_task(sprints: dict) -> tuple[dict | None, dict | None]:
    for sprint in sprints["sprints"]:
        for task in sprint["tasks"]:
            if not task.get("done", False):
                return sprint, task
    return None, None

# ── Notifications ────────────────────────────────────────────────────────────

def notify(message: str, title: str = "Kalita Pipeline"):
    try:
        requests.post(
            f"{NTFY_URL}/{NTFY_TOPIC}",
            data=message.encode(),
            headers={"Title": title, "Priority": "default"},
            timeout=5
        )
    except Exception:
        pass  # уведомления не должны ронять конвейер

# ── Prompt builder via OpenRouter ────────────────────────────────────────────

def build_codex_prompt(task: dict, sprint: dict, context: dict) -> str:
    """
    OpenRouter собирает точный промпт для Codex из:
    - намерения задачи (intent из sprints.yaml)
    - последних 3 summaries (что уже было сделано)
    - архитектурных решений (decisions)
    - inbox (твои замечания)
    - CLAUDE.md правил
    - контекста спринта (goal)
    """
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

    decisions_text = "\n".join(f"- {d}" for d in decisions) or "Нет зафиксированных решений"
    inbox_text     = "\n".join(f"- {i}" for i in inbox_items) or "Нет замечаний"

    system_prompt = f"""Ты технический ассистент который формулирует точные задачи для Codex CLI.
Codex — AI-кодер который читает промпт и вносит изменения в Go-проект.

Проект: Kalita — enterprise runtime для AI-агентов.

ПРАВИЛА ПРОЕКТА (из CLAUDE.md):
{claude_md}

АРХИТЕКТУРА:
{arch_md[:2000] if arch_md else "см. CLAUDE.md"}

Твоя задача: взять намерение и превратить его в точный исполнимый промпт для Codex.
Промпт должен:
- Указывать конкретные файлы и пакеты которые нужно создать/изменить
- Указывать сигнатуры интерфейсов и функций где это важно
- Явно называть что НЕ трогать
- Быть выполнимым за один сеанс (не слишком большим)
- Учитывать предыдущий прогресс и замечания
"""

    user_prompt = f"""ТЕКУЩИЙ СПРИНТ: {sprint['name']}
ЦЕЛЬ СПРИНТА: {sprint.get('goal', '')}

НАМЕРЕНИЕ ЗАДАЧИ {task['id']}:
{task['intent']}

ЧТО УЖЕ БЫЛО СДЕЛАНО (последние задачи):
{summaries_text}

АРХИТЕКТУРНЫЕ РЕШЕНИЯ (нельзя нарушать):
{decisions_text}

МОИ ЗАМЕЧАНИЯ (учти обязательно):
{inbox_text}

Сформулируй точный промпт для Codex. Только промпт, без объяснений."""

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

# ── Codex runner ─────────────────────────────────────────────────────────────

def run_codex(prompt: str, branch: str) -> tuple[str, bool]:
    """Запускает Codex, возвращает (summary, success)"""

    # Переключаемся на feature ветку
    subprocess.run(["git", "checkout", "-B", branch], cwd=REPO_DIR, check=True,
                   capture_output=True)

    print(f"\n{'='*60}")
    print(f"CODEX PROMPT:\n{prompt[:500]}...")
    print(f"{'='*60}\n")

    result = subprocess.run(
        ["codex", "--quiet", "--no-interactive", prompt],
        capture_output=True, text=True,
        cwd=REPO_DIR, timeout=600
    )

    success = result.returncode == 0

    # Пушим что есть даже если были ошибки
    subprocess.run(["git", "push", "origin", branch, "--force"],
                   cwd=REPO_DIR, capture_output=True)

    summary = result.stdout[-2000:] if result.stdout else result.stderr[-1000:]
    return summary, success

# ── Gitea PR ──────────────────────────────────────────────────────────────────

def create_pr(branch: str, title: str, body: str) -> str | None:
    if not GITEA_TOKEN:
        print("No GITEA_TOKEN — skipping PR creation")
        return None
    try:
        r = requests.post(
            f"{GITEA_URL}/api/v1/repos/{GITEA_REPO}/pulls",
            headers={"Authorization": f"token {GITEA_TOKEN}"},
            json={"title": title, "body": body, "head": branch, "base": "main"},
            timeout=10
        )
        pr = r.json()
        return pr.get("html_url")
    except Exception as e:
        print(f"PR creation failed: {e}")
        return None

# ── Claude review on milestone ────────────────────────────────────────────────

def run_claude_review(sprint: dict) -> str:
    """Запускает Claude CLI для ревью архитектуры после вехи"""
    print(f"\n🏁 MILESTONE: {sprint['name']} — запускаем Claude review...")

    review_prompt = f"""Ты архитектор проекта Kalita.

Только что завершён спринт: {sprint['name']}
Цель спринта: {sprint.get('goal', '')}

Задачи:
{chr(10).join(f"- {t['id']}: {t['intent'][:100]}" for t in sprint['tasks'])}

Выполни:
1. Проверь doc/ARCHITECTURE.md на актуальность — обнови если появились новые компоненты
2. Проверь что ни один из инвариантов из CLAUDE.md не нарушен в новом коде
3. Если нашёл архитектурные проблемы — добавь в doc/ARCHITECTURE.md секцию "## Known Issues Sprint {sprint['id']}"
4. Если всё хорошо — добавь в doc/ARCHITECTURE.md секцию "## Sprint {sprint['id']} — Done" с кратким описанием что добавлено

Будь конкретен и краток. Правь файлы напрямую.
"""

    result = subprocess.run(
        ["claude", "--print", review_prompt],
        capture_output=True, text=True,
        cwd=REPO_DIR, timeout=300
    )

    # Коммитим правки Claude
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
        print("✅ Все задачи выполнены!")
        notify("Все задачи выполнены! Kalita готова.", "Pipeline Done")
        return False

    task_id = task["id"]
    branch  = f"feature/task-{task_id}"
    print(f"\n▶ Задача {task_id}: {task['intent'][:80]}...")
    notify(f"Начинаю задачу {task_id}", "Pipeline")

    # Шаг 1: OpenRouter генерирует точный промпт
    print("→ Генерирую промпт через OpenRouter...")
    prompt = build_codex_prompt(task, sprint, context)

    # Шаг 2: Codex кодит
    print("→ Запускаю Codex...")
    summary, success = run_codex(prompt, branch)

    # Шаг 3: Сохраняем summary в context
    context["summaries"].append({
        "task_id":      task_id,
        "done_at":      datetime.now().isoformat(),
        "what_was_done": summary[:500],
        "problems":     "none" if success else summary[-300:],
        "files_changed": []  # codex может вернуть список
    })
    # Храним только последние 10 summaries
    context["summaries"] = context["summaries"][-10:]
    # Очищаем inbox — он был учтён в промпте
    context["inbox"] = []
    save_context(context)

    if not success:
        notify(f"❌ Задача {task_id} завершилась с ошибкой. Проверь логи.", "Pipeline Error")
        print(f"❌ Codex вернул ошибку для задачи {task_id}")
        return False

    # Шаг 4: Помечаем задачу done
    for sp in sprints_data["sprints"]:
        for t in sp["tasks"]:
            if t["id"] == task_id:
                t["done"] = True
    save_sprints(sprints_data)

    # Шаг 5: PR в Gitea
    pr_body = f"**Sprint {sprint['id']}:** {sprint['name']}\n\n**Task {task_id}**\n\n{summary[:1000]}"
    pr_url  = create_pr(branch, f"[{task_id}] {sprint['name']}", pr_body)

    msg = f"✅ Задача {task_id} готова"
    if pr_url:
        msg += f"\nPR: {pr_url}"
    notify(msg, "Pipeline")
    print(f"\n✅ Задача {task_id} выполнена")

    # Шаг 6: Проверяем веху
    sprint_tasks = next(s for s in sprints_data["sprints"] if s["id"] == sprint["id"])["tasks"]
    if sprint.get("milestone") and all(t.get("done") for t in sprint_tasks):
        review = run_claude_review(sprint)
        notify(f"🏁 Спринт {sprint['id']} завершён. Claude сделал ревью архитектуры.", "Milestone")

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
            status = "✅" if task.get("done") else "⬜"
            if task.get("done"):
                done += 1
            print(f"  {status} [{task['id']}] {task['intent'][:70]}...")

    print(f"\nПрогресс: {done}/{total} задач")
    if context.get("inbox"):
        print(f"\nInbox ({len(context['inbox'])} замечаний):")
        for item in context["inbox"]:
            print(f"  - {item}")

def cmd_inbox(message: str):
    context = load_context()
    context["inbox"].append(message)
    save_context(context)
    print(f"✅ Добавлено в inbox: {message}")

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
    run_p.add_argument("--all", action="store_true", help="Run all tasks sequentially")

    inbox_p = sub.add_parser("inbox")
    inbox_p.add_argument("message", help="Note to add to inbox")

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
