# TECHDEBT — живой реестр технического долга kalita

Формат записи: `[severity] область — что не так → что надо. (когда/контекст)`
severity: **P0** блокирует прод · **P1** мешает заявленному сценарию · **P2** ергономика/качество · **P3** мелочь.

Этот файл ведётся непрерывно: всякий раз, когда обходим проблему, оставляем явный долг здесь, а не молча.

---

## Ядро: automation / schedule

- **P1 schedule-парсинг минимальный.** `Tick` понимает только `every <dur>` (интервал) и fallback «раз в день». Форма `daily at 09:00` НЕ парсится в время суток — на каждом тике в новом дне-ведре сработает, время игнорируется. → реальный cron-парсер (минуты/часы/дни недели/время). (2026-06-12, при auto-reindex; engine/automation.go `scheduleInterval`)
- **P1 неограниченный рост задач от schedule.** `on schedule every Nm` плодит task каждую интервал-ведро ВЕЧНО (resync KnowVault, regen сайта), даже если ничего не изменилось. In-memory `e.tasks` растёт без GC. → политика ретенции/прунинга завершённых задач; либо change-detection до создания task. (2026-06-12)
- **P2 automation не умеет напрямую дёргать workflow-переход.** Closed-list действий: agent/notify/webhook/escalate/create/update. Перевод статуса делаем обходом через `agent Role: task` + воркер зовёт `act`. → рассмотреть действие `transition X` в automation. (2026-06-12, knowvault reindex)
- **P2 multi-line скобки в permissions не поддержаны.** `act [a, b,` с переносом строки парсится как новые verbs (E018). Длинные списки прав — только одной строкой. → парсер должен склеивать скобки через перенос. (2026-06-12, content-пак)

## Ядро: identity / HITL

- **P1 consumer-auth отсутствует.** identity заточена под governed-акторов и агентов (токены/роли/подписи), НЕ под self-signup тысяч клиентов: регистрация, верификация почты, сброс пароля, OAuth-соцвход, rate-limit, GDPR-согласие. Блокирует личный кабинет на публичном сайте. → либо строить consumer-слой, либо интеграция внешнего IdP (Keycloak/Auth0) с маппингом в actors. (2026-06-12, вопрос про личный кабинет)
- **P2 подписи HITL не криптографически обязательны в dev-пути.** `requireSig` по умолчанию выкл → `decide grant=true` проходит без верифицированной подписи. Для прода нужен `WithRequireSignatures()` + реальная подпись approver. (2026-06-12, демо content)

## Федерация узлов (топология «узел на систему»)

Выбранная топология: один узел kalita на систему (сайт/CRM/…), узлы общаются по MCP. Сегодня поддержано ПО ФОРМЕ (узел = и MCP-сервер, и может держать воркеров-клиентов чужих узлов с governed-ролью; журнал пишет cross-node basis). НЕ поддержано:

- **P1 cross-node refs.** `ref[X]` — локальный лукап. Сослаться из CRM-узла на запись сайт-узла = удалённый id + синхронизация, нет механизма. (2026-06-12)
- **P1 надёжный cross-node handoff.** Поток «лид с сайт-узла → сделка в CRM-узле» требует outbox/ретраи/идемпотентность доставки. Нет. (2026-06-12)
- **P2 федеративный cockpit-query.** Сводка owner’а через несколько узлов = federated-query или rollup-реплика. Нет. (2026-06-12)
- **P2 федерация identity.** Claude как один оператор флота узлов — нужен общий якорь доверия / shared identity-authority. Сейчас идентичность на узел. (2026-06-12)

## Content / CMS пак + рендерер

- **P2 deploy рендерера — только `dir:`/`git:`.** Доставка вывода есть (`KSITE_DEPLOY=dir:<вебрут>` mirror+prune, `git:<repo>` commit/push — Pages/Netlify). Нет SFTP/rsync-remote/S3, нет атомарного swap (mirror правит вебрут на месте), запуск воркера всё ещё ручной (токен/env). (2026-06-12)
- **P1 нет deploy-шага.** Рендерер пишет статику локально; доставки на хостинг (rsync/SFTP/S3/git-push) нет — петля «правка → live на хостинге» не замкнута. (2026-06-12)
- **P2 страницы статей не рендерятся.** `articles_section` даёт ссылки `blog/<slug>`, но сами страницы статей не генерируются → битые ссылки. (2026-06-12)
- **P2 рендерер регенит вслепую.** regen каждые 2м даже без изменений; нет change-detection на контент (в отличие от индексатора KnowVault с fingerprint). (2026-06-12)
- **P2 markdown-рендер примитивный.** Без изображений, таблиц, вложенных списков; ссылки/escape грубые. (2026-06-12, render.py `md()`)
- **P2 нет визуального/блочного редактора страниц** для не-технаря (table stakes у Wix/Webflow/HubSpot). (2026-06-12, бенчмарк)
- **P3 нет SEO-обвязки** (sitemap.xml, og-метатеги, canonical, robots) и адаптива/тем. (2026-06-12)

## Коннекторы по краю (connector SDK)

«Интегрируем, не строим» пока декларация — интерфейса нет.

- **P2 connector scaffold есть, обобщения нет.** `workers/connectors/inbound.py` — первый коннектор (форма→Lead) и образец паттерна (untrusted POST → whitelist-map → MCP create под least-privilege identity). Но это только inbound/create: нет upsert/update, нет дедупа, нет rate-limit, секрет один на форму. → выделить общую базу когда появится 2-й коннектор. (2026-06-12)
- **P1 нет payment-коннектора** (Stripe/YooKassa) + webhook → инвойс в `Paid`. Агент к рельсам не прикасается — но системе нужна интеграция. Скопировать паттерн inbound.py. (2026-06-12)
- **P2 нет email/calendar sync** (Nylas/Graph) — table stakes CRM. (2026-06-12)
- **P2 нет Jitsi/appointment-коннектора** (комната + ссылка в кабинет). (2026-06-12)
- **P2 нет Bitrix24 REST/webhook-коннектора** (мост к его CRM: сделки/контакты/счета). (2026-06-12)

## CRM table-stakes (ориентир Bitrix24/Salesforce/Pipedrive)

- **P2 born-agentic SalesBot только на `on create Opportunity`.** Триаж лида (New→Working) не реализован: Activity нельзя привязать к Lead (нет ref Activity→Lead, только →Opportunity). → добавить Activity↔Lead или Lead-триаж без активности. (2026-06-12)
- **P2 SalesBot не ставит `due` у заведённой Activity** (нет дедлайна follow-up). → попросить у LLM срок или дефолт +2 дня. (2026-06-12)
- **P2 авто-капчур активностей, дедуп контактов, мобайл, форкаст по территориям** — отсутствуют (различители из бенчмарка). (2026-06-12)
- **P3 deal→invoice handoff между паками** (CRM Opportunity Won → finance Invoice) не связан — finance.dsl это анонсирует, моста нет. (2026-06-12)

## KnowVault

- **P2 legacy-форматы doc/xls/ppt** не извлекаются (нужен LibreOffice/конвертер; у фаундера не стоит). (ранее)
- **P2 mail/git как источники** — заявлены в видении, не реализованы (ingest понимает только Files/Upload). (ранее)

---

## Долг закрытый (для истории)

- ~~Инкрементальная индексация KnowVault (skip unchanged / re-embed changed / drop deleted)~~ — закрыто `84f92ca`.
- ~~Auto-reindex без ручного триггера~~ — закрыто `b822a03` (interval schedule + resync task).
- ~~Born-agentic CRM (система действия)~~ — частично: SalesBot триажит новую Opportunity (квалификация + follow-up Activity + advance), HITL на close_won цел. Доказано вживую 2026-06-12.
- ~~Inbound connector «форма→Lead» + scaffold connector-слоя~~ — `workers/connectors/inbound.py`: untrusted POST → whitelist-map → governed create под least-privilege Intake. Секрет-гейт + блок инжекта статуса доказаны вживую 2026-06-12 (`crm.dsl` роль Intake).
- ~~Outbound deploy рендерера (петля «правка→live»)~~ — `KSITE_DEPLOY=dir:`(mirror+prune)/`git:`(commit/push). Доказано вживую: публикация→вебрут, снятие страницы→исчезает из вебрута, git-commit. 2026-06-12. Симметрия connector-слоя (in+out) замкнута.
