# G4 — план заморозки day-2 слоя (HUMAN GATE владельца)

> Подготовлено Forge к решению владельца. Ничего не коммитится и не пушится без
> явного разрешения. Факты: 92 dirty-записи (52 untracked + 40 modified/staged) на
> HEAD `v1.0-rc1+20` (51ab01c). Полная инвентаризация — в tmp/g4-status.txt.

## Секрет-аудит (проверено лично ведущим + инвентаризатором)

| Поверхность | Вердикт |
|---|---|
| `.env` (локальный dev DSN postgres://…@192.168.1.4) | покрыт .gitignore (строка `.env`) — НЕ коммитится |
| `integrations/dsh/cordis.yml`, `_strict.yml` | чисто: KALITA_TOKEN берётся из env, хардкода нет |
| `kalita-data/` (node.key), `.native/`, `tmp/`, `tmp-vault/`, `bin/`, `*.exe` | покрыты .gitignore |
| `_bk*/journal.jsonl` + MANIFEST | **журналы ноды с бизнес-данными — НЕ коммитить** (и добавить в .gitignore) |
| `agent/` (стратегия, коммерческие доки) | уже в .gitignore — не попадает |

## Классификация 92 записей

| Класс | Файлы | Решение |
|---|---|---|
| KALITA CORE (код+тесты) | internal/{pii (новый), engine: erase/pii/decide_guard + modified engine.go/records.go/workflow.go/op.go/automation_test, eventstore modified ×5, dsl modified ×4 + pii_test, api server.go + metrics, mcp tools.go + 5 тестов, cmd: anchor/backup/logs/trace/tsa/verify + main.go, goldengen store.go}, go.mod/go.sum | коммит 5 (один coherent kernel-коммит) |
| KALITA PACK | packs/ap/ (Cycle-001) | коммит 4 |
| INTEGRATION RUNTIME | integrations/dsh/ (cordis.yml, README, _strict.yml, bin/worker-скрипты), workers/author_demo.py | коммит 3 |
| EXTERNAL INFRASTRUCTURE | docker-compose.yml, deploy/prometheus/ | коммит 2 |
| DOCS | docs/forge/, docs/runbooks/, docs/compose/, docs/PROCESS-LIFECYCLE.md, 19 modified (Cycle-002 sync: README, SECURITY, V1-GATE, EVENT-STORE, DSL-*, HLD*, MCP-CONTRACT*, ROADMAP*, TYPE-SYSTEM, PORTAL-VISION, PLATFORM-STDLIB, DSL-CORE-LIB, LLD-10/18) | коммит 1 |
| DO-NOT-COMMIT | `_bk/`, `_bk2/`, `_bk3/` (журналы!), `_anchor-node.*`, `_anchor.txt`, `_livetest*.log` ×7, `_mcpbody.json`, `kalita-dsh-test.*`, `kalita-metrics.*`, `kalita.exe~` | в .gitignore |
| На усмотрение | `.dsh/` (repo-local скиллы kalita-worker/kalita-author — продуктовые инструкции для агентов) | предлагаю НЕ коммитить в G4 (harness-конфиг, прецедент forge-пресета — вне репо) |

## Предлагаемая серия (5 локальных коммитов; порядок гарантирует зелёный билд на каждом шаге)

1. `docs: Cycle-002 doc-truth sync + forge map + runbooks` — все docs-изменения.
2. `deploy: prometheus + compose wiring` — docker-compose.yml, deploy/prometheus/.
3. `integrations: DSH overlay + author demo worker` — integrations/, workers/author_demo.py.
4. `pack(ap): incoming invoices — executable model (Cycle-001)` — packs/ap/.
5. `kernel: PII seal/shred, erase subject, ops cmds (anchor/backup/TSA/trace), metrics, Decide guard revalidation (Cycle-003)` — internal/** + cmd/** + go.mod/go.sum.

До коммита 5: правка `.gitignore` (новые patterns ниже) и удаление/перемещение
артефактов из списка DO-NOT-COMMIT (сами файлы не трогаем — только не коммитим).

```gitignore
# Local node/runtime artifacts (day-2 drift)
/_bk*/
/_anchor-node.*
/_anchor.txt
/_livetest*.log
/_mcpbody.json
/kalita-dsh-test.*
/kalita-metrics.*
/kalita.exe~
```

## Gate-вопрос владельцу

1. Одобряете серию из 5 локальных коммитов (состав выше)? Варианты: (A) как предложено;
   (B) только core+pack+docs, integrations/deploy отдельно; (C) один squashed-коммит;
   (D) стоп — сначала посмотреть конкретные файлы.
2. `.dsh/` коммитить или оставить вне git?
3. Push — делаете сами (как раньше) или разрешаете форж-сессии локальный push?
