# Полный roadmap N1

**Статус:** план, 2026-10-07. Основание: [продуктовый контракт](../PRODUCT_CONTRACT.md), [HLD](../HLD.md), [решения](../DECISIONS.md). Источник подробных задач — [TASKS.json](TASKS.json).

## Последовательность доказательств

| Gate | Что становится доказанным | Итерации | Что ещё не доказано |
|---|---|---|---|
| R0 | Согласованная архитектура, current truth, выбранные входные решения и критерии | I00 | Реализация новых функций; первый покупатель без реального доступа |
| R1 | Ограниченное долговечное исполнение с полномочиями, бюджетом и broker boundary | I01–I03 | Польза бизнеса и универсальная самостоятельность |
| R2 | Польза одного процесса на двух источниках в серии кейсов | I04–I06 | Полный starter pack и delegation области |
| R3 | Полезная безопасная генерация/жизненный цикл views и ограниченные внешние действия | I07–I08 | Эксплуатационная и коммерческая повторяемость |
| R4 | Один поддерживаемый pack-кандидат, проверенный опыт и работа в выданной области | I09–I11 | Применимость без форка во второй компании |
| R5 | Повторяемая полезность и измеренная стоимость внедрения/сопровождения | I12 | Универсальная отрасль или способность заменить любую систему |
| R6 | Второй выбранный процесс и selective responsibility handover | I13–I14 | Глобальная миграция legacy/межорганизационные гарантии |
| R7 | Один или несколько востребованных экспериментов новых границ | I15 | Готовый универсальный software будущего |

R0 в названии документационного baseline и выполнение I00 различаются. Написанный HLD не закрывает buyer interview, доступ к источникам, crash prototype и согласование pilot thresholds. Все implementation tasks сейчас planned. R0–R7 не Git tags и не автоматически SemVer 0.x/1.x.

## Приоритеты и параллельность

Критический путь: выбрать реальный процесс/источники → contracts + Tx → Attempt/budget/effect → broker/isolation/harness → intake/confirmed obligations → pilot evidence. После полезного пилота — ViewSpec и ограниченные consequential actions; затем adaptive area experiment, lifecycle и complete pack.

Эксплуатационные spikes начинаются раньше коммерческого candidate: profile выбирается I00, backup/rotation constraints учитываются при schema design. I10 может идти параллельно части I07–I09 после своих task dependencies; R4 ждёт всех обязательных результатов. Source A/B adapters в I04 и отдельные read/ACL tests можно вести параллельно, если ownership файлов разделён. Tasks.json содержит конкретную DAG; номер iteration не заменяет её.

Luna/high выполняет bounded exploration/implementation/verification; root принимает архитектурные и интеграционные решения. Astra/high используется на денежных/правовых эффектах, security boundary, concurrency/restore, существенных новых контрактах и независимом review. Использование более дешёвой модели не должно превращать model output в authority. В самом продукте routing моделей — versioned company/method policy, а не жёсткая зависимость от выбранных здесь имён моделей.

## Правило оценки срока и объёма

Для task есть deliverables, зависимости, приёмка, проверки и S/M/L. Календарь формируется после I00, с учётом состава команды, source access и времени pilot серии. Дата релиза не подменяет gate. L task до работы разбивается исполнителем на ограниченные проверяемые изменения; root сохраняет acceptance исходного task.

Не планируем универсальный BPM, общий язык всего, множество installers, три первых отрасли, federation и свободные plugins до доказательства выбранного пути. Отложенные ветви имеют condition и остаются planned/deferred, а не автоматически закрываются.

## Пересмотр и остановка

- PG gate провален: новый ADR с единственным выбранным adapter и пересмотром ownership/Tx, до R1 production claims.
- Не найден реальный buyer/data owner: технический prototype допустим, value/market gate blocked.
- Pilot не выигрывает по качеству/полной стоимости: сузить или сменить процесс с новым baseline; не компенсировать отсутствующий спрос платформенными функциями.
- Нужен простой managed delivery: пересмотреть один reference profile, не поддерживать всё одновременно.
- Обнаружены uncontrolled effect, ACL leak или budget bypass: приостановить affected dispatch, сохранить unknown/резервы/историю, устранить и повторить focused verification.
- Restore/cutover неоднозначен: quarantine/manual reconcile; не продолжать автоматически.

R6 начинается только после R5 и выбора одного нового процесса. R7 содержит независимые условные опыты: third-party extensions, federation, machine-first business или перенос опыта. Ненужная ветвь не должна блокировать полезную; KAL-080 принимает результаты только выбранных экспериментов и явно оставляет прочие deferred.

## Контроль завершения

Task: implemented → original scenario verified → independent material review → repairs verified → acceptance evidence. Release: все обязательные tasks/gates и эксплуатационные ограничения приняты ответственными владельцами. Любой `waiver` имеет owner, срок, безопасный ограниченный режим и не отменяет N-02–N-06. Без доказательств инварианта enforced profile не разрешается заявлять этот уровень доверия.

Подробности: [RELEASES](RELEASES.md), [ITERATIONS](ITERATIONS.md), [TASKS](TASKS.md), [TRACEABILITY](../TRACEABILITY.md).
