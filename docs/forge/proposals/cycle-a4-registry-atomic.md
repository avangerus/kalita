# Cycle-A4 · PROPOSE — Identity registry: atomic Register + invite projection (LLD-10 PR-1/PR-2)

> Milestone A fix-цикл #3. Для ARCHITECT REVIEW.

## Проблема

1. **PR-1 (TOCTOU)**: `Registry.register` (internal/identity/registry.go:138-160) делает
   `lookup` (существует ли актор) и затем `store.Append` БЕЗ общего захвата мутекса.
   Два конкурентных Register одного id → оба видят `nil` → два `actor.registered` →
   при RegisterWithToken второй вызов вернёт токен, чей hash не в журнале, либо
   двойная запись (путаница токенов). Evidence: SYSTEM_TRUTH (LLD-10 PR-1);
   registry.go:142-153 (lookup → Append, между ними mu отпущен — lookup→all отпускает mu).
2. **PR-2 (O(журнала))**: `findInvite` (invites.go:101-130) делает `store.All` — полный
   скан журнала на каждый CreateInvite/Redeem. Холодный путь, но O(журнал) растёт
   неограниченно. LLD-10 PR-2 требует проекцию O(1) + тест Redeem.

## Варианты

### A — mu-сериализация + инвайт-проекция (рекомендуется)

1. `register`: держать `r.mu` поверх lookup+append. Рефакторинг: `register` берёт mu,
   вызывает `foldLocked`, проверяет `r.cache[id]`, аппендит — под тем же mu. Регистрации
   сериализуются; повторный Register видит первого и возвращает ErrActorExists. Никакого
   orphan-токена (никаких IdempotencyKey-нюансов с RegisterWithToken).
2. `findInvite`: проекция `r.invites map[string]*Invite` (ключ hex(CodeHash)), фолдится
   в `foldLocked` из invite.created/redeemed; `findInvite` → mu + fold + map-lookup O(1).
   Тот же паттерн, что `tokenIndex` (registry.go:33-41).

**Цена**: ~40 строк + тесты (race-тест двойного Register; тест Redeem через проекцию
идентичен скану — сверка результатов). Blast radius: internal/identity (register,
foldLocked, invites.go) + тесты.

### B — только PR-1 (PR-2 отложить)

PR-2 — чистый перф холодного пути; «не переписываем ради красоты». Медленнее закрывает
LLD-10, но меньше кода.

### C — IdempotencyKey на append вместо mu

Дедюп на уровне стора, но RegisterWithToken при dedup-хите вернёт токен, чей hash не
записан (orphan) — требует различать «создан vs dedup»; сложнее и хрупче mu-подхода.

## Ожидаемое изменение архитектуры

- `internal/identity/registry.go`: register под mu; `invites`-проекция в Registry + fold.
- `internal/identity/invites.go`: findInvite → O(1) map-lookup.
- Поведение НЕ меняется: те же ошибки, тот же журнал (виды событий не меняются).

## Acceptance criteria

1. Race-тест: N конкурентных Register одного id → ровно один `actor.registered`,
   N-1 ErrActorExists; никакого orphan-токена.
2. Redeem после проекции: инвайт создаётся/гасится идентично старому скану (тест
   сверяет findInvite результат до/после — либо поведенческий Redeem-тест).
3. `go test ./... -race ./internal/identity/` зелёный; полный прогон зелёный.
4. Журнал/DSL не меняются; replay идентичен.
5. Reviewers: skeptic_pro + review_security_pro (identity — trust surface).

## Риски

- Deadlock при вложенном захвате mu (register держит mu и вызывает foldLocked/append —
  НЕ lookup/all, которые сами берут mu) — проверяется тестом.
- Проекция invite: ключ hex(CodeHash) уникален (рандом 16 байт) — коллизий нет.

## Human gates

Нет.
