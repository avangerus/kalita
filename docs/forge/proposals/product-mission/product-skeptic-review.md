# Product Skeptic Pro — ревью (миссия F, Round-9)

> Задача: «Докажи, что обычный бизнес-пользователь не сможет использовать
> Kalita без команды внедрения». **Вердикт: REJECT (Round-9) — фиксы →
> повторная оценка в Round-10.** Движок и self-building loop доказаны
> clean-room'ом; блокеры лежали в deploy/bootstrap-слое.

## Findings и статус закрытия

| # | Sev | Finding | Статус |
|---|---|---|---|
| B1 | BLOCKER | docker compose не содержит builder-воркер; пак по умолчанию — contracts, без --approver | ✅ CLOSED: compose: builder-сервис (Dockerfile.builder), KALITA_PACK default = packs/builder, --approver, KALITA_BUILDER_URL → meta |
| B2 | BLOCKER | Первый вход — только токен; парольный login не в UI | ✅ CLOSED: UI-вкладка Login (логин+пароль → POST /api/login); восстановление = повторный вход |
| M1 | MAJOR | builder URL — localStorage-хак | ✅ CLOSED: server-advertised (meta.builder_url) + same-host default |
| M2 | MAJOR | SoD-403 без подсказки self-onboarding | ✅ CLOSED: humanizeError подсказывает «запросите роль (Agents → приглашения)» |
| M3 | MAJOR | «Failed to fetch» при упавшем воркере | ✅ CLOSED: человеческий текст «Ask Kalita недоступен…» |
| N1 | NIT | RU/EN микс, нет i18n | ⏳ single-language pilot (документировано) |
| N2 | NIT | Нет импорта данных (счета вручную) | ⏳ inbound-коннекторы — существующий паттерн; после пилота |
| N3 | NIT | Только 2/3 фреймворка buildable | ⏳ crm — knowledge-only, явный отказ (не угадывание) |
| N4 | NIT | Нет истории конкурентных изменений | ⏳ STALE_BASE + deterministic keys; документировано |

**Итог F (product): FINAL VERDICT — ACCEPT-WITH-NOTES (Round-10).** Все
BLOCKER'ы и MAJOR'ы закрыты (верифицированы по коду скептиком). Остаточные
R1/R2 закрыты в Round-10: `.env.example` (KALITA_PACK=builder + APPROVER +
BUILDER_URL/LLM_URL) и QUICKSTART.md (парольный первый пользователь,
продуктовый onboarding); protocol-мисматч builder URL (http://host:8400 default
для localhost; KALITA_BUILDER_URL за TLS-прокси для remote). R10 (SLA-дашборд
не evidenced live) — исправлено: curated top-level фрагменты (события +
дашборды) теперь ВХОДЯТ в предлагаемый DSL; live-тест ассертит AP + Service
desk дашборды. NIT'ы (RU-only, bulk import, 2/3 фреймворков, concurrency,
D-011) — документированные лимиты пилота, не adoption-блокеры.
