# Architecture Skeptic Pro — ревью (миссия F, Round-9)

> Задача: «Докажи, что Business Expert / Builder разрушили clean boundaries
> Kalita или создали второй скрытый DSL/runtime». Вердикт: **ACCEPT-WITH-NOTES**
> — не доказано; пять инвариантов держатся в коде; нет второго runtime,
> нет собственной authority builder'а, нет инъекций в `when`, нет forge/replay
> подписей, нет кросс-пользовательских утечек сессий.

## 8 проверочных точек (все PASS)

1. **Builder identity/statelessness** — нет пути apply/sign/mutate под
   собственной identity; MCP-вызовы только под bearer вызывающего; между
   ходами state не хранится (всё перечитывается из журнала).
2. **Подделка решений / инъекции в выражения** — `when`-гейты строятся ТОЛЬКО
   из распарсенных чисел (`_fmt`) и фиксированного набора ролей; ответ вида
   «amount > 0 or 1=1» без роли → parse отказывает ([]).
3. **Frameworks** — fail-closed: sha256 + id/version + schema + path-traversal
   (frameworks.py:102-150); в промпты попадает только верифицированный реестр.
4. **Кросс-пользователь / XSS** — security.dsl scopes owner/session.owner=$me;
   рендер сообщений через Preact html`` (экранирование); innerHTML нет.
5. **CompileMulti/DefinitionSources** — additive-гейт на каждом пути
   (propose/decide/replay-fold).
6. **Подписи proposals** — human+defApprover; challenge привязан к
   актору+proposal; replay кросс/same-proposal невозможен.
7. **Второй runtime** — walker = закрытое подмножество, вне — `None`
   (unknown); общий корпус walker_golden.json гоняется движком и walker'ом.
8. **Model independence** — LLM_URL/LLM_MODEL/LLM_KEY env-only; OpenAI-
   совместимый вызов; вывод валидируется (kind∈whitelist, fid∈registry);
   семантика — в ноде.

## Findings и статус закрытия (Round-9)

| # | Sev | Finding | Статус |
|---|---|---|---|
| 1 | MAJOR | get_definition/proposals отдают полные definition sources любому актору | ✅ CLOSED: оба гейтятся ролью defApprover (tools.go, handlers_ui.go); Files вырезаны из списка proposals |
| 2 | MINOR | Идемпотентные ключи с uuid4 (retry = двойная запись) | ✅ CLOSED: детерминированный uuid5 по (session, turn/kind, body); новая сессия — явный nonce |
| 3 | MINOR | match_framework возвращает crm → build_spec ValueError → 500 | ✅ CLOSED: BUILDABLE-фильтр (ap/itsm) в обоих путях match |
| 4 | NIT | packHeaderOf — второй raw-text парсер вместо токенов лексера | ⏳ кандидат (не эксплуатируется; лексер отклоняет multi-line strings) |
| 5 | NIT | decision_map доверяет любому accepted-решению (create unscoped) | ⏳ задокументирован; эскалации нет (всё равно propose→подпись) |
| 6 | NIT | basis mislabels actor «builder» | ✅ CLOSED: basis = design_session (как у propose) |

**Итог F (архитектура): ACCEPT** — findings закрыты или задокументированы как
не-блокирующие кандидаты.
