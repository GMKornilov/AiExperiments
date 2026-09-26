# Разбор неудачной автономной попытки: чат `4cec4a1846e799cd7bcde06180aaf1a0`

## Scope и идентификаторы

Этот отчёт разбирает неудачную автономную попытку по чату
`4cec4a1846e799cd7bcde06180aaf1a0`. Цель — объяснить наблюдаемую цепочку
решений, отклонений и отката состояния. Это не разбор скрытого
chain-of-thought: ниже приведены только проверяемые входы, выходы, решения
валидаторов и краткие выводы из них.

| Сущность | Идентификатор |
| --- | --- |
| Чат | `4cec4a1846e799cd7bcde06180aaf1a0` |
| Неудачная попытка / correlation ID | `83228872-c229-4aa5-8ec2-90a0ab9da96b` |
| Успешная повторная попытка | `975da4e1-30ed-4f6b-aaa3-98243b60dc25` |
| Декодер JSON с fenced output | `b98ecc84…` |
| Недопустимый переход `clarify_input → execution` | `280db4b…` |
| Текстовый DSML после BrewMark batch | `65bbe740…` |
| Принятый переход в research | `aa7ae1e5…` |
| Успешный BrewMark batch | `962293d0…` |
| Proposal с утверждением о жерновах | `f62d21ff…` |
| Проверка `inventory-truth` | `fe4edd47…` |
| Принятый research proposal после repair | `6a8e8507…` |
| Proposal без точного catalog handoff | `845d8055…` |
| Проверка `research-sufficiency` | `b5f7730b…` |

## Исходное состояние

Пользователь уже ответил на уточняющие вопросы. Task-flow находился на стадии
`clarify_input`; целью следующего перехода было подтверждённое исследование
входных данных (`research_input_data`). В диалоге названы кофемолка DF64 Gen 2
и кофеварка De'Longhi EC685. При названной модели workflow требует каталоговый
lookup до перехода к составлению рецепта (`execution`).

У автономной попытки был бюджет в 8 task-step LLM-вызовов. Внешние LLM-запросы
не падали на уровне сети или провайдера: в логах они завершились HTTP 200.

## Хронология 8 бюджетных шагов

| Шаг | Вызов | Наблюдаемый выход / действие | Решение | Изменение состояния |
| --- | --- | --- | --- | --- |
| 1 | `b98ecc84…` | Корректно выбран `research_input_data`, но JSON обёрнут в ` ```json ` | Отклонён strict decoder (`invalid_proposal`) | Нет |
| 2 | `280db4b…` | Предложен переход `clarify_input → execution` без обязательного lookup | Отклонён проверкой переходов (`validation`) | Нет |
| 3 | `aa7ae1e5…` | Предложен переход в `research_input_data` с планом исследования | Принят | `stage=research_input_data`; `goal_confirmed=true` |
| 4 | `962293d0…` | Запрошен и выполнен batch из трёх BrewMark-инструментов | Успех | Snapshot task-flow не изменён до proposal |
| 5 | `65bbe740…` | Вместо JSON proposal возвращён текстовый DSML tool-call | Отклонён strict decoder (`invalid_proposal`) | Нет |
| 6 | `f62d21ff…` | Research proposal с утверждением о типе жерновов | Отклонён `inventory-truth` (`fe4edd47…`) | Нет |
| 7 | `6a8e8507…` | Research proposal без спорного утверждения | Принят | Стадия осталась `research_input_data` |
| 8 | `845d8055…` | Предложен переход к `execution`, без точных результатов каталога в handoff | Отклонён `research-sufficiency` (`b5f7730b…`) | Нет; бюджет исчерпан |

## Точные результаты BrewMark batch

На шаге 4 вызовы BrewMark выполнились успешно. Их результаты:

| Запрос | Результат |
| --- | --- |
| `brewmark_list_grinders("DF64 Gen 2")` | `matchStatus=exact`; диапазон `0–90`; `settingUnit=NUMBER`; `espressoAnchor=10`; `burrType=FLAT` |
| `brewmark_list_brewers("De'Longhi", "EC685")` | `empty` — записи в каталоге нет; это не доказывает отсутствие устройства у пользователя |
| `brewmark_list_brew_methods()` | Доступен только `Drip`; для эспрессо нерелевантно |

## Разбор отклонений

### 1. Fenced JSON (`b98ecc84…`)

Наблюдаемый факт: семантически proposal выбирал правильную следующую стадию
`research_input_data`, но ответ был оформлен как Markdown fenced JSON.
Контракт strict decoder принимает только чистый JSON, поэтому он вернул
`invalid_proposal`.

Краткое проверяемое резюме решения модели: модель распознала, что пользователь
уже ответил на вопросы, и решила сначала оформить переход к research, оставив
вызовы инструментов для следующего шага. Некорректность была форматной, а не
планировочной: применён обычный стиль представления JSON, несовместимый с
контрактом транспорта.

### 2. Пропуск research (`280db4b…`)

Наблюдаемый факт: модель предложила `clarify_input → execution`; детерминированное
правило переходов запретило это как `validation`.

Краткое проверяемое резюме решения модели: в выходе отражено, что модель
заметила необходимость BrewMark lookup, но посчитала, что может опереться на
уже известное пользователю оборудование и перейти к рецепту. Это неверно для
данного контракта: конкретные модели были названы, а значит каталоговая
проверка обязательна. По сути, был применён fallback для неполных данных к
случаю с известными моделями.

### 3. DSML вместо proposal после успешного batch (`65bbe740…`)

Наблюдаемый факт: после результатов BrewMark вместо ожидаемого JSON proposal
был возвращён текст, похожий на DSML tool-call. Инструменты на этой фазе не
исполнялись; strict decoder классифицировал ответ как `invalid_proposal`.

В доступном контексте до финальной генерации уже были корректные результаты
DF64, отсутствующая запись EC685 и нерелевантность `Drip`. Следовательно,
ожидаемой безопасной операцией был research-only catalog handoff. Фактический
выход противоречит этой траектории и формату контракта.

Причину сериализации нельзя считать установленной. Версия о несовместимости
формата tool-call у модели `deepseek-v4-flash` — гипотеза: логи подтверждают
HTTP 200 и текстовый DSML, но не внутреннюю причину его появления.

### 4. `inventory-truth` и несовпадение контекста (`f62d21ff…`, `fe4edd47…`)

Наблюдаемый факт: proposal утверждал, что на DF64 стоят стандартные /
эспрессо-жернова; `inventory-truth` это отклонил как неподтверждённое.

В полном диалоге пользователь ответил «да» на вопрос «На DF64 стоят стандартные
эспрессо-жернова, верно?». Проверенный raw payload инварианта содержал в
нормализованных facts только «Кофемолка: DF64 Gen 2» — без нормализованного
факта о жерновах и без связи ответа «да» с вопросом. Это подтверждённое
несовпадение контекста: task-модель могла восстановить факт из истории, а
инвариант не мог проверить его по собственному payload.

Относительно полного диалога отклонение является false positive: утверждение
proposal опиралось на явный ответ пользователя, который не был передан в
контекст `inventory-truth`.

### 5. `research-sufficiency`: отсутствует точный catalog handoff (`845d8055…`, `b5f7730b…`)

Наблюдаемый факт: proposal предлагал `execution`, но не включал подтверждённые
поля каталога для DF64: `matchStatus=exact`, `0–90`, `NUMBER`,
`espressoAnchor=10`, `FLAT`. `research-sufficiency` корректно не разрешил
переход.

Краткое проверяемое резюме решения модели: в repair-контексте ей был известен
статус успешного batch, но не все исходные значения результата. Она не стала
изобретать точные числа и сделала неверный процедурный вывод, что можно
перейти к рецепту по общему знанию оборудования. Контракт требует обратного:
статус `success` не заменяет явную передачу каталожных данных.

## Потеря tool results в repair-контексте

Последовательность, подтверждаемая событиями:

1. На шаге 4 BrewMark вернул нужные точные данные.
2. Шаг 5 после batch вернул невалидный DSML вместо JSON proposal.
3. Repair-контекст последующих кандидатов был построен без полных результатов
   ранее исполненных инструментов.
4. В нём сохранились статусы вызовов (`success` / `empty`), но не все значения
   каталоговой записи.
5. На шаге 8 инвариант потребовал именно потерянные значения для catalog handoff.

Таким образом, после невалидного ответа модель и инвариант работали с разными
наборами фактов. Это системный механизм, который объясняет финальную ловушку:
повторять инструменты запрещено, точные результаты отсутствуют в repair-контексте,
а переход без них запрещён.

## Commit и rollback

Принятые изменения шагов 3 и 7 были промежуточными изменениями автономной
попытки. После отклонения шага 8 бюджет в 8 вызовов исчерпался. Task-flow не
закоммитил частичный результат: восстановил исходное состояние `clarify_input`
и подставил штатное сообщение о невозможности завершить автономный шаг.

Следовательно, финальный вид задачи не означает, что шаги 3 и 7 не выполнялись;
он означает, что попытка использует атомарный commit только после успешного
завершения всей цепочки.

## Успешная повторная попытка

Повтор `975da4e1-30ed-4f6b-aaa3-98243b60dc25` завершился успешно. В нём была
пройдена ожидаемая последовательность:

```text
clarify_input → research_input_data → точный catalog handoff → execution → user_feedback
```

Рецепт был сохранён, задача достигла `user_feedback`.

## Вывод и направления исправления

Непосредственная причина сбоя — исчерпание бюджета task-step после пяти
отклонений. Сетевой или провайдерный сбой не подтверждён. Главный системный
фактор — утрата полных tool results между невалидным post-tool ответом и repair
candidate; её усилили хрупкий strict JSON-контракт и несовпадение контекста
инварианта с историей диалога.

Возможные направления исправления без реализации:

- Передавать в каждый repair-контекст канонические результаты всех уже
  выполненных инструментов, а не только статусы.
- Валидировать и нормализовать fenced JSON до strict decoder либо сильнее
  закрепить требование «только JSON» в транспорте/structured output.
- Делать недопустимый переход `clarify_input → execution` более явным в
  контексте и возвращать repair-подсказку с требуемой следующей стадией.
- Передавать инвариантам provenance диалоговых фактов: вопрос, ответ и
  нормализованный факт, чтобы убрать рассинхронизацию контекста.
- При post-tool decode failure разрешать безопасное повторное построение
  proposal из сохранённых результатов без повторного вызова внешних tools.
- Разделить бюджет на логические операции или не считать чистые repair-ответы
  наравне с полноценными автономными шагами, если это не нарушает лимиты.

## Аудит system prompt по отклонениям

Этот раздел сопоставляет наблюдаемые ответы с проверенными структурами task,
post-tool, repair и invariant prompt. Он не воспроизводит скрытый reasoning
модели. «Уверенность» показывает уверенность в связи между формулировкой
prompt и отклонением, а не вероятность поведения модели вообще.

### Общая структура prompt

Наблюдаемый факт: общий task prompt имеет объём примерно 7,6–9,2 тыс. символов
и одновременно описывает все стадии task-flow: `clarify_input`,
`research_input_data`, `execution` и `user_feedback`. В нём есть общие правила
формирования proposal, переходов, исследования, инструментов и результата.

Проблема: prompt не является фазовым. Даже когда допустима одна конкретная
следующая операция, модель одновременно получает инструкции для последующих
стадий. Это увеличивает число конкурирующих целей: оформить JSON, искать в
каталоге, выдать рецепт, обработать feedback.

Вывод: выделение phase-specific prompt и отдельного контракта
`catalog_handoff` уменьшит неоднозначность. Уверенность: высокая — смешение
стадий подтверждено; точный вклад в каждый отдельный ответ остаётся выводом.

### `b98ecc84…`: JSON в Markdown fences

**Релевантная структура prompt.** Task prompt требует JSON proposal, однако не
содержит явного запрета Markdown fences и не использует принудительный
structured output / JSON schema на транспортном уровне.

**Чего не хватало.** Короткого, выделенного на последней позиции требования:
«верни ровно один JSON-объект; не используй ` ``` `, Markdown, пояснения или
tool-call синтаксис». Ещё надёжнее — schema/response-format вне текстового
prompt.

**Что было лишним.** Общие инструкции всех стадий и примерная текстовая
риторика вокруг proposal создают условия, в которых привычный формат
«```json» выглядит допустимым, хотя парсер его отвергает.

**Конфликт правил и эффект.** Явного противоречия нет: это пробел между
семантическим требованием «JSON» и синтаксическим контрактом strict decoder.
Модель сформировала семантически подходящий proposal, но оформила его привычным
Markdown-способом; decoder вернул `invalid_proposal`.

**Уверенность.** Высокая.

### `280db4b…`: запрещённый `clarify_input → execution`

**Релевантная структура prompt.** Общий task prompt одновременно описывает
clarify, обязательное исследование при известных моделях и правила execution.
Repair-наставление требует «сохрани текущий этап» после невалидного кандидата.
При этом оно не называет `allowed_next` и не формулирует конкретно для данного
состояния: «разрешён только `clarify_input → research_input_data`».

**Чего не хватало.** Машиночитаемого списка допустимых переходов в каждом
вызове и repair-инструкции вида: «текущая стадия `clarify_input`; единственная
разрешённая следующая стадия — `research_input_data`; `execution` запрещён».

**Что было лишним.** Описание recipe/execution до завершения каталожной
проверки, а также широкий fallback-язык для неполных входных данных. Он
конкурирует с более узким правилом для известной модели.

**Конфликт правил и эффект.** «Сохрани текущий этап» конфликтует по смыслу с
обязательным переходом `clarify_input → research_input_data`: первое можно
прочитать как запрет на стадию-переход, второе требует его. Неуказанный
`allowed_next` оставляет модели путь к преждевременному `execution`, который
она и предложила; детерминированный валидатор отклонил переход.

**Уверенность.** Высокая в наличии конфликта и отсутствия `allowed_next`;
средняя в том, что именно этот конфликт стал решающим мотивом модели.

### `65bbe740…`: DSML после успешного BrewMark batch

**Релевантная структура prompt.** Post-tool prompt сохраняет прежний
`current_step` как «perform lookup». На этой фазе реальные tools отключены,
но prompt не содержит явного запрета на DSML / повторный tool-call и не
переводит задачу в отдельное состояние «собери только JSON catalog handoff из
результатов».

**Чего не хватало.** Явного режима post-tool: «инструменты уже выполнены;
никаких tool calls, DSML и повторных lookup; верни только JSON proposal,
используя приложенные результаты». Нужна также отдельная структура результата
`catalog_handoff`, а не повторное использование общего task proposal.

**Что было лишним.** Устаревший `current_step=perform lookup` после того, как
lookup уже завершён. Он подталкивает к повторению действия и противоречит
фактическому состоянию orchestration.

**Конфликт правил и эффект.** Модель видит шаг выполнения lookup, но среда
больше не принимает tool calls. Без явного запрета она сгенерировала текстовый
DSML-вызов; провайдер вернул HTTP 200, а strict decoder распознал не JSON и
отклонил ответ.

**Уверенность.** Высокая в асимметрии post-tool prompt и среды; средняя в
атрибуции DSML именно этому фактору, поскольку формат tool-call может зависеть
от конкретной модели.

### `f62d21ff… → fe4edd47…`: `inventory-truth` и provenance факта

**Релевантная структура prompt.** Task-модель получает историю диалога, где
есть antecedent-вопрос про эспрессо-жернова и ответ пользователя «4. да».
`inventory-truth` получает нормализованные facts, в которых этого
нормализованного факта и связи вопрос → ответ нет. Дополнительно invariant
prompt включает нерелевантные для этой проверки эвристики
`semantic_suspicion` и `execution_result_present`.

**Чего не хватало.** Provenance: для каждого нормализованного факта нужны
источник, исходный вопрос и ответ либо надёжная ссылка на соответствующую пару
сообщений. Тогда invariant мог бы проверить, что «да» относится именно к
жерновам, а не требовать догадки из обрезанного facts payload.

**Что было лишним.** `semantic_suspicion` и `execution_result_present` в
инварианте inventory-truth. Они расширяют семантическую площадь проверки, но
не помогают установить происхождение спорного факта и повышают риск ложного
отклонения.

**Конфликт правил и эффект.** Task-модель может использовать подтверждение из
полной беседы, invariant — нет. Это подтверждённое несовпадение контекста.
По отношению к полному диалогу результат `inventory-truth` — false positive:
proposal опирался на явный ответ пользователя, не переданный валидатору.

**Уверенность.** Высокая.

### `845d8055… → b5f7730b…`: exact handoff после repair

**Релевантная структура prompt.** Финальный repair/task prompt передаёт лишь
outcome metadata выполненного batch. Оно прямо маркировано как `not evidence`,
запрещает повторять инструменты, но требует перейти к execution только после
точного catalog handoff. Полные tool messages/results, полученные до DSML,
в этот repair-контекст не включены.

Research invariant system prompt, напротив, содержит конкретный пример для
DF64: anchor `10`, диапазон `0–90`, `FLAT`. Этих значений repair-модель уже не
видит.

**Чего не хватало.** Канонического, неизменяемого блока уже полученных tool
results в каждом repair-вызове; явного подэтапа `catalog_handoff`, который
сначала обязан отразить `exact`-данные и только затем открывает `execution`.

**Что было лишним.** Одновременное сочетание «не считай outcome evidence»,
«не повторяй lookup» и требования exact handoff без приложенных значений.
Также избыточен конкретный DF64-пример во внутреннем prompt инварианта, если
эти же данные отсутствуют у task-модели.

**Конфликт правил и эффект.** Repair-модель не должна придумывать значения,
не может повторно вызвать tool и обязана передать точные значения, которые ей
не показаны. Инвариант знает примерные/конкретные данные о DF64, task-модель
не знает: это data leakage/asymmetry между проверяющим и проверяемым
контекстом. Модель попыталась продолжить к `execution` по outcome metadata;
`research-sufficiency` обоснованно отклонил отсутствие exact handoff.

**Уверенность.** Высокая в потере результатов и асимметрии; высокая в том,
что они сделали корректный handoff практически недостижимым без повторного
lookup.

### Рекомендуемое направление перепроектирования prompt-контрактов

Без реализации, рекомендуемая последовательность:

1. Разделить общий prompt на фазовые: `clarify`, `research`, `catalog_handoff`,
   `execution`, `feedback`. В каждый передавать только допустимые действия и
   явный `allowed_next`.
2. Ввести отдельный typed `catalog_handoff` с полями результата каталога,
   provenance и флагом готовности к execution.
3. Сохранять и повторно прикладывать канонические tool results во все repair и
   post-tool вызовы; outcome metadata не должно быть единственным источником
   результатов.
4. Зафиксировать structured output на уровне API и явный запрет fences, DSML,
   prose и повторных tool-call там, где инструменты отключены.
5. Передавать инвариантам те же факты и provenance, которыми обоснован proposal;
   убрать из inventory-инварианта не относящиеся к provenance эвристики.
6. Убрать конкретные каталожные значения из invariant-only примеров либо
   гарантировать их одинаковую доступность task-модели и валидатору.

## Конкретные изменения system prompt

Ниже предложены точечные изменения контрактов prompt. Блоки даны на English,
как их следует передавать в runtime. Они не реализованы в этом изменении и не
заменяют необходимые context/payload-изменения.

### `b98…`: строгий JSON без fences

**Место изменения.** Общий контракт вывода task, repair и post-tool prompt;
добавлять и в начало, и последней инструкцией перед ответом.

**Добавить.**

```text
OUTPUT CONTRACT — HIGHEST PRIORITY
Return exactly one valid JSON object matching the proposal schema.
The first character of your response MUST be `{` and the last character MUST be `}`.
Do not use Markdown, code fences, prose before or after JSON, XML, DSML, or tool-call syntax.
```

**Удалить.** Необязательные текстовые примеры оформления JSON и формулировки,
которые разрешают «ответ» вне schema. Не дублировать противоречивые общие
требования к формату в разных фазах.

**Почему закрывает проблему.** Фиксирует не только семантику JSON, но и границы
байтового контракта strict decoder, из-за которых ` ```json ` было отклонено.

**Тип.** Prompt-only; ещё надёжнее в сочетании с API structured output.

### `280…`: переход из clarify только в research

**Место изменения.** Заменить универсальный clarify/repair блок на
stage-specific блок, формируемый orchestration для текущей стадии.

**Заменить / добавить.**

```text
CLARIFY PHASE
current_stage: clarify_input
allowed_next: [research_input_data]
forbidden_next: [execution, user_feedback]

The user has answered the clarification questions. Your only valid state transition
is clarify_input -> research_input_data. Do not produce a recipe, final settings,
or an execution result in this phase.
```

Для repair вместо расплывчатого «сохрани текущий этап»:

```text
AUTHORITATIVE REPAIR CONTROL
Discard the rejected proposal. Preserve confirmed facts and current_stage.
For this attempt, allowed_next is authoritative: [research_input_data].
Return a corrected proposal for that transition only.
```

**Удалить.** Формулировку repair «сохрани текущий этап», если она не уточняет,
можно ли совершить обязательный переход; общие execution-инструкции из
clarify-подсказки.

**Почему закрывает проблему.** Убирает смысловой конфликт между сохранением
состояния и обязательным переходом, делает допустимый переход явным и исключает
преждевременный `execution`.

**Тип.** Prompt-only при условии, что runtime уже знает `allowed_next`; иначе
требуется добавить это поле в control payload.

### `65b…`: post-tool фаза и запрет DSML / повторного lookup

**Место изменения.** Полностью заменить post-tool continuation prompt после
успешного batch; не передавать ему устаревший `current_step=perform lookup`.

**Заменить / добавить.**

```text
POST-TOOL PHASE — TOOLS ARE DISABLED
All requested catalog lookups have already completed. Do not call tools again.
Do not emit DSML, function-call syntax, XML, or a request to repeat a lookup.
Use only the supplied catalog_evidence to create one JSON proposal.

Your required next artifact is catalog_handoff. It must summarize the applicable
evidence and explicitly preserve empty/not-found outcomes. The current proposal
may include a complete catalog_handoff and then set stage=execution, but it must
not produce a recipe or perform domain execution work in the same response.
```

**Удалить.** `current_step=perform lookup`, призывы «вызови инструменты» и
общие tool-инструкции из post-tool контекста.

**Почему закрывает проблему.** Согласует видимую модели задачу с фактическим
состоянием среды: lookup завершён, tools отключены, требуется только proposal.
Это уменьшает вероятность текстового DSML, который decoder воспринимает как
невалидный ответ.

**Тип.** Prompt-only для запрета поведения; требует context change для передачи
`catalog_evidence`.

### `f62… / fe4…`: подтверждённый inventory с provenance

**Место изменения.** Ввести единый блок фактов `confirmed_inventory_evidence`
для task-модели и `inventory-truth`; заменить текущую передачу плоских facts
без antecedent-вопроса.

**Добавить в task и invariant context.**

```text
confirmed_inventory_evidence:
- fact: "DF64 Gen 2 has standard espresso burrs"
  status: confirmed
  provenance:
    question: "Are the standard espresso burrs installed on the DF64?"
    answer: "Yes"
    source: user_message
```

**Заменить system prompt `inventory-truth`.**

```text
INVENTORY TRUTH CHECK
Treat a proposed inventory statement as supported only when it is present in
confirmed_inventory_evidence or confirmed_inventory_facts with provenance.
Question/answer provenance is authoritative confirmation when the answer is
unambiguous for the question. Do not reject a statement merely because its
shortened flat fact is absent when its provenance record is present.
catalog_evidence may support technical characteristics of an already confirmed
model only; it never proves ownership, existence, or availability of a device.
Evaluate inventory support only. Do not apply semantic_suspicion or
execution_result_present heuristics in this check.
```

**Удалить.** Передачу только плоского facts списка как единственного основания;
из inventory prompt — нерелевантные `semantic_suspicion` и
`execution_result_present` эвристики.

**Почему закрывает проблему.** Валидатор получает тот же подтверждающий Q/A
контекст, на который разумно опирается proposal; проверка становится про
provenance, а не про догадку по неполному факту.

**Тип.** Требует context/payload changes и обновления invariant prompt; не
решается только текстом prompt.

### `845… / b5…`: сохранение catalog evidence и exact handoff

**Место изменения.** Сохранить нормализованный результат каждого выполненного
catalog tool в control state и добавлять его во все post-tool и repair prompt.
Заменить outcome-only metadata новым блоком задачи и обновить research invariant.

**Добавить в task/post-tool/repair context.**

```text
catalog_evidence:
- query: "DF64 Gen 2"
  source: BrewMark
  result: exact
  setting_range: "0-90"
  setting_unit: NUMBER
  espresso_anchor: 10
  burr_type: FLAT
- query: "De'Longhi EC685"
  source: BrewMark
  result: empty

CATALOG HANDOFF TASK
Use catalog_evidence as the sole source for catalog-specific values.
Do not infer, replace, or repeat catalog lookups. Before requesting execution,
produce catalog_handoff that includes every applicable exact field and clearly
states empty results. Execution is forbidden until catalog_handoff is complete.
```

**Заменить research invariant prompt.**

```text
RESEARCH SUFFICIENCY CHECK
Validate catalog_handoff only against catalog_evidence attached to this request.
Require every applicable exact field from catalog_evidence before allowing
research_input_data -> execution. An empty result must be represented as empty,
not as absence of the user's device. Do not use hidden examples, remembered
catalog values, or values not present in catalog_evidence.
```

**Удалить.** Outcome metadata как единственный источник после tool calls;
hardcoded пример DF64 (`10`, `0–90`, `FLAT`) из research invariant system prompt;
требование exact handoff, если соответствующие результаты не приложены.

**Почему закрывает проблему.** Repair-модель и валидатор получают идентичное,
проверяемое доказательство. Устраняется data leakage/asymmetry: validation не
требует значения, которых task-модель не может видеть, и модель не вынуждена
выбирать между выдумыванием, запрещённым повтором lookup и отклонением.

**Тип.** Требует context/payload persistence и prompt changes; prompt-only
правка недостаточна.

### Приоритет внедрения

- **P0:** Persist `catalog_evidence` во всех repair/post-tool контекстах и
  убрать hardcoded DF64 из invariant-only prompt; добавить `allowed_next` и
  authoritative repair control.
- **P1:** Ввести фазовые prompt (`clarify`, `research`, `catalog_handoff`,
  `execution`) и жёсткий post-tool блок с отключёнными tools.
- **P2:** Усилить JSON-контракт structured output и перейти на
  `confirmed_inventory_evidence` с Q/A provenance; упростить
  `inventory-truth` до проверки поддержки факта.
