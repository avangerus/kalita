# Runbook'и — day-2 эксплуатация узла калиты

Каждый runbook кончается **критерием успеха** (проверяемым, не «сделал шаги»).
Команды ниже — реальный CLI (`kalita version` — полный список). Предпосылка:
`KALITA_PG_DSN` указывает на журнал (иначе нода in-memory и state теряется).

---

## 1. Backup (журнал + файлы + ключи) — автоматизировано

```bash
# единый согласованный бэкап-набор + MANIFEST + checkpoint-якорь (LLD-17 O2/O3)
kalita backup --out backup/$(date +%F) --files "$KALITA_FILES_DIR" --pii-keys "$KALITA_PII_KEYS"
# -> journal.jsonl + files/ + pii-keys.json + MANIFEST.json + ANCHOR

# проверить бэкап (непроверенный бэкап = отсутствующий):
kalita backup verify --dir backup/$(date +%F)
# -> sha256 всех компонентов + повторная проверка hash-цепочки + якорь

# ANCHOR (хеш последнего checkpoint.sealed) сохраняется ВОВНЕ узла — клиентский
# бэкап/TSA/нотариус: внешнее доказательство «журнал не переписан» (O3).
```

**Критерий успеха:** `backup verify` печатает «hash chain intact»; подмена любого
байта journal.jsonl ломает verify (тампер ловится по sha256).

## 2. Restore / DR

```bash
# пустая машина: поднять Postgres, восстановить бэкап (журнал + MANIFEST)
createdb kalita
kalita backup restore --dir backup/$(date +%F)       # загружает journal.jsonl в пустой PG
kalita verify --file backup/$(date +%F) --pub node.pub   # цепочка+якорь БЭКАПА целы (OPS-2)
# вернуть файлы/ключ ноды (если бэкапились): каталог files/ и node-key — как делались
kalita serve --pack <packs> --ui-dir web            # переигрывает журнал
curl -fsS http://127.0.0.1:8080/readyz              # 200 {status:ready}
```

**Критерий успеха:** запись, созданная до бэкапа, читается после restore; `verify`
печатает «chain ok … head seq»; `/readyz` = 200. (Форматы бэкапа: journal.jsonl +
MANIFEST.json; файлы/ключи — опциональные компоненты, см. `kalita backup --files
--pii-keys`.)

## 3. Lockdown (аварийный тормоз)

```bash
kalita lockdown                     # все bearer-токены отвергнуты, кроме emergency admin
kalita lockdown lift                # вернуть: ранее выданные токены снова приняты
```

**Критерий успеха:** после `lockdown` старый токен отклонён (AUTH_REQUIRED), свежий
emergency admin работает; после `lift` старые токены снова валидны.

## 4. Идентичность агента/человека + ротация ключа

```bash
kalita agent add --id eng-1 --role Engineer      # выдаёт bearer-токен
kalita agent revoke --id eng-1                   # токен и подписи мертвы
# ротация ключа актора — через UI (login/passkey) или agent add заново; событие actor.key_rotated в журнале
```

**Критерий успеха:** отозванный токен получает AUTH_REQUIRED; в журнале — `actor.key_rotated`.

## 5. Upgrade и миграция эр

```bash
# upgrade требует --image (registry, где публикуется образ) и --to (тег).
# NOTE: registry-publish/cosign ещё не настроены (OPS-3, post-release) — до этого
# апгрейд = пересборка образа: docker compose up --build (VERSION инжектится compose).
kalita upgrade --image ghcr.io/<org>/kalita --to v1.0.1 --dry-run   # preflight+backup+pull-verify
kalita upgrade --image ghcr.io/<org>/kalita --to v1.0.1             # реально, с rollback
# self-signed TLS (дефолтный деплой) → проверка smoke требует явный флаг (SEC-3):
kalita upgrade --image ghcr.io/<org>/kalita --to v1.0.1 --insecure-skip-tls-verify
# смена эры (ручная миграция): 
kalita export --pack <dir> --out era.jsonl        # снапшот журнала
kalita import --pack <new> --in era.jsonl         # НОВАЯ эра (журнал должен быть пуст)
kalita era verify --old <dsn> --new <dsn>         # генезис новой эры чейнится к sealed-финалу старой
```

**Критерий успеха:** после upgrade `/readyz` = 200; `era verify` доказывает связь эр.

## 6. Аудит доказуемости (регулятор)

```bash
kalita verify --pub node.pub                      # чекпоинты + офлайн-подписи
kalita verify --pub node.pub --require-signed-approvals   # каждое approve обязано иметь passkey
kalita report --since 7d --format md              # «неделя в цифрах», чистый fold по журналу
kalita egress-report --since 7d                   # что ушло за периметр vs whitelist
# egress-report включает доставки webhook-раннера ноды (Cycle-B1): их «whitelist» —
# KALITA_EGRESS_DOMAINS; доставка вне allowlist не выполняется (fail-closed), а в
# отчёте видна как аномалия только если allowlist был настроен иначе.
# Имя из allowlist резолвится один раз на соединение, соединение идёт на проверенный
# адрес: приватные/link-local адреса — только с KALITA_EGRESS_ALLOW_PRIVATE=1
# (квитанция `private_target`), IDN-хосты — только в punycode (квитанция `idn_host`).
```

**Критерий успеха:** `verify` печатает «checkpoints ok: history is provably unrewritten».

## 7. Восстановление воркера

```bash
docker restart <worker>           # или systemctl restart <worker>
# воркер (agent_runner/connector/bridge) переподписывается с durable-курсора
```

**Критерий успеха:** воркер продолжает с курсора; повтор обработки факта даёт no-op
(детерминированный idempotency_key), не дубликат эффекта (LLD-21 §3).

## 8. Prometheus TLS (SEC-3): привязать самоподписанный сертификат ноды

```bash
# после первого старта ноды экспортировать её самоподписанный сертификат как CA:
# (путь внутри контейнера — /data/tls/node.crt, см. deploy/entrypoint.sh TLS_DIR)
docker cp kalita:/data/tls/node.crt /etc/prometheus/kalita-ca.crt
# prometheus.yml уже использует ca_file + server_name: localhost — сертификат ноды
# самоподписан на CN/SAN=localhost (entrypoint.sh), а scrape идёт на hostname `kalita`;
# server_name сообщает Prometheus, какое имя проверять. При смене KALITA_TLS_CN
# (напр. на публичный домен) обнови и server_name.
# перечитать конфиг:
curl -X POST http://prometheus:9090/-/reload
```

**Критерий успеха:** `up`-таргет kalita = 1 в Prometheus без `insecure_skip_verify`;
подмена сертификата (рестарт ноды с новым self-signed) ломает scrape — оператор
обновляет CA-файл.

---

## Пока НЕ закрыто (зависит от незавершённых LLD)

- **Шреддинг субъекта** (право на забвение) — runbook появится вместе с LLD-16
  (crypto-shredding): `erase-subject` + согласованное уничтожение ключа в бэкапе (LLD-17).
- **Полный DR-набор** (RPO/RTO, проверка бэкапа по таймеру, внешний якорь чекпоинта) —
  с LLD-17; §1/§2 выше — ручной минимум до этого.
