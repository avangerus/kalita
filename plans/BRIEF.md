# Kalita — BRIEF

## Суть системы

Kalita — enterprise runtime для управления цифровой рабочей силой (AI-агентами).
Не фреймворк, не чат, не BPM. Управляемая система исполнения где:

> LLM предлагает → система валидирует → человек одобряет (если нужно) → система исполняет детерминированно

## Для кого

Enterprise operations teams, system integrators, GovTech/B2B платформы.
Организации с комплексными операционными процессами где нужна аудируемость.

## Что делает

- Управляет работой через Cases и WorkItems
- Принимает решения через Coordination + Policy слои
- Выбирает исполнителей (Actor) по capability, profile, trust
- Исполняет через детерминированные ActionPlans с WAL и компенсацией
- Отслеживает состояние через Control Plane (операторский вид)
- Поддерживает approvals, deferrals, escalation, governance

## Что не делает (инварианты)

- НЕТ прямого LLM-исполнения
- НЕТ chat-first интерфейса
- НЕТ CRUD-first архитектуры
- НЕТ DAG/workflow engines (Airflow-style)
- НЕТ вероятностных решений в runtime
- НЕТ неконтролируемой агентной автономии

## Стек

- Go + Gin, модульный монолит с event-driven ядром
- Хранилище: in-memory репозитории (проектированы под будущую БД)
- Исполнение: синхронное + контролируемое async
- Внешние API: намеренно отсутствуют в текущей стадии

## Текущее состояние (M1 — DONE)

Работает полный pipeline: Event → Case → Work → Coordination → Policy → Execution.
Control Plane функционален. Demo с multi-case сценариями. Trust system работает.
Approvals с idempotent handling. Server-rendered HTML UI.

## Следующие вехи

- M2: Coordination 2.0 (system-level, queue-aware decisions)
- M3: Внешняя персистентность (замена in-memory на DB)
- M4: Real use case (AIS Otkhody реальные данные)
- M5: Production hardening

## Конечная цель

Enterprise OS для цифровой рабочей силы — управление бизнес-операциями,
оркестрация цифровых сотрудников, динамическое развитие систем.
