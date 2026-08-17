# Cycle-A3 · PROPOSE — Operator trust surface: verify --file + TLS defaults (OPS-2, SEC-3)

> Milestone A/F fix-цикл #2 (два P2 одного класса: «доверие оператора по умолчанию»).
> Стартует после ACCEPT Cycle-A2. Для ARCHITECT REVIEW.

## Проблема

1. **OPS-2**: `kalita verify` читает журнал ТОЛЬКО из PG (main.go:90-93:
   `KALITA_PG_DSN is required`), а restore-совет `kalita backup` говорит
   «point a fresh node at this journal.jsonl, then `kalita verify --pub <node.pub>`»
   (backup.go:139) — команда не умеет читать файл бэкапа. Backup-дрилл из
   OPERATIONS.md невыполним как написано.
2. **SEC-3**: `kalita upgrade --insecure-skip-tls-verify` default **true**
   (upgrade.go:102) — молчаливое отключение проверки TLS; prometheus.yml:15
   `insecure_skip_verify: true` без CA-истории (deploy/prometheus/prometheus.yml).

Evidence: SYSTEM_TRUTH техдолг (P2 BUG, P2 SECURITY); RELEASE_BLOCKERS OPS-2/SEC-3.

## Варианты

### A — минимальный честный фикс (рекомендуется)

1. `verify --file <path>`: путь к каталогу бэкапа (или journal.jsonl) — события
   читаются через существующий `loadVerifiedBackup` (backup.go:207-254: sha256 +
   chain + anchor проверяются заодно); без `--file` — прежний PG-путь. Restore-совет
   в backup.go:139 становится исполняемым.
2. upgrade: `--insecure-skip-tls-verify` default **false**; при self-signed
   деплое оператор передаёт флаг явно (в docker-compose/runbook это задокументировано);
   обновить usage-текст и deploy-доки.
3. prometheus.yml: `insecure_skip_verify` заменяется на `ca_file:
   /etc/prometheus/kalita-ca.crt` + runbook-шаг «экспортировать самоподписанный
   сертификат ноды в CA-файл» (docs/runbooks/); dev-вариант с
   insecure_skip_verify оставить комментарием с предупреждением.

**Цена**: ~40 строк + тесты (verify_test.go: file-путь на фикстуре бэкапа;
upgrade: default-флаг). Blast radius: cmd/kalita + deploy/prometheus + runbook.

### B — только OPS-2 (SEC-3 отдельным циклом)

Медленнее; два микроцикла вместо одного coherent batch «operator trust».

### C — полноценный CA-bootstrap (генерировать CA нодой)

Большой дизайн (cert authority в ноде) — вне scope пилота; кандидат в Later.

## Acceptance criteria

1. `kalita backup` → `kalita verify --file <dir>` проходит на свежем бэкапе
   (chain+anchor), в т.ч. намеренно повреждённый файл → CHAIN BROKEN, exit 1 (тест).
2. `kalita verify` без --file продолжает работать с PG (существующий тест).
3. `--insecure-skip-tls-verify` без явного флага = false (тест: default значение).
4. prometheus.yml использует ca_file; runbook описывает экспорт сертификата.
5. build/vet/test зелёные; 16 паков + 3 примера.
6. Reviewers: skeptic_pro (+review_security_pro — TLS поверхность).

## Риски

- Ни один существующий вызов verify/upgrade не ломается (PG-путь и явный флаг).
- Blast radius мал; журнал/DSL не тронуты.

## Human gates

Нет.
