# LLD-04-ABI — WASM escape hatch: контракт компонента

Дата: 2026-07-02. Автор решений: Fable — этот документ фиксирует ТОЛЬКО
контракт (грамматика, ABI, лимиты, события). Интеграция в evaluator, кэш,
загрузка из blob — реализация Sonnet 5 по этому контракту (R3).
Родитель: HLD-V1 §H5.

**Границы для исполнителя.** Менять нельзя: грамматику §1, ABI §2, лимиты §3
(значения — можно выносить в конфиг узла, дефолты фиксированы), список
запретов §4.

---

## 1. Грамматика (аддитивно)

```
component_decl := "component" IDENT ":" INDENT
                    "wasm" "hash" "=" SHA256HEX
                    "in" "{" field_decl {"," field_decl} "}"
                    "out" type
                  DEDENT
```
- Типы in/out: только scalar-типы DSL (money, decimal, int, string, bool,
  date, datetime, duration, percent, enum-литералы как string). Запрещены:
  ref, file, array[*] — компонент не видит граф данных (I-2).
- Использование: ТОЛЬКО в выражениях computed и в guard-условиях workflow:
  `total: money computed = tax_calc(net, rate)`. НЕ в automation-действиях,
  НЕ в permissions.
- Ошибки компилятора: unknown component; аргументы не соответствуют in-схеме;
  недопустимый тип; hash не найден в blob store (проверка при apply,
  не при check — check офлайн валидирует только формат).

## 2. ABI (canonical-JSON-in / canonical-JSON-out)

Модуль обязан экспортировать ровно:
```
kalita_alloc(size: u32) -> ptr: u32          // выделение в линейной памяти
kalita_call(in_ptr: u32, in_len: u32) -> u64 // (out_ptr << 32) | out_len
```
- Вход: canonical JSON объекта in-полей, UTF-8 (порядок ключей — сортированный;
  money/decimal — строки, не float: "123.45"; duration — секунды int;
  date/datetime — RFC 3339).
- Выход: canonical JSON `{"ok": <value>}` либо `{"err": "<сообщение>"}`.
  Любой другой формат = ошибка компонента.
- Модуль НЕ имеет imports вообще (`module.ImportedFunctions() == 0` —
  проверка при загрузке, отказ иначе). Нет WASI, нет часов, нет рандома,
  нет I/O — **детерминизм по построению, не по обещанию**.

## 3. Лимиты исполнения (дефолты; конфиг узла может ужесточать)

| Параметр | Значение |
|---|---|
| Runtime | wazero (pure Go, CGO запрещён) |
| Fuel | 10_000_000 |
| Memory | 16 MiB max |
| Wall-timeout | 100 ms |
| Размер модуля | ≤ 5 MiB |
| Кэш результата | key = (module_hash, canonical_in); LRU 10k на узел |

Превышение любого лимита = ошибка компонента (не узла).

## 4. Семантика ошибок и события

- Ошибка компонента (err-ответ, лимит, trap, кривой JSON) → computed-поле =
  **null** + Append `component.failed {component, module_hash, entity,
  record_id, field, error}` (schema_v: 1; родня automation.failed из LLD-01 —
  та же retry-политика НЕ применяется: computed пересчитывается при следующем
  чтении/мутации естественно; failed — для наблюдаемости, тихих провалов нет).
- В guard-условии ошибка компонента = guard false + то же событие (переход
  не происходит — fail-closed).
- Установка модуля: файл → content-addressed blob (механика files);
  `component`-декларация приезжает через propose_change → подпись → apply
  проверяет наличие blob по hash → Append `component.installed
  {name, module_hash}` — кто и что привнёс, доказуемо.
- Компонент по построению не может: мутировать, читать вне in, обходить
  permissions, тянуть время/рандом (нет imports). Санбокс-модель HLD §3.8
  закрыта.

## 5. Приёмочные тесты (фиксированы)

1. Референс-модуль (TinyGo, исходник в examples/components/tax_calc):
   computed считает; результат кэшируется (2-й вызов без исполнения — счётчик).
2. Модуль с import'ом (WASI hello) → отказ загрузки.
3. Fuel-бомба (бесконечный цикл) → ошибка за ≤ timeout, поле null,
   component.failed записан, узел жив.
4. err-ответ и не-JSON-ответ → null + component.failed.
5. Guard c ошибкой компонента → переход отклонён (fail-closed).
6. Hash-подмена (blob не совпадает с декларацией) → отказ apply.
7. Детерминизм: 1000 вызовов на одном входе → один результат, один хэш выхода.
8. money-строки: "0.1"+"0.2"-класс — референс-модуль работает на строках,
   не float (тест на копейки).

## 6. PR-нарезка (R3)

PR-1: wazero-обвязка + ABI + лимиты + тесты 2,3,7.
PR-2: грамматика component + compose + apply-проверка blob (+тест 6).
PR-3: интеграция в computed/guard + события + кэш (+тесты 1,4,5,8) +
      examples/components/tax_calc + гайд автора компонента.
