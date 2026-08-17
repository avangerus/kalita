# Process Lifecycle — как Master Goal-цикл живёт в калите

Master Goal требует жизненный цикл процесса:
Business Intent → Analysis → Proposed Process → Validation → Simulation/Test →
Approval → Execution → Monitoring → Completion → Outcome Evaluation → Improvement.
Ниже — какой механизм калиты реализует каждый шаг (всё уже построено).

| Шаг цикла | Механизм калиты | Где |
|---|---|---|
| **Business Intent** | Естественно-языковое описание → агент `kalita-author` (DSH-скилл) | `.dsh/skills/kalita-author` |
| **Analysis** | `describe_system` + `search_perimeter` (знания компании) + заземление на эталонные паки | MCP-тулы |
| **Proposed Process** | `compose_pack` / `get_grammar`+`validate_dsl` → формальная исполнимая модель (DSL), не текст | `internal/dsl` |
| **Validation** | Закрытая грамматика: дрейф не компилируется (E-коды с fix_hint); `kalita check` | `internal/dsl` |
| **Simulation/Test** | **Shadow mode**: `kalita shadow enable --role R` — мутации роли уходят в `MutationProposed` и применяются только после человеческого решения; безопасный прогон нового процесса на живом журнале | `internal/engine/shadow.go` |
| **Approval** | `propose_change` паркуется → HITL-подпись (WebAuthn/passkey, `--require-human-signature`) → `definition.applied`; миграционный план виден подписанту | `internal/engine/proposals.go` |
| **Execution** | Workflow-движок: состояния/переходы, задачи с lease+TTL, automation (schedule/event/stuck), каскады, computed-поля | `internal/engine` |
| **Monitoring** | `/metrics` (Prometheus) + `/healthz`+`/readyz` + `deploy/prometheus/alerts.yml` + журнал как единственная правда | `internal/api/metrics.go` |
| **Completion** | Состояния-терминалы, `complete_task`, артефакты (records/comments/journal) | `internal/engine` |
| **Outcome Evaluation** | `kalita report --since 7d` — чистый fold по журналу («неделя в цифрах»), `kalita egress-report` | `cmd/kalita/report.go` |
| **Improvement** | Процессы **versioned**: любое изменение = новый `propose_change` (def_version растёт, additive-only), выполняющиеся экземпляры не ломаются (переходы добавляются, не удаляются); эры — для необратимых миграций | `internal/engine/proposals.go`, `cmd/kalita/migrate.go` |

## Версионирование и не-повреждение выполняющихся экземпляров

- `propose_change` принимает только **аддитивные** изменения (`only additive changes are allowed`) —
  удаление сущности отклоняется; действующие записи переживают обновление пака.
- Каждое применение бампает `def_version`; события журнала несут `def_version` на момент записи.
- Необратимая миграция — отдельная процедура **эры** (`kalita export/import/era verify`), с доказанным сцеплением генезиса.

## Масштабирование и performance-базис

- **Вертикаль**: одна нода = один процесс + Postgres; журнал immutable, проекция in-memory (replay при старте).
- **Replay-базис**: `TestReplayBudget1M` — 1M событий укладывается в бюджет 5с (гейт); бенчмарки `BenchmarkPassportP3Replay100k/1M`.
- **Горизонталь**: федерация нод через `workers/bridge` (pull-журнал → `emit_event`, exactly-once, LLD-21) — узлы общаются событиями, не RPC.
- **Один писатель на журнал**: сериализация аппендов advisory-локом; несколько реплик чтения — через bridge/read_events.
