# Независимая критика и проверка N1

**Дата:** 2026-10-07. Это журнал design review документационного набора, не runtime acceptance.

## Первая критика — до написания архитектуры

Консультант/критик: Astra/high, задача `architecture_foundation_challenge`. Исходная задача — опровергнуть выводы исследования и выбранную основу по текущим HLD/LLD/code, простоте, first market и будущей корпоративной работе. Общая идея сохранена; вывод «переделать всё» не подтверждён. Ни выбранный рынок, ни durable runtime, ни deployment не признаны доказанными.

| Finding | Исправление в N1 |
|---|---|
| Старые HLD запрещают пересмотр ядра; UI runtime/irreversible/HITL/signatures conflicts | DECISIONS N-ADR-01/04/07/08/10; новая иерархия и точечные notices старых docs |
| Sector ranking не доказывает первого покупателя | PRODUCT_CONTRACT selector; KAL-002/004; N-ADR-03 provisional |
| Очередь обязательств не доказывает работу агентов в области | AGENT_PROTOCOL; отдельный experiment KAL-049/050 в R4 |
| Кооперативный worker egress не enforced security | AUTHORITY_SECURITY; N-ADR-06; KAL-017/020 |
| Только success/fail невозможно для удалённого действия | Effect unknown/receipt/reconcile; N-ADR-08; KAL-014/044 |
| «Доказательства» слишком сильное универсальное обещание | WORK_MODEL Claim/Observation/Evidence/Acceptance; risk-tiered policy и causal measurement |
| DSL рискует стать языком всего | N-ADR-04 canonical contracts и отдельные agent/view protocols |
| Полезное reuse и реальная delivery уже существуют, runtime gaps конкретны | AS_IS отделяет dirty checkout от релиза; KAL-001/015/043 |
| PG против Temporal ещё не исследован; два master недопустимы | N-ADR-05/I00 fault+ops gate, one-adapter deployment |
| On-prem/single binary не означает простой полный lifecycle | N-ADR-13, один provisional profile, KAL-004/051–055 |
| Опыт не равен logs и общему обучению | EXPERIENCE MethodRevision, negative/new cases, owner/expiry/revoke |

## Замечания Astra к деталям при сборке

1. Старый task completion без Attempt token не совместим с новым fencing, даже для того же actor. N-ADR-15; EXECUTION/API переписаны на legacy read compatibility и обязательный новый write protocol. KAL-015 проверяет counterexample.
2. Backup может не содержать effects/revokes после точки копии. N-ADR-14; HLD/OPERATIONS требуют quarantine, lost-tail reconciliation, fresh authority и epoch. KAL-053 проверяет отсутствующий в backup effect.
3. Бюджет не доказуем без цены/максимума до model call. N-ADR-09; HLD/EXECUTION/AGENT_PROTOCOL определяют hard bound, caps, unknown reserves и soft-mode исключение. KAL-018 проверяет overrun.
4. Static workflow недостаточен для adaptive delegation. Добавлен AGENT_PROTOCOL: ResponsibilityContract, PlanRevision, durable bounded model→intent→broker→observation loop. KAL-019/049/050 отделяют техническую основу от area experiment.
5. Source run не заменяет grant lineage. AUTHORITY_SECURITY/AGENT_PROTOCOL требуют parent_grant_id/root_authority_id, проверку активной цепочки и каскад revoke; независимый company grant — новый root. KAL-016 проверяет delayed cache invalidation.

6. Transitive automation/webhook не может повышать authority под autoActor. AUTHORITY_SECURITY требует causation/root grant/budget либо самостоятельного bounded company edge; unbound legacy egress отключён в R1 до bridge KAL-043.

## Итоговый проход

Свежая проверка собранного набора выполняется тем же независимым Astra/high критиком после написания документов. Первый итоговый проход не нашёл P0, но выявил три P1 и два P2:

- P1 projection admission: durable heads + applied-head barrier в HLD/EXECUTION, regression KAL-010. Stale owner/criteria/ACL не разрешают read/command после commit новой revision.
- P1 plan patch: начатые steps нельзя переименовать/изменить; outstanding effects/reserves существуют вне текущего graph и блокируют terminal success. AGENT_PROTOCOL §7 и KAL-049.
- P1 семантический acceptance-cycle: KAL-040 сужен до renderer/read/action; publish/snapshot — KAL-041/042 и R3 gate KAL-045.
- P2 claim route: EXECUTION согласован с API `/api/next/worker/claims`.
- P2 release labels: OPERATIONS согласован с R3 views/effects и R4 delegated-area experiment.

Повторный Astra/high review перечитал исправленные контракты и JSON: все три P1 и первоначальные P2 закрыты; новых P0/P1 не найдено. Дополнительный P2 enum исправлен: `settlement_pending` — reason состояния `blocked`, не новый Run status. **Итог: нет открытых material design findings в проверенном наборе.** Это design disposition, не утверждение реализации или отсутствия будущих дефектов.

Структурные проверки отдельно: links/anchors, JSON schema shape, unique IDs, dependency DAG, release/iteration coverage, acceptance/verification fields и status integrity. Их итог — [VALIDATION.json](VALIDATION.json). Они не заменяют design critique или runtime tests.
