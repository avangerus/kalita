# Security Pro — ревью разговорного слоя (миссия F, Round-9)

> Критическое правило: «LLM никогда не обходит existing Kalita authorization».
> **Вердикт: ACCEPT-WITH-NOTES — обхода нет.** Каждый вызов ноды на LLM-пути
> идёт с bearer вызывающего; Permission Engine гейтит мутации ровно как для
> человека. Инъекции в `when` нет (гейты — только из распарсенных чисел);
> XSS нет; фреймворки — fail-closed; подпись привязана к актору+proposal.

## Findings и статус закрытия (Round-9)

| # | Sev | Finding | Статус |
|---|---|---|---|
| 1 | MAJOR | Подписант не видит, что подписывает: additive-гейт не покрывает роли/права/гейты/guards; каноник без хэша контента; план «no structural changes» | ✅ CLOSED: diffPlan теперь перечисляет дельты прав (по содержимому правил), удаления ролей/переходов, изменения approval-гейтов и guards; каноник подписи включает DefinitionContentHash (сортированные files) — подпись связана с контентом |
| 2 | MAJOR | propose_change без гейта → журнал/память DoS | ✅ CLOSED: лимит 20 открытых proposal (CONFLICT + fix_hint) + **лимит размера одного proposal (4 MiB источников, VALIDATION)** + тесты TestProposePendingCap/TestProposeSizeCap; decided не считаются |
| 3 | MINOR | Идемпотентный ключ proposal без контента | ✅ CLOSED: ключ включает sha256(files) — разные proposal не коллизят |
| 4 | MINOR | create unscoped + owner override (наслед. pack-gap) | ⏳ задокументирован (общий класс TECHDEBT; инъекция, не эксфильтрация) |
| 5 | MINOR | Связка framework↔assembler по конвенции | ⏳ кандидат (loader-чек пересечения topic-множеств) |
| N1 | NIT | Порог-парсинг безопасен (verified); exact-token matching — кандидат | ⏳ |
| N2 | NIT | sha256 фреймворков привязан к неподписанному manifest (host-write) | ⏳ blast radius ограничен: assembler не читает контент фреймворка |
| N3 | NIT | Логи чистые (verified: session/phase/ms, без токенов/тел) | ✅ |
| N4 | NIT | XSS: innerHTML нет (verified) | ✅ |
| N5 | NIT | EXPLAIN↔proposed не связан хэшем (defense-in-depth) | ⏳ кандидат |

**Итог F (security): ACCEPT** — обход авторизации отсутствует; MAJOR'ы закрыты.
