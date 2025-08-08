# dsl/modules/olga/entities.dsl

entity Brief:
    number: int unique                  # Номер (автоинкремент)
    project_name: string required       # Название проекта (обязательно)
    deadline: date                      # Дедлайн
    client: string required             # Клиент (выбор из справочника, либо свободный ввод)
    brand: string required              # Бренд (выбор из справочника, либо свободный ввод)
    period_from: date                   # Срок действия с
    period_to: date                     # Срок действия по
    no_tender: bool default=false       # Без тендера
    comment: text                       # Комментарий (длинный текст)
    companies: array[string]            # Компании-участники (множественный выбор)
    profit_norm: array[float]           # Норма прибыли по компаниям (если надо — можно связать с companies по индексу)
    managers: array[string]             # Менеджеры (многих)
    project_team: array[string]         # Проектная группа

entity Project:
    number: int unique                  # Номер проекта
    name: string required               # Название проекта
    client: string required             # Клиент
    brand: string required              # Бренд
    period_from: date required          # Срок действия с
    period_to: date required            # Срок действия по
    brief_number: int                   # Номер Брифа (связь)
    no_tender: bool default=false       # Без тендера
    companies: array[string]            # Компании-участники
    profit_norm: array[float]           # Норма прибыли по компаниям
    project_managers: array[string]     # Менеджеры проекта
    project_team: array[string]         # Проектная группа
    status: enum[Draft,Tender,InWork,Closed] default=Draft

entity Estimate:                        # Смета
    number: int unique                  # Номер сметы
    name: string required               # Название сметы
    project: string required            # Проект (выбор)
    period_from: date required          # Срок действия с
    period_to: date required            # Срок действия по
    client: string required             # Клиент (автозаполнение)
    company: string required            # Компания
    manager: string                     # Менеджер сметы
    currency: string                    # Валюта
    type: enum[Project,Budget] required # Тип сметы
    status: enum[Draft,InSigning,FinalApproval,InWork,Editing,SigningChanges,ForFinalize,Closed,Stopped] default=Draft
    comment: text
    # другие специфические поля, как потребуется

entity EstimateLine:                    # Строка сметы
    number: int                         # Номер строки
    estimate: string required           # Смета (родитель)
    code: string                        # Статья плана счетов
    item: string                        # Наименование услуги
    qty: array[map]                     # Массив пар [unit, amount]
    unit_cost: float                    # Стоимость единицы
    subtotal: float                     # Стоимость по прайсу
    vat: float                          # VAT
    pl_percent: float                   # Плановый процент прибыли
    gross_extra_net: float              # Доход по строке
    costs: float                        # Сумма расходов по строке
    act_percent: float                  # Фактический процент прибыли
    rest: float                         # Остаток

entity Expense:                         # Расход
    number: int unique
    created_at: date
    creator: string
    assignee: string                    # Подотчетный
    currency: string
    payment_date: date
    status: string                      # Можно перечислить статусы как enum
    in_subline_number: int              # Привязка к InSubline
    item: string                        # Причина оплаты
    qty: array[map]                     # Пары "единица"/"количество"
