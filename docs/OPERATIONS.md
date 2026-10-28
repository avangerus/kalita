# OPERATIONS — runbook оператора узла kalita

Для оператора-нетехнаря. Каждая процедура — команды в порядке выполнения.
Узел = один бинарь `kalita` + PostgreSQL (журнал). Все команды читают
`KALITA_PG_DSN` через цепочку секретов (см. «Секреты» ниже).

## Установка выпущенной версии

Выпуски — на GitHub Releases (архивы linux/windows/darwin, UI встроен) и образ
`ghcr.io/avangerus/kalita:<версия>`. Всё подписано keyless (cosign + GitHub OIDC)
workflow `release.yml` по тегу версии (D-022). Проверка архива перед установкой:
```
cosign verify-blob --bundle SHA256SUMS.bundle \
  --certificate-identity-regexp '^https://github\.com/avangerus/kalita/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
```
Образ: `cosign verify` с теми же двумя флагами и `ghcr.io/avangerus/kalita:<версия>`.

## Ежедневно: ничего

Узел живёт сам. Смотрите дашборд/отчёты по потребности:
```
kalita report --since 7d --pack <dir>     # неделя в цифрах (действия, роли, очередь подписей, SLA)
kalita egress-report --since 7d           # что покидало периметр, сверка с whitelist
```

## Проверка целостности (еженедельно и после любого инцидента)

```
kalita verify --pub node.pub                        # хеш-цепь + подписи чекпоинтов
kalita verify --pub node.pub --require-signed-approvals   # + подписи людей на решениях
```
`chain ok` + `checkpoints ok` = история доказуемо не переписана.
`CHAIN BROKEN` = стоп, ничего не менять, звонить поставщику.

## Бэкап (ежедневно, автоматизируйте cron'ом)

```
pg_dump "$KALITA_PG_DSN" > backups/$(date +%F).sql
```
Журнал — единственный источник правды; проекции пересобираются реплеем.
Раз в квартал — restore-учение: развернуть бэкап в чистый PG, запустить
узел на нём, `kalita verify` — зелёный.

## Upgrade (новая версия)

```
kalita upgrade --to <версия> --dry-run   # репетиция: шаги 1-3
kalita upgrade --to <версия>             # боевой прогон
```
Нужен `cosign` в PATH. По умолчанию образ — `ghcr.io/avangerus/kalita`, подпись
проверяется keyless по идентичности release-workflow; свой образ — `--image`,
подписанный ключом — `--cosign-key cosign.pub`.
7 шагов печатаются на экран (preflight → backup → pull+подпись → stop →
start → smoke → запись в журнал). Провал на шагах 5-6 — **автоматический
откат**, старая версия уже поднята, журнал не тронут. Старый бинарь на
новом журнале сам откажется стартовать (защита от downgrade); осознанный
даунгрейд — только `--force-downgrade`.

## Экстренная блокировка (подозрение на компрометацию)

```
kalita lockdown                # все токены отвергаются; на экране — разовый emergency-токен
# ... разбирательство под emergency-токеном ...
kalita lockdown lift           # снять занавес, прежние токены снова работают
```
Emergency-токен показывается ОДИН раз, в журнал пишется только хеш.

## Пользователи и агенты

```
kalita user add --id alice --role Lead --password '...'   # человек (+ passkey через UI)
kalita agent add --id bot1 --role Agent --egress-domains api.example.com
kalita user revoke --id alice                             # мёртвые токен и подписи
```
Прод-режим подписей: `kalita serve --require-human-signature` — согласование
проходит только с passkey (WebAuthn), токенного «да» недостаточно.

## Shadow-режим (пилот нового агента)

```
kalita shadow enable --role Agent --pack <dir>    # агент предлагает, не делает
kalita shadow disable --role Agent --pack <dir>
```
Предложения видны в ленте (mutation.proposed), одобрение применяет их
обычным путём — включая HITL самой операции.

## Секреты

Приоритет: файл `KALITA_SECRETS_FILE` (права строго 0600) → docker/systemd
secret (`KALITA_PG_DSN_FILE`) → env `KALITA_PG_DSN` (warning, не для прод).
Слабые права на файле секретов = отказ старта (это не баг).

## Если узел не стартует

1. Читайте первую строку ошибки — она всегда говорит, что делать (fix hint).
2. `refusing to start ... downgrade` — вы запускаете старый бинарь на новом
   журнале: возьмите свежий бинарь или осознанно `--force-downgrade`.
3. `secrets: ...` — права файла секретов (0600) или путь.
4. После kill -9 / падения питания узел стартует сам: незакрытая операция
   либо доиграна, либо помечена `inconsistent`-задачей — смотрите ленту.

## Контакты

Безопасность: см. SECURITY.md (корень репо). Поддержка: поставщик узла.
