# LLD-03 — upgrade, эры, правила совместимости

Дата: 2026-07-02. Автор решений: Fable. Реализация: Sonnet 5 строго по
документу. Родитель: HLD-V1 §H3. Зависимость: LLD-01 §1.1 (schema_v).

**Границы для исполнителя.** Sonnet НЕ решает сам: формат manifest эры и
сцепку чекпоинтов; правила downgrade; семантику record.imported. Runbook-тексты
пишет по шаблону, финальную редакцию — Fable/фаундер.

---

## 1. Версия узла и защита от downgrade

- В бинарь зашивается semver (`kalita version` уже есть — источник один,
  ldflags). Новые события:
  - `node.upgraded {from, to, binary_sha256}` (schema_v: 1) — пишется после
    успешного upgrade;
  - `node.downgraded {from, to, forced: true}` — только через явный
    `--force-downgrade`.
- Правило старта: если `max(node.upgraded.to)` по журналу > версии бинаря →
  **отказ стартовать** с сообщением и подсказкой (либо новый бинарь, либо
  `--force-downgrade` с перечислением kind, которые будут проигнорированы).
  Форс пишет node.downgraded и продолжает: неизвестные kind при fold —
  пропускаются С ПОДСЧЁТОМ и итоговым warning (не молча — I-3).
- Fold-совместимость: бинарь N читает все события ≤N. Гейт — golden-журналы (§3).

## 2. `kalita upgrade` (docker-first)

Оркестрация v1 — для compose-деплоя (native — runbook, §5):
```
kalita upgrade --to <ver|latest> [--dry-run]
  1. preflight: journal head, verify (быстрый: хвост от последнего checkpoint),
     свободное место, версия PG
  2. backup: pg_dump в volume backups/ (имя = ts+from-ver); отказ при неудаче
  3. pull образа <ver>; сверка подписи релиза (cosign; ключ в репо)
  4. stop node (компose service) — PG остаётся
  5. start node:<ver> → replay → /readyz ожидание (timeout 10m)
  6. smoke: /healthz, /readyz, GET /api/meta, счётчик записей == до
  7. Append node.upgraded; отчёт оператору
  rollback (автоматический при провале 5-6): start node:<from>;
  журнал не мутировал — старый бинарь реплеит как было; отчёт FAILURE
```
- Upgrade НИЧЕГО не пишет в журнал до шага 7 (инвариант: провал = ноль следов,
  кроме бэкапа).
- `--dry-run`: шаги 1–3 + отчёт.

## 3. Golden-журналы (гейт fold-совместимости)

- `testdata/golden/<ver>/journal.jsonl` + `expected/` (снапшоты проекций:
  records по сущностям, tasks, approvals, actors — канонический JSON,
  отсортированный).
- Генератор `go run ./cmd/goldengen` (новый, прод-код не трогает): прогоняет
  фиксированный сценарий (создание, переходы, HITL, automation, undo,
  comment, file; emit — после LLD-01) на MemStore с фикс-клоком → дампит.
  Детерминизм генерации НЕ требуется: файл фиксируется один раз коммитом;
  гейт = replay зафиксированного файла. uuid в снапшотах expected/
  нормализуются при дампе и сравнении (map uuid → порядковый id#N в порядке
  первого появления) — сравнение стабильно без инъекции uuid-генератора в ядро.
- CI-гейт: текущий бинарь реплеит ВСЕ testdata/golden/*/journal.jsonl;
  проекции == expected. На release-tag добавляется новый golden от этой версии
  (Makefile-цель; коммитится).
- Первый golden снимается ДО начала реализации LLD-01 (фиксирует v0-поведение)
  — **это PR-0 всего релизного трека.**

## 4. Эры (деструктивные миграции как процедура)

- `kalita export --out era.jsonl`:
  строки `{entity, id, values, state_since}` + завершающий manifest
  `{era_id (uuid), source_final_seq, source_final_checkpoint_hash,
  def_version, node_id, exported_at}`. Перед export — Seal() (свежий
  checkpoint), его hash и уходит в manifest.
- Трансформация: оператор правит era.jsonl скриптом (шаблоны в runbook:
  переименование поля = jq-однострочник; смена типа = таблица преобразования;
  удаление = drop ключа).
- `kalita import --era era.jsonl --pack <новый пак>` в ПУСТОЙ журнал:
  1. событие `era.genesis {prev_era: manifest}` — **сцепка эр: hash финального
     чекпоинта старой эры внутри первого события новой** (история доказуема
     через разрушение);
  2. definition.applied (новый пак);
  3. по строке: `record.imported {values, origin: {era_id, id}}` — НОВЫЙ kind,
     НЕ record.created: провенанс различим, serial-счётчики восстанавливаются
     из максимумов, автоматизация on create НЕ срабатывает на импорте
     (осознанное решение — импорт не «создание»).
- `kalita era verify --old <старый pg|dump> --new <узел>`: чекпоинт старой ==
  prev_era нового; счётчики записей совпадают с манифестом.
- Старый журнал: архив (pg_dump в холодное хранение), узел старой эры
  останавливается навсегда.

## 5. Backup/restore и runbooks (тексты — по шаблону, приёмка — учением)

- `deploy/runbooks/`: `backup.md` (pg_dump cron + офлайн verify еженедельно),
  `restore.md` (чистый хост → compose → pg_restore → verify → readyz),
  `upgrade.md` (native-вариант: stop, swap binary, start, smoke, rollback),
  `era.md` (шаблоны трансформаций), `lockdown.md` (после LLD identity-части).
- Учение (однократно, до пилота): полный restore на чистой машине по
  runbook, по таймеру; результат — в docs (дата, время, находки).

## 6. Приёмочные тесты (фиксированы)

1. Golden v0 (PR-0) и все последующие реплеятся текущим бинарём; снапшоты
   совпадают.
2. `TestDowngradeRefusal`: журнал с node.upgraded(to=X+1) → бинарь X
   отказывает; `--force-downgrade` стартует, неизвестные kind посчитаны в
   warning, node.downgraded записан.
3. `TestUpgradeRollbackLeavesNoTrace` (интеграционный, docker): инъекция
   провала smoke → автоматический rollback, журнал побайтово прежний
   (кроме ничего), старый узел readyz.
4. `TestEraChain`: export → jq-переименование поля → import → era verify
   зелёный; подмена одного байта старого дампа → verify FAIL.
5. `TestImportedRecordsSemantics`: on create-automation не сработала;
   serial продолжился с max+1; origin сохранён.
6. Учение restore проведено и задокументировано (не-код гейт чек-листа DoD).

## 7. PR-нарезка

PR-0: goldenzgen + golden v0 + CI-гейт. **Делается ПЕРВЫМ, до LLD-01 PR-1.**
PR-1: версия/upgraded/downgrade-защита (+тест 2).
PR-2: export/import/era.genesis/record.imported/era verify (+тесты 4, 5).
PR-3: kalita upgrade оркестратор + smoke (+тест 3, docker-инт).
PR-4: runbooks + учение (не-код).
