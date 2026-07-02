# LLD-01 — журнальная механика: скобки, ошибки-события, `event`, идемпотентность, периметр

Дата: 2026-07-02. Автор решений: Fable. Реализация: Sonnet 5 строго по этому
документу. Родитель: HLD-V1 §H2, §H4, §H6. Инварианты I-1..I-5 обязательны.

**Границы для исполнителя.** Sonnet НЕ решает сам: изменение canonicalJSON /
hashEnvelope сверх §1.1; новые kind сверх §1.2; изменения грамматики сверх §4;
ослабление приёмочных тестов §7; любая новая семантика permissions. Обнаружил
противоречие с кодом — фиксируй вопрос в agent/kalita и останавливайся на этом
куске (остальные продолжай).

---

## 1. Envelope и новые виды событий

### 1.1 `schema_v` в envelope
- Envelope получает поле `schema_v int` (omitempty). **Правило канонизации:
  поле сериализуется в hashEnvelope ТОЛЬКО при значении ≠ 0.** Все
  существующие события (schema_v отсутствует ≡ 0) остаются побитово теми же —
  hash-chain не трогается (I-1).
- Все события, вводимые этим LLD, пишутся с `schema_v: 1`.
- Fold-правило: движок обязан принимать события всех schema_v ≤ своей;
  неизвестный БОЛЬШИЙ schema_v при replay = ошибка старта (не тихий пропуск) —
  защита от запуска старого бинаря на новом журнале до LLD-03 (там появится
  штатный downgrade-механизм; до него — жёсткий отказ).

### 1.2 Новые kind (закрытый список этого LLD)
| kind | payload | назначение |
|---|---|---|
| `op.begin` | `{op_id, op_kind: "rewind"\|"cascade", entity, record_id}` | скобка многособытийной операции |
| `op.end` | `{op_id}` | закрытие скобки |
| `op.aborted` | `{op_id, reason}` | дописывается восстановлением при незакрытой скобке |
| `automation.failed` | `{rule, entity, record_id, action_kind, action_index, error, attempt}` | отказ побочного действия |
| `automation.abandoned` | `{rule, entity, record_id, action_kind, action_index, attempts}` | ретраи исчерпаны |
| `event.emitted` | `{name, payload}` | доменное событие DSL (§4) |
| `ingest.flagged` | `{entity, record_id, source, reasons[], score}` | детектор периметра (§6) |

`rule` везде = `"file:line"` (как в Basis сейчас). Никаких других kind этот
LLD не вводит.

## 2. Скобки op.*

- Пишутся ТОЛЬКО для многособытийных операций: rewind с >1 компенсацией;
  каскад fireActions с ≥1 действием; цепочка emits→подписчики. Одиночная
  мутация скобок не получает (нет шума).
- `op_id` = uuid v7, генерирует движок. **Вложенность запрещена:** движок
  держит текущий op-контекст (поле Engine, под mu); вторичные каскады
  (auto-transition → automation → …) пишутся в ТОТ ЖЕ op. Глубина цепочки
  каскада ограничена **8** (если в коде уже есть anti-loop лимит — взять
  меньший из двух и объединить механизмы, о расхождении — вопрос в agent/kalita).
- **Восстановление.** Replay при старте собирает set открытых op_id. По
  завершении replay для каждого незакрытого: (а) Append `op.aborted
  {op_id, reason: "unclosed after replay"}`; (б) пометка записи
  `Inconsistent: true` в проекции (НЕ в журнале — состояние выводимо);
  (в) createTask{Kind: TaskEscalation, Role: <approver-роль узла>,
  Action: "inconsistent"}. В режиме read-only (`kalita verify`) — только
  отчёт, никаких Append.
- Inconsistent-запись: мутации через API/MCP отклоняются с кодом
  `RECORD_INCONSISTENT` + fix_hint («supervisor must rewind or confirm»);
  снятие флага — действием супервизора `confirm_consistent` (журналируется
  как обычный act системной области; право — у роли approver).

## 3. Ошибки-события и ретраи (fireActions)

- Каждая ветка fireActions возвращает ошибку; при err ≠ nil → Append
  `automation.failed{attempt: n}`. **Ошибка самого этого Append не глотается:**
  лог процесса + инкремент счётчика метрики; паника недопустима.
- Ретраи: проекция `failedActions map[key]state`, key =
  `rule|record_id|action_index`; строится fold'ом failed/abandoned (успешное
  срабатывание того же key снимает состояние). Тик ретраит по экспоненте:
  1m, 2m, 4m, 8m, 16m; **cap = 5 попыток** → Append `automation.abandoned` +
  createTask оператору (approver-роль). Idempotency-ключ ретрая включает
  attempt: `<idemPrefix>|<i>|a<attempt>` — иначе ByIdemKey отвергнет повтор
  как дубль.
- CI-гейт: в internal/engine запрещён паттерн присваивания результата
  createTask/applyTransition в blank identifiers на путях automation
  (grep-гейт в CI-скрипте с явным allowlist-файлом для осознанных исключений).

## 4. Примитив `event` (грамматика + семантика)

### 4.1 Грамматика (аддитивно; коды ошибок — следующие свободные E0xx,
фактические номера Sonnet берёт по существующему реестру errors.go подряд)
```
event_decl    := "event" IDENT ":" INDENT field_decl+ DEDENT
                 # поля: scalar-типы и ref[...]; запрещены computed, serial,
                 # file, array[file] (E: "event payload field type")
emits_clause  := "emits" IDENT "(" [name "=" expr {"," ...}] ")"
                 # хвост transition-строки, после guard/requires
emit_action   := "emit" IDENT "(" [args] ")"          # действие automation
event_trigger := "on" "event" [IDENT "."] IDENT ":"   # триггер automation
perm_verb     := "emit" "[" ident_list "]"            # verb в permissions
```
Ошибки компилятора (fix_hint обязателен): unknown event; недопустимый тип
поля payload; **emits чужого пака** (эмитить можно только события,
объявленные в своём паке — инвариант владения фактом); подписка на
неустановленный пак/событие; дубль имени события в паке.

### 4.2 Семантика
- Каноническое имя: `<pack>.<Event>`; внутри пака допустима короткая форма.
- `emits` исполняется после успешного applyTransition, внутри op-скобок
  каскада; выражения аргументов вычисляются в контексте записи (те же правила,
  что args automation). Payload валидируется по схеме события (типы;
  ref — существование записи). Ошибка вычисления/валидации = отказ ВСЕГО
  перехода (emits — часть перехода, транзакционная семантика через скобки).
- Диспатч подписчиков: синхронно в том же каскаде, детерминированный порядок
  (sort by pack, file, line); каждый подписчик — отдельное действие с
  механикой §3 (failed/retry индивидуальны).
- MCP-тул `emit_event {name, payload, idempotency_key}`:
  проверка verb `emit [Name]` у роли актора через Permission Engine (I-2) →
  валидация payload → Append event.emitted (актор = вызыватель) → диспатч.
  Тул нужен inbound-коннекторам и bridge (H8).
- `event.emitted` не мутирует записей сам по себе — это факт; все изменения
  делают подписчики под своими правами.
- UI людям прямой emit не даётся (только переходы/automation/MCP-verb).

## 5. Идемпотентность

- **MCP: обязательна.** Все мутационные тулы получают поле
  `idempotency_key` (string, 8..128); отсутствие → INVALID_PARAMS c fix_hint.
  agent_runner и inbound.py обновляются в том же PR (uuid на попытку задачи,
  переиспользуемый при ретрае).
- **REST: мягкий режим.** Заголовок `Idempotency-Key` принимается и дедупится;
  отсутствие — поведение прежнее + response-заголовок
  `Kalita-Warning: no-idempotency-key`. SDK/встроенный UI генерируют ключ на
  каждое действие пользователя. Жёсткий режим — флаг узла
  `--require-idempotency` (в 1.0 выключен по умолчанию).

## 6. Периметр (контрактная часть; реализация в workers/)

- **Контракт prompt-сборки** (общая функция в workers/agent_runner):
  system-блок (роль+правила, фиксированная формулировка стражника) →
  task-блок → data-блоки строго вида
  `<data source="<entity/id|file>" trust="untrusted">…</data>`.
  Ответ хендлера — только JSON по схеме; всё прочее = ошибка задачи (fail_task
  с отчётом, НЕ молча).
- **Детектор** `score(text) -> (float 0..1, reasons[])`, словарь v1 (en+ru):
  императивы к assistant/system; "ignore/disregard previous|instructions";
  markdown/XML-имитация системных блоков; попытки tool-синтаксиса
  (`{"tool":`, `act(`, имена MCP-тулов); просьбы раскрыть промпт/ключи.
  Порог 0.5 → Append `ingest.flagged` + пометка source-записи (поле
  `suspicious: true` у Source в knowvault-паке — DSL-правка пака, не ядра).
  Детектор НЕ блокирует (false positives на юртекстах легитимны).
- Реализация детектора — таблица правил в коде воркера (не ядро!). Ядро знает
  только kind `ingest.flagged`.

## 7. Приёмочные тесты (фиксированы; ослаблять нельзя, добавлять можно)

1. `TestCascadeFailureIsJournaled` — notify на несуществующую роль →
   automation.failed в журнале; после 5 ретраев — abandoned + задача
   approver-роли. Ноль потерь молча.
2. `TestOpBracketsRecovery` — инъекция отказа store после op.begin →
   рестарт → op.aborted, запись inconsistent, задача создана, мутации
   записи отклоняются кодом RECORD_INCONSISTENT до confirm_consistent.
3. `TestRewindCrashConsistency` — kill между компенсациями rewind →
   replay → inconsistent, не тихий рассинхрон.
4. `TestEventEmitDispatch` — пак A emits, пак B `on event a.X` → подписчик
   сработал; его отказ → failed→retry→abandoned индивидуально.
5. `TestEmitPermissionDenied` — emit_event без verb → denied, журнал без
   event.emitted.
6. `TestEmitIdempotent` — повторный emit_event с тем же ключом → одно событие.
7. `TestEmitsForeignPackCompileError`, `TestEventPayloadTypeError` — коды +
   fix_hint.
8. `TestEmitsFailureAbortsTransition` — ошибка вычисления payload откатывает
   переход целиком.
9. `TestHoneypotDetector` — корпус testdata/honeypot: recall ≥ 0.9 на atk/,
   false-positive ≤ 0.1 на benign/.
10. `TestMockLLMCannotEscape` — мок-модель возвращает: self-approve,
    смену статуса напрямую, чтение чужой записи, несуществующее поле в
    схеме ответа → каждое отклонено (permission denied / schema reject),
    задача завершается fail с отчётом.
11. Golden: все существующие тесты и dogfood-паки проходят без правок
    (кроме добавления idempotency_key в MCP-вызовы) — I-1.
12. Bench: op-скобки + failed-проекция дают ≤5% деградации на профиле
    «пилотный микс» (методика H10).

## 8. Порядок реализации (PR-нарезка для Sonnet)

PR-1: schema_v в envelope + kind-константы + fold-заглушки (+тест 11).
PR-2: fireActions с ошибками, automation.failed/abandoned, retry-проекция,
      CI-grep-гейт (+тесты 1).
PR-3: op-скобки + recovery + inconsistent (+тесты 2,3).
PR-4: event: грамматика/compose/семантика/emits (+тесты 4,7,8).
PR-5: MCP emit_event + verb emit + идемпотентность MCP (+тесты 5,6) +
      обновление agent_runner/inbound.
PR-6: периметр: prompt-контракт, детектор, honeypot-корпус (+тесты 9,10).
Каждый PR: зелёный CI + отметка в agent/kalita. Зоны eventstore/permissions —
ревью Fable до merge.
