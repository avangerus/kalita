# POST_RELEASE — v1.0.0-rc.2 (staff-only pilot)

> После публикации RC. Release baseline: commit `359da54`, tag `v1.0.0-rc.2`
> (main pushed to origin). Артефакты: Docker-образ `kalita:v1.0.0-rc.2`
> (build-arg VERSION=v1.0.0-rc.2; внутри образа `kalita version` = v1.0.0-rc.2),
> бинарник bin/kalita.exe (sha256 `3FC5E08CE0D1788FBC747FD776C76AB7E0D49289829A32ECDFC0741DADC3A2D9`).
> Version identity проверена: CLI / образ / MCP serverInfo — все `v1.0.0-rc.2`.

## Known limitations (выпущено осознанно)

- **Staff-only**: consumer-auth отсутствует (accepted exception владельца).
- **Prompt-injection периметр** — трипвайр (детектор + flag_ingest), не блокирует;
  бэкстоп — ABAC (LIM-8).
- **Webhook egress allowlist** — IDN/punycode-гомограф и DNS-rebinding не закрыты
  (pilot-grade, LIM-9): держите ASCII-домены в KALITA_EGRESS_DOMAINS.
- **PII-AAD** — закрыт для новых данных ({subject, entity.field}); старые
  subject-only шифртексты открываются fallback (SEC-4).
- **on update-триггеры** не срабатывают после act-переходов (LIM-1).
- **Federation** — уровень воркеров; демо не прогнано E2E (LIM-6).
- **Upgrade-publish** (registry/cosign) — post-release (OPS-3); апгрейд = пересборка
  образа `docker compose up --build` до настройки публикации.
- **EVENT-STORE §4** (подписи агентов над мутациями) — open question владельца.
- Python-воркеры в CI — джоба в дереве, активна на push (уже), live-node тесты
  воркеров — отдельная джоба после пилота.
- Single-node, in-memory проекции, один мутекс: вертикальный масштаб, не hyperscale.

## Monitoring targets (что смотреть на пилоте)

- `/healthz` (liveness), `/readyz` (pack loaded) — alert при не-200.
- `/metrics` (Prometheus): `DecideFallbackSynthetic` (fail-open edge Decide),
  `AutomationJournalFailures` (незажурналированные ошибки каскадов), authfail/rate-limit.
- Лог-сигналы: `webhook: … denied` (egress-отказ), `webhook runner: settled N`,
  `--insecure-dev-auth` WARNING (никогда в проде), «KALITA_PG_DSN is set from a plain
  environment variable» (перевести на KALITA_SECRETS_FILE).
- `kalita egress-report --since 7d` — еженедельно: аномалии = вызовы вне whitelist.
- `kalita verify --pub node.pub` — еженедельно; `kalita backup` + `backup verify` — по
  расписанию; ANCHOR хранить вне узла.
- Рост журнала: `kalita report`/passport-бенчмарки; Replay-1M budget 5s (реально ~1.3s).

## Rollback criteria (когда RC сворачиваем)

- Целостность: `verify`/`backup verify` падает или ANCHOR не сходится → стоп, не чинить молча.
- Безопасность: найден P0/P1-обход ABAC/HITL/журнала → стоп + отзыв токенов (lockdown).
- Интеграция: egress-доставка теряет эффекты (не просто ретраи) → стоп.
- Доступность: нештатный рост `AutomationJournalFailures`/retry-штормов → расследование.
- Откат = перезапуск с предыдущего образа; журнал append-only, данные не теряются.

## Критерии перехода RC → v1.0.0 (финальный релиз)

1. Пилот отработал ≥ 2 недель на реальном процессе без BLOCKER-инцидентов.
2. Egress-сценарий прошёл с реальной системой Y (не mock) с идемпотентной обработкой.
3. Ни одного нового P0/P1 из monitoring (выше) без фикса.
4. Решение владельца по EVENT-STORE §4 (подписи агентов) — понизить док или реализовать.
5. Владелец явно одобряет финальный `git tag v1.0.0` на том же/новом baseline.
6. Полный release suite зелёный на финальном baseline (vet + 348 PASS / 12 SKIP / 0 FAIL
   или больше при добавлении тестов).

## First pilot feedback questions

- Легко ли было установить по deploy/QUICKSTART без участия разработчика?
- Где «система X → Kalita → Y» расходилась с ожиданиями (поля/статусы/роли)?
- Какие утверждения хочется сделать быстрее/медленнее (лестница порогов)?
- Что агент должен был сделать, но не мог (deny-матрица слишком строгая)?
- Какие бизнес-вопросы пришлось решать вручную (кандидаты в business framework)?
