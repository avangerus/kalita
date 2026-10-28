# Требования → контракт → задача → release → evidence

**Статус:** acceptance map N1. Tests в этой таблице предстоит реализовать; наличие строки не означает прохождение runtime проверки. Все tasks перечислены в [TASKS.json](delivery/TASKS.json); документальная проверка находится в [VALIDATION.json](VALIDATION.json).

## Инварианты

| HLD invariant | Основные контракты | Tasks | Gate | Сценарий опровержения |
|---|---|---|---|---|
| N-01 — стабильная работа | WORK_MODEL, AGENT_PROTOCOL | KAL-006, 009, 025–028, 049–050 | R2/R4 | SC-01: закрыть чат, сменить actor/model/view, передать ответственность; условия не потеряны |
| N-02 — актуальная authority и ACL | AUTHORITY_SECURITY, API_CONTRACTS, INTEGRATIONS, VIEWS | KAL-007–008, 016, 024, 029, 039–042 | R1/R2/R3 | SC-02: revoked parent и чужой client через search/count/cache/view/child не раскрываются |
| N-03 — enforced boundary | AUTHORITY_SECURITY, OPERATIONS | KAL-017–018, 020, 051–052 | R1/R4 | SC-03: executor пытается socket/DB/secret/redirect bypass; blocked в declared enforced profile |
| N-04 — общий bounded budget | EXECUTION, AGENT_PROTOCOL | KAL-013, 018–020, 048–050 | R1/R4 | SC-04: concurrent children+retry+unknown billing не обходят hard bound, double settlement невозможен |
| N-05 — fencing | EXECUTION, API_CONTRACTS | KAL-011–012, 015, 019–020 | R1 | SC-05: старый ответ того же actor после re-claim отвергнут; trusted receipt реального эффекта сохранён |
| N-06 — effect unknown | EXECUTION, INTEGRATIONS, API_CONTRACTS | KAL-014, 020, 043–045 | R1/R3 | SC-06: remote accepted → crash до receipt; unknown сохраняет резерв, blind retry отсутствует |
| N-07 — типы доказательства | WORK_MODEL, PRODUCT_CONTRACT | KAL-025–028, 031–035, 047, 064 | R2/R5 | SC-07: вопрос не обещание; report не acceptance; usage count не ROI |
| N-08 — версии и replay | EXECUTION, WORK_MODEL, API_CONTRACTS | KAL-008–010, 015, 048, 054, 058 | R1/R4 | SC-08: новая schema/method не меняет исторические criteria; replay не dispatch |
| N-09 — expressive safe views | VIEWS, AUTHORITY_SECURITY | KAL-036–042, 045 | R3 | SC-09: misleading label/private literal/JS/stale binding; trusted confirmation не обманута |
| N-10 — управляемый опыт | EXPERIENCE, AGENT_PROTOCOL | KAL-046–050, 069, 079 | R4/R6/R7 | SC-10: revoked/неприменимый/чужой метод не выбран; новый negative case отвергает улучшение |
| N-11 — полноценный recovery | OPERATIONS, EXECUTION | KAL-053–055, 059, 073–074 | R4/R6 | SC-11: backup до отправки+revoke; restore из старой базы не повторяет effect и не воскрешает права |
| N-12 — простота и честные гарантии | PRODUCT_CONTRACT, OPERATIONS, pack | KAL-002–005, 030–035, 051–065 | R2/R4/R5 | SC-12: новый оператор ставит/работает/обновляет/восстанавливает; full cost включает rescue/review |

Названия LLD здесь — документы в [карте](README.md), а не имена существующих code modules. Полные KAL IDs содержатся в JSON; сокращения диапазонов в таблице используются только для чтения.

## Business acceptance

| Product AC | Tasks / Gate | Evidence |
|---|---|---|
| AC-01 — точное обязательство и очередь | KAL-025–026 / R2 | размеченный источник, false promise/candidate correction и полезный case |
| AC-02 — durable case/handoff | KAL-006, 027, 050 / R2/R4 | handoff offer/accept, restart/chat switch, goal amendment |
| AC-03 — consequential action | KAL-014, 016, 039, 043–044 / R1/R3 | trusted digest, stale approval и ограниченный receipt |
| AC-04 — revoke/isolation | KAL-016, 024, 029, 041–042 / R1–R3 | grant lineage + derived outputs, private literal и аудитория |
| AC-05 — видимый общий расход | KAL-013, 018, 055 / R1/R4 | parallel reserves, unknown billing, operator cost view |
| AC-06 — unknown/recovery | KAL-014, 044, 053 / R1/R3/R4 | remote acceptance crash и lost-tail quarantine restore |
| AC-07 — индивидуальный/общий UI | KAL-036–042 / R3 | несколько task/role layouts, pin/live/snapshot distinction |
| AC-08 — применимый опыт | KAL-046–048, 069 / R4/R6 | positive/negative/new evaluation, owner/expiry/revoke |
| AC-09 — простой lifecycle | KAL-051–060 / R4 | clean-room installation/first value/upgrade/restore/disconnect/exit |
| AC-10 — измеренная польза | KAL-031–035, 061–065 / R2/R5 | baseline/native alternative/full-cost series в двух компаниях |

## Минимальный формат acceptance evidence

`evidence_id`, task/release, candidate source/patch refs, supported profile, версии contracts/pack/method/policy, environment, scenario/input refs с ACL, ожидаемый и наблюдаемый исход, время, проверивший actor, failures/repairs и повторная targeted verification. Запись не включает secrets или полные приватные transcripts по умолчанию.

Техническое evidence подтверждает конкретное поведение в указанной среде. Human Acceptance подтверждает решение уполномоченного участника. Market evidence описывает наблюдаемую серию и метод сравнения. Эти классы нельзя взаимозаменять, даже если все хранятся в одном журнале.

## Остановка и исключения

SC-02–SC-06/SC-11 failures — material safety failures: affected dispatch выключается, reserves/история сохраняются, owner расследует. Gate не принимается формулировкой «почти всё работает». Условная ветвь R6/R7 может быть deferred с причиной; она не закрывает чужой acceptance. Изменение инварианта требует нового N-ADR и review, а не waiver в task.
