# Инварианты состояния AI-бариста

## Назначение, scope и владение

Эта спецификация определяет неизменяемые инварианты AI-бариста и проверку
всех пользовательских ответов и task proposal до их принятия. Цель — не дать
ассистенту предложить решение, которое противоречит заданным правилам, даже
когда пользователь просит об этом прямо или модель сформировала такой ответ.

Инварианты — статический набор приложения. Они существуют отдельно от
диалога, памяти, профилей и задач: не извлекаются из переписки, не изменяются
пользователем и не сохраняются как сообщение или факт. Эта спецификация —
единственный владелец их публичного состава, последовательной проверки,
repair/refusal lifecycle, read-only панели, конфигурации validator-ов и их
наблюдаемости. Общая архитектурная граница stateless LLM subagent-а определена
в [архитектуре backend](../backend-architecture/SPEC.md); этот документ задаёт
её применение для инвариантов и role-specific context policy.

[Слои памяти](../memory-layers/SPEC.md) остаются владельцем facts, extractor-а
и их персистентности; [агент-бариста](../barista-agent/SPEC.md) — владельцем
общего жизненного цикла чата и title; [конечный автомат задач](../task-state-machine/SPEC.md)
— владельцем task state и плана. Эти контракты дополняются настоящим
документом, а не заменяются им. Данные каталога BrewMark MCP не являются
доказательством наличия оборудования у пользователя и не заменяют проверки
availability/inventory-truth.

В scope входят только следующие публичные кофейные инварианты:

| ID | Инвариант | Обязательное поведение |
| --- | --- | --- |
| `equipment-availability` | Доступность оборудования | Ассистент не предлагает безусловный рецепт, для которого требуется устройство, явно названное недоступным или сломанным. Он может предложить совместимую альтернативу или условный вариант, не утверждая наличие устройства. |
| `beans-availability` | Доступность зёрен | Ассистент не предлагает безусловный рецепт с зёрнами, которые пользователь явно назвал закончившимися или недоступными. Он запрашивает альтернативу либо формулирует условный вариант. |
| `inventory-truth` | Достоверность инвентаря | Ассистент не утверждает, что конкретное кофейное оборудование или зёрна есть у пользователя, если это не подтверждено текущим input или применимыми facts. |

Отсутствие fact не доказывает недоступность ресурса: оно означает «неизвестно».
Неизвестное устройство или зёрна можно упомянуть только как явное условие.
Системная инструкция `inventory-truth` проверяет только утверждения о наличии,
отсутствии или доступности оборудования и зёрен. Она не расширяет проверку на
характеристики, настройку, тип комплектующих, каталожные свойства или иные
предметные детали. Сырой эллиптический ответ пользователя без переданного
antecedent-вопроса не подтверждает новый факт и не служит основанием для
дополнительного вывода о предметных деталях; в таком случае checker оценивает
только доступные ему утверждения своего scope.
`research-sufficiency` — внутренний LLM-backed guard только для
research-only `task_proposal` в `research_input_data`. Он принимает candidate
лишь если тот ограничен исследованием оборудования и, при отсутствии tool-call,
подтверждённые input/facts/snapshot достаточны для следующего execution и не
называют конкретную модель grinder/brewer. Его неизменяемый контекст
дополнительно содержит только безопасный batch-статус MCP-попыток этого шага и
флаг `semantic_suspicion` deterministic prefilter-а. Batch-статус равен
`no tool`, `success`, `partial_failure` или `failure`; per-call статусы
ограничены `success`, `empty`, `ambiguous` и broker error. Guard не получает
MCP arguments или result/content, не создаёт fact, не добавляет типизированную
модель оборудования и не сохраняет статусы MCP-попыток.
Task transition и final-validation guard — тоже инварианты исполнения, но они
внутренние и не показываются в пользовательской панели. Детерминированная
проверка может окончательно отклонить только структурный факт (недекодируемый
proposal, запрещённое ребро stage, status или plan-снимок). Если она по тексту
proposal лишь подозревает предметное выполнение в research (включая совпадение
слов вроде «рецепт»), это `semantic_suspicion`, а не `violation`: перед
окончательным решением candidate обязательно проходит существующий LLM-backed
`research-sufficiency` checker. Только его строго декодированный `violation`
может запустить repair.

Не входят: редактирование или создание правил пользователем, история
проверок в UI, batching semantic checks, типизированный inventory, проверка
title и memory extractor общим validator-ом, а также legacy summary/facts
контур.

<!-- ac-section: core-flow -->
## Инвариантный pipeline

Инстанс agent/orchestrator получает упорядоченный список `Invariant`.
Каждый элемент имеет уникальный ID, публичные название и описание, перечень
применимых subjects и операцию проверки. Допустимые subjects: `user_input`,
`chat_candidate` и `task_proposal`. Проверка получает только конкретный subject
и неизменяемый контекст: статические правила, applicable facts, текущий input
и, для task proposal, подтверждённый server snapshot задачи. Она не читает и
не изменяет хранилище самостоятельно.

Проверка одного инварианта возвращает один из outcomes:

| Outcome | Смысл |
| --- | --- |
| `allow` | Subject не нарушает правило. |
| `violation` | Содержит `invariant_id`, безопасную причину и `repair_instruction`. |
| `error` | Содержит только безопасную техническую категорию; решение о соответствии не принято. |

Applicable инварианты выполняются последовательно, без batching. Каждый
semantic invariant запускается даже если предыдущий уже вернул `violation` или
`error`. Все полученные violations собираются в один список. Если хотя бы один
checker вернул `error`, техническая ошибка имеет приоритет: repair не
запускается, refusal не создаётся, и ни одно новое durable state не принимается.
Реализация отдельного правила может быть deterministic, LLM-backed или
CLI-backed; независимо от способа проверки она соблюдает этот outcome contract
и не владеет orchestration.

### Lightweight LLM subagent

Все active single-call LLM роли используют общую stateless lightweight
subagent boundary из `backend-architecture`. Ни один subagent не владеет state,
commit, retry, task routing, UI или side effects хранилища; это делает
orchestration единственным владельцем repair и принятия результата.

| Роль | Вход и context policy | Decoder/result |
| --- | --- | --- |
| Chat candidate | Базовые правила, active profile, global/project facts и последние N сообщений текущего чата. | Непустой текст candidate. |
| Task candidate | Контекст chat candidate плюс подтверждённый snapshot задачи и результаты шагов текущей попытки. | Строгий task proposal. |
| Title | Только первый принятый input. | Валидный plain-text title либо safe decode error. |
| Memory extractor | Предыдущие snapshots и последние N сообщений текущего чата с финальной парой. | Строгий полный snapshot facts. |
| Semantic invariant | Проверяемый subject, применимые facts и task snapshot при наличии. | Строгий `allow`/`violation` verdict конкретного правила. |

Chat и task candidate subagent-ы получают публичные правила как неизменяемое
system-ограничение и явно учитывают их при подготовке candidate. Это не
заменяет post-validation: только verdict-ы инвариантов разрешают принять
candidate.

Для history сохраняются действующие специализированные окна: chat/task — N
сообщений текущего чата, extractor — N сообщений и финальная пара, title —
только input, invariant — только subject, applicable facts и task snapshot.
Сами static invariants, facts, candidate и task snapshot не обрезаются. Если
они не помещаются в контекст модели, subagent возвращает `context_limit`; это
fail-closed error, а не повод молча удалить значимую информацию.

Три публичных кофейных правила и внутренний `research-sufficiency` реализуются
отдельными LLM-backed `Invariant`-ами. Их естественно-языковая семантика и
свободные facts не дают надёжно детерминировать совпадение названий ресурсов
или достаточность сведений для выполнения. Task transition и final-validation
guard реализуются deterministic invariant-ами: они сравнивают target proposal
только с актуальным подтверждённым server snapshot, а не с состоянием,
присланным клиентом.

`research-sufficiency` получает полный research-only candidate, применимый task
snapshot, принятый input и facts, но не MCP arguments/result/content. Он
одновременно оценивает достаточность research и её границу: различает
фактически выданный пользователю рецепт, параметры или завершённый execution
результат (`violation`) от описания будущего execution-действия или следующего
шага (`allow`). Точные значения, явно перенесённые из успешного BrewMark
catalog result в `catalog handoff` — диапазон и единица шкалы, anchor,
brew-method default, batch limit, filter adjustment или тип жерновов — являются
каталожными характеристиками, а не параметрами готового рецепта. Поэтому они
допустимы в research-only proposal только как подписанные стартовые опоры для
будущего execution, без дозы, выхода, времени, температуры или инструкции
заваривания. Само слово «рецепт» и любое другое ключевое слово не является
ни достаточным, ни необходимым условием `violation`. Поэтому
`semantic_suspicion` обязательно доходит до этого checker-а, но не создаёт
отдельный checker или provider-вызов.

Системная инструкция `research-sufficiency` не содержит названий моделей,
марок, числовых диапазонов, anchor или иных конкретных каталожных значений как
правила либо примера. Она не требует exact catalog handoff и не отклоняет
research-only proposal из-за отсутствия точного значения, которого нет в
переданном checker evidence. Если точное значение присутствует в candidate как
явно маркированная каталожная опора, guard проверяет лишь границу research
(отсутствие рецепта и параметров), не подменяя evidence собственным знанием о
каталоге.

### Обычный chat

Перед созданием durable user input запускаются все три pre-validation semantic
checks. Они отклоняют только однозначный запрос на нарушение; неоднозначный
input пропускается в generation и всё равно проверяется после неё.

1. Если хотя бы один pre-check вернул `error`, input не создаёт durable pair,
   task, memory update или title claim. UI оставляет локальный input с
   безопасной технической ошибкой и ручным retry тем же input/ID.
2. Если pre-check вернул один или несколько violations без error, создаётся
   user/assistant pair с шаблонным безопасным refusal. Для пары запускается
   один обычный memory extractor, затем пара и допустимый snapshot facts
   принимаются по существующему memory contract. Main candidate не вызывается.
3. Если все pre-checks вернули `allow`, основной subagent создаёт candidate.
   Затем три post-validation checks последовательно проверяют candidate.
4. Post violations передаются одной следующей generation-попытке как полный
   список `invariant_id`, причины и repair instructions. Непринятый candidate
   не отображается, не попадает в историю и не попадает в extractor.
5. Допускаются исходный candidate и не более двух repair candidates. После
   `allow` один memory extractor обрабатывает только финальную пару; pair и
   snapshot facts принимаются по текущему atomic contract.
6. Если все три candidate нарушили инварианты, вместо третьего rejected
   candidate принимается шаблонный refusal и для него выполняется один
   extractor. Если любой post-check вернул `error`, extractor и commit не
   запускаются.

Refusal перечисляет понятные названия всех сработавших правил и предлагает
безопасный следующий шаг: назвать доступное оборудование/зёрна либо уточнить
инвентарь. Он не раскрывает prompt, provider response или внутреннюю причину
checker-а. Repair получает подробную structured причину как данные и не может
превратить её в инструкцию более высокого приоритета.

### Task flow

Для input task сначала выполняются те же три pre-checks. Новая задача не
создаётся до их успешного `allow`; при pre violation создаётся только
user/refusal pair, а при pre error не принимается никакого durable state.

Для каждого task candidate pipeline имеет порядок: строгий proposal decode →
deterministic transition invariant от актуального подтверждённого server
snapshot, включая `semantic_suspicion` без verdict-а →
`research-sufficiency`, если candidate является research-only proposal → три
sequential public semantic checks → final-validation decision,
если цель proposal — `execution → user_feedback`. `research-sufficiency`
получает только описанный выше safe MCP status и участвует в том же
allow/violation/error lifecycle, что и другие semantic checks. Transition guard
разрешает только граф и status-переходы, определённые
`task-state-machine`; равный stage трактуется как отсутствие transition. Для
`clarify_input` семантическое сопоставление принятого user input с последним
вопросом и решение о достаточности принадлежат task LLM по контракту
`task-state-machine`; transition guard не добавляет server-side эвристику
ответа. Для `execution → user_feedback`
final-validation guard требует completed plan и успех всех обязательных
semantic checks; только тогда server детерминированно формирует публичный
`validation_result` и безопасный summary. Это не поле task proposal: неизвестное
поле `validation_result` отклоняется строгим decoder-ом. Этот gate не создаёт
самостоятельный provider-вызов. Все применимые violations, включая
неразрешённый transition, подтверждённый `research-sufficiency` violation или
неуспех gate, передаются в один repair. `semantic_suspicion` без LLM verdict
не расходует repair и не блокирует candidate. При timeout, network/provider,
strict-decode, context-limit или иной ошибке `research-sufficiency` весь шаг
завершается fail-closed `invariant_validation`: repair/refusal не создаётся,
candidate не принимается, user/refusal pair, facts, task state и plan не
меняются; пользователь вручную повторяет исходный шаг. Даже при
violation/error transition все semantic checks выполняются; error имеет
приоритет над violations.

В `research_input_data` до decode допустимый response может быть batch из
одного–четырёх уникальных BrewMark tool-calls по `brewmark-mcp`, когда для
следующего выполнения нужен каталожный факт об оборудовании; lookup названной
конкретной модели grinder/brewer обязателен. Если дополнительные сведения не нужны
и конкретная модель не названа,
допустим только research-only proposal, определённый `task-state-machine`.
После всех tool results единственный synthesis обязан пройти полный pipeline как
research-only proposal; tool-call не создаёт durable state. Ни этот proposal,
ни synthesis не могут содержать рецепт, параметры, иной предметный результат
или завершение execution-пункта. Принятый переход в `execution` завершает
research; следующий execution step начинается отдельным LLM-вызовом. Неизвестный,
дублированный, batch больше четырёх, неверно сформированный или вне-stage
tool-call отклоняется как невалидный candidate и не вызывает MCP.

`research-sufficiency` получает explicit `proposed_stage` из строго
декодированного candidate и оценивает только этот предложенный переход.
Если `proposed_stage=research_input_data`, guard не вправе объявлять переход в
`execution` или выполнение лишь по тексту candidate; намерение выполнить
рецепт на будущем execution step само по себе допустимо. Реальный рецепт,
параметры или иной предметный output остаются violation research step
независимо от `proposed_stage`.

На один task step допускаются исходный candidate и максимум два repair
candidates: только строго декодированный semantic `violation` может расходовать
один из двух repair. На всю пользовательскую task-chain допускается не более
восьми task candidate/repair/tool-loop provider-вызовов: repair и оба вызова
tool loop расходуют этот общий бюджет. LLM-backed invariant verdict, включая
`research-sufficiency`, не расходует ни этот бюджет, ни repair budget.
Pre/post checks, один final extractor и independent title в этот бюджет не
входят. После каждого разрешённого task candidate следующий автономный шаг
возможен только когда proposal прошёл все проверки; промежуточные output,
proposal, plan и validation result не публикуются.

После конечного разрешённого task candidate один extractor обрабатывает
совокупный output. Затем user/assistant pair, facts, task state и plan
принимаются одним atomic commit. При исчерпании repair или общего бюджета
сохраняется только user/refusal pair после одного extractor; новая task не
создаётся, а существующие task state и plan не меняются. При technical
validator error не принимаются ни pair, ни facts, ни task state/plan. Ошибка
extractor-а после конечного разрешённого task candidate также не принимает ни
одну часть task результата и оставляет последний task snapshot без изменений;
пользователь вручную повторяет шаг.

### Отмена, title и конкурентность

Pre-validation, generation, repair, post-validation и extractor принадлежат
одной отменяемой пользовательской операции. Удаление сущности, подтверждённая
пауза или отмена побеждают поздний результат: он не создаёт pair, facts, title
или task transition. После каждого внешнего шага orchestration проверяет, что
операция всё ещё владеет актуальным server snapshot.

Title запускается только после финального accepted commit: обычного ответа или
template refusal. Он остаётся независимым асинхронным вызовом и не задерживает
уже принятый response, но не запускается для technical validator error или
непринятого candidate.

<!-- ac-section: external-contract -->
## Внешнее состояние и UI

Backend предоставляет read-only контракт списка публичных инвариантов. Каждый
элемент содержит только стабильный ID, название и описание. Список строится из
фактически injected публичных инвариантов; transport settings, prompts,
repair instructions и internal transition invariant в него не входят. Список
одинаков для всех browser-сеансов и не является частью chat history, memory
или persisted user state.

В header group выбранного чата, определённой `task-state-machine`, рядом с
toggle «Задачи» и действием «Память» доступен toggle «Инварианты».
Панель показывает ровно три публичных кофейных правила, не содержит элементов
редактирования или истории проверки и не показывает raw reasons. Пока список
загружается, показано loading-состояние; ошибка чтения показывает retryable
ошибку только панели и не блокирует chat. Отказ и техническая ошибка остаются
в хронологии/статусе конкретной попытки, а не в панели.

Pre refusal и exhaustion refusal отображаются как обычная assistant-реплика.
Техническая ошибка checker-а имеет внешнюю безопасную категорию
`invariant_validation` и действие «Повторить» для того же локального input/ID.
Она не выдаётся за пользовательскую validation error и не утверждает, что
запрос нарушил правило.

<!-- ac-section: states-and-errors -->
## Ошибки, конфигурация и наблюдаемость

Секция `invariant_validation` обязательна для запуска этого MVP. Она задаёт
transport/model и runtime `request_timeout=60s` для дешёвой модели validator-ов.
Каждый semantic invariant строит собственный static system prompt, но выполняет
ровно один последовательный вызов через эту конфигурацию: automatic retry после
timeout, network/provider или decoder error запрещён.
Невалидная, отсутствующая или неполная конфигурация не позволяет backend
стартовать; частично незащищённый режим не допускается.

Каждый subagent-вызов и каждый invariant verdict логируются структурированно с
correlation ID, project/chat ID при наличии, purpose, invariant ID, subject,
phase (`pre`/`post`), candidate/repair index, результатом, безопасной
категорией ошибки и длительностью. Логи не содержат raw input, candidate,
facts, task output, reason, prompt, provider payload или secrets. В журнале
администратора новые безопасные метаданные могут отображаться как обычные
события; они не раскрывают содержимое проверки.

| Ситуация | Наблюдаемое поведение |
| --- | --- |
| Однозначный pre conflict | Main candidate не вызывается; сохраняются user/refusal pair и результат одного extractor-а. |
| Post violation | Пользователь не видит candidate; запускается один repair с полным списком violations. |
| Repair исчерпан | Сохраняется template refusal; task proposal/state не принимается. |
| Checker timeout/network/provider/invalid/context error | Все applicable checks текущей фазы завершаются; результат — `invariant_validation`, без repair/refusal, pair, facts, task state/plan и title. Пользователь вручную повторяет input. |
| Extractor error после accepted chat candidate или refusal без изменения task state | Существующее правило memory-layers сохраняется: pair принимается, старые snapshots остаются, UI показывает memory error. Для конечного разрешённого task candidate действует атомарное правило task flow: не принимается ни pair, ни facts, ни task result. |
| Cancel/pause/delete | Неподтверждённый pipeline не публикуется; поздний result игнорируется. |

<!-- ac-section: connectivity -->
## Связность и восстановление

При offline, timeout или иной транспортной ошибке checker-а, включая отсутствие
verdict к 60 s, применяется fail-closed outcome `invariant_validation`: решение
не кэшируется как allow и не даёт принять candidate. Повтор выполняет все
применимые checks заново с тем же local input/ID и актуальным server snapshot.
Автоматический retry не допускается.

<!-- ac-section: adaptive -->
## Адаптивность и доступность

На ширине 390 px и 1440 px toggle и панель инвариантов не создают
горизонтальную прокрутку viewport. Длинные названия и описания переносятся или
имеют собственную прокрутку. Toggle, retry панели и retry технической ошибки
доступны с клавиатуры, touch и мышью; loading, error и отказ программно
объявляются assistive technology. Панель не требует выбранной памяти и не
мешает навигации по чату.

<!-- ac-section: permissions -->
## Безопасность и изоляция

Публичные правила приложения не содержат пользовательских данных. Facts и
task snapshots, передаваемые LLM-backed checker-ам, принадлежат только
текущему browser-сеансу и выбранному проекту/чату. Они сериализуются как
недоверенные данные, не дают checker-у права менять системные правила,
исполнять инструменты, читать данные другого владельца или раскрывать
credentials.

Факты о доступности ресурсов остаются свободными strings. По контракту
`memory-layers` подтверждённые equipment/beans facts, включая «нет»,
«сломано» и «закончилось», являются global по умолчанию; project scope возможен
только при явном пользовательском ограничении. Project fact имеет приоритет
над global fact, а имеющиеся historical project facts не повышаются
автоматически до global. Эта модель намеренно не вводит typed inventory.

<!-- ac-section: destructive-actions -->
## Деструктивные действия

N/A: feature не даёт пользователю создавать, редактировать, удалять или
откатывать инварианты. Очистка facts и удаление чата/проекта остаются в scope
`memory-layers` и `barista-agent` соответственно и не расширяются здесь.

<!-- ac-section: deep-link -->
## Deep link и intake

N/A: панель инвариантов не имеет отдельного URL и не принимает внешний intake.
Она восстанавливается вместе с выбранным чатом по существующему browser-сеансу;
правила загружаются заново из read-only контракта приложения.

<!-- ac-section: performance -->
## Лимиты, свежесть и verification strategy

Лимиты generation жёсткие: обычный chat использует не более трёх candidate
calls (исходный + два repair), task-chain — не более восьми task
candidate/repair/tool-loop LLM-вызовов, включая repairs. На каждый user input
выполняются три отдельных sequential pre-checks. На каждый candidate выполняются
три отдельных sequential public post-checks; research-only proposal добавляет
ровно один `research-sufficiency` check, в который при наличии передаётся
`semantic_suspicion`. Final extractor запускается один раз только
для accepted pair/refusal; title и semantic checks независимы и не расходуют
candidate budgets.

Runtime budget каждого validator-вызова — 60 s: valid verdict, пришедший позже
30 s, но не позже 60 s, обрабатывается штатно; после 60 s это окончательный
fail-closed timeout без automatic retry. Абсолютный latency-бюджет всей операции
не задан: последовательность validator-вызовов намеренно увеличивает latency
ради fail-closed гарантии. UI остаётся pending до final outcome и не показывает
intermediate candidate/repair. При `context_limit`
содержимое, необходимое для соблюдения инвариантов, не обрезается.

Обязательный acceptance gate состоит из controlled-provider unit/integration
и browser-проверок ниже, а также real LLM smoke на настроенной validation
модели. Smoke запускает четыре фиксированных contexts (недоступное
оборудование, недоступные зёрна, неподтверждённый inventory, допустимый
условный вариант) через все три public semantic checker-а и три research
fixtures (нужен MCP fact / сведения достаточны / semantic suspicion): не более 15 внешних
LLM-вызовов. Каждый verdict должен успешно декодироваться и дать ожидаемый
allow/violation по своему ID. Если требуемая реальная конфигурация недоступна,
acceptance gate не пройден, а не заменяется зелёными controlled tests.

<!-- ac-section: acceptance-criteria -->
## Acceptance criteria

- **AC-INV-01.** Запущенный agent содержит ровно три публичных кофейных
  инварианта с ID, названием и описанием, отдельно от каждой chat history,
  profile, memory и task state. Read-only API возвращает эти три элемента, но
  не возвращает internal transition guard, prompt или transport fields.
  Проверка: application/API contract.
- **AC-INV-02.** Input с явным требованием рецепта для отсутствующего
  оборудования проходит все три pre-checks, не делает chat candidate call,
  сохраняет одну user/refusal pair и один extractor outcome. Refusal называет
  `equipment-availability` и предлагает сообщить доступное оборудование.
  Проверка: controlled provider + persistence integration.
- **AC-INV-03.** Candidate, нарушающий `beans-availability` или
  `inventory-truth`, не виден и не сохраняется. Все три post-checks
  выполняются последовательно; один repair получает полный список violations.
  Разрешённый repair становится единственным показанным assistant output и
  запускает ровно один extractor. Проверка: controlled provider call trace +
  persistence integration.
- **AC-INV-04.** При violations двух или трёх правил в одной фазе все
  applicable checker-ы запускаются, а ровно один repair получает все их ID и
  repair instructions. Проверка: controlled provider prompt/call inspection.
- **AC-INV-05.** Timeout, network, provider, invalid verdict или
  `context_limit` любого checker-а не прекращает запуск остальных applicable
  checker-ов этой фазы, но возвращает `invariant_validation`, не создаёт
  durable pair, facts, task state/plan или title и даёт retry того же local
  input/ID. Проверка: fault injection + browser + persistence integration.
- **AC-INV-06.** После исходного candidate и двух неуспешных repairs обычный
  chat сохраняет только user/template-refusal pair и один extractor outcome;
  все три rejected candidates отсутствуют из history и facts input extractor-а.
  Проверка: controlled provider + persistence integration.
- **AC-INV-07.** Каждый task proposal декодируется, сравнивается с актуальным
  подтверждённым task snapshot deterministic transition/final-validation
  guard-ом и затем проходит все три public semantic checks. Research-only
  proposal дополнительно проходит внутренний `research-sufficiency` guard с
  безопасными batch/per-call статусами MCP-попыток; эти статусы и какой-либо
  typed equipment domain не сохраняются. Target transition и client request не
  являются источником истины; семантическое решение о достаточности clarify
  принимает task LLM, а не server-side heuristic. `validation_result` не входит в
  proposal: неизвестное поле отклоняется, а после gate его детерминированно
  создаёт server. Некорректный transition, jump, неуспех gate, ложное
  подтверждение или violation `research-sufficiency` включаются в один repair
  и не меняют plan/state до atomic commit. Gate не создаёт дополнительный
  provider-вызов. Проверка: state-machine integration + controlled provider +
  storage spy.
- **AC-INV-08.** На одну task user attempt расходуется не более восьми task
  candidate/repair/tool-loop LLM-вызовов и не более трёх candidates на один task
  step. При исчерпании сохраняется user/template-refusal pair после одного
  extractor-а; новая task не создаётся, существующие task state и plan
  неизменны. Проверка: controlled provider call count + persistence integration.
- **AC-INV-09.** Подтверждённые availability facts об equipment/beans по
  умолчанию попадают в global memory, включая `нет`, `сломано` и
  `закончилось`; явно project-scoped fact остаётся в project memory и имеет
  приоритет в конфликте. Historical project facts не повышаются при обновлении.
  Проверка: extractor fixture + restart/upgrade integration.
- **AC-INV-10.** Chat/task history применяет действующие N-window policies,
  а facts, candidate, task snapshot и static rules не обрезаются. Их
  переполнение возвращает fail-closed technical outcome и не публикует
  candidate. Проверка: controlled provider input inspection + context-limit
  fault injection.
- **AC-INV-11.** Панель «Инварианты» рядом с «Памятью» показывает три
  read-only правила, loading и retryable error, не блокируя chat. На 390 px и
  1440 px она доступна с клавиатуры и screen reader и не создаёт горизонтальный
  overflow. Проверка: browser accessibility test.
- **AC-INV-12.** Missing/invalid `invariant_validation` configuration или
  неуникальные/неполные injected invariant metadata не позволяют backend
  начать обслуживание запросов. Проверка: startup configuration test.
- **AC-INV-13.** Лог каждого subagent/invariant вызова содержит correlation ID,
  назначение, phase, invariant ID при наличии, candidate index, result,
  category и duration, но не содержит raw input/candidate/facts/reason/prompt,
  provider payload или credentials. Проверка: log inspection.
- **AC-INV-14.** Delete, pause или cancellation в любой pre/generate/repair/
  post/extractor фазе запрещает поздний pair, facts, title или task transition;
  UI показывает только последний подтверждённый snapshot. Проверка: controlled
  blocking provider + browser race + persistence integration.
- **AC-INV-15.** Mandatory real LLM acceptance smoke выполняет 15 или меньше
  вызовов настроенной validation-модели: четыре фиксированных context через три
  public semantic checker-а и три research fixture через
  `research-sufficiency`. Каждый verdict строго декодируется и даёт ожидаемый
  allow/violation по своему ID. Отсутствие доступа к этой модели означает
  незавершённый acceptance gate. Проверка: live LLM run с подсчётом фактических
  вызовов.
- **AC-INV-16.** Controlled-provider trace доказывает, что chat candidate,
 task candidate, title, extractor, каждый из трёх public semantic checker-ов и
  `research-sufficiency` при его применимости делает не более одного provider
  call на запуск своей роли, включая обработку `semantic_suspicion`; decoder error возвращает
  safe technical outcome и не вызывает commit или retry из subagent-а.
  Проверка: controlled provider + storage spy.
- **AC-INV-17.** `research-sufficiency` получает research-only candidate,
  подтверждённые input/facts/task snapshot, safe batch/per-call MCP statuses и
  `semantic_suspicion` при его наличии. В fixture, где для execution требуется каталожный факт либо input
  называет конкретную модель grinder/brewer, status `no tool` даёт `violation`
  и один repair без commit. В fixture с достаточными сведениями без названной
  конкретной модели он даёт `allow` без MCP-вызова и без нового equipment state.
  При `partial_failure` guard не получает content/results и может дать `allow`
  только если успешные вызовы и подтверждённые сведения достаточны независимо от
  failed/empty/ambiguous calls. Проверка: controlled guard/provider + MCP batch
  trace + persistence inspection.
- **AC-INV-18.** В research fixture два BrewMark lookup завершаются
  `success` с `matchStatus=empty`, а synthesis возвращает корректный
  research-only переход `research_input_data → execution` с текстом о будущем
  действии «перехожу к сборке рецепта», без рецепта, параметров или completed
  execution-пункта. Deterministic prefilter фиксирует `semantic_suspicion`,
  которое передаётся в единственный `research-sufficiency` validator; его
  `allow` принимает переход без repair и без дополнительного provider-вызова.
  Следующий отдельный candidate call начинается только с
  подтверждённого `stage=execution`. При timeout, provider/network,
  context-limit или нестрогом verdict этого validator-а нет repair/refusal,
  pair, facts или изменения task snapshot; UI получает
  `invariant_validation` и ручной retry. Проверка: controlled LLM + MCP trace,
  fault injection, provider call count и persistence/browser inspection.
- **AC-INV-19.** При ответе `research-sufficiency` через 31–59 s ровно один
  provider call возвращает строгий valid verdict и pipeline обрабатывает его
  как обычный `allow` или `violation`; второй вызов и автоматический repair при
  `allow` отсутствуют. При отсутствии verdict к 60 s ровно тот же единственный
  вызов завершается `invariant_validation`, не создаёт repair/refusal, pair,
  facts или изменения task snapshot и предлагает ручной retry. Проверка:
  controlled delayed provider (31 s и 61 s), call-count trace и
  persistence/browser inspection.

- **AC-INV-20.** В candidate с explicit
  `proposed_stage=research_input_data` и текстом о будущем recipe work
  `research-sufficiency` получает этот stage и возвращает `allow`, а не
  выдумывает переход в execution. Candidate с фактическим recipe или числовыми
  параметрами в том же stage получает `violation`. Проверка: controlled
  validator prompt/verdict inspection + repair/persistence trace.

- **AC-INV-21.** Research-only candidate с exact BrewMark catalog handoff,
  содержащим переданные в evidence диапазон, единицу шкалы, anchor и тип
  жерновов, получает от `research-sufficiency` `allow`, если не содержит
  готового рецепта или инструкции заваривания. Тот же candidate с дозой,
  выходом, временем, температурой либо инструкцией приготовления получает
  `violation`. Проверка: controlled validator fixtures + repair/persistence
  trace.

- **AC-INV-22.** System instruction `inventory-truth` не рассматривает
  характеристики оборудования, настройку, тип комплектующих или каталог как
  утверждения о наличии инвентаря. В fixture с raw ответом «да» без переданного
  antecedent-вопроса checker не выводит из него новый факт и не создаёт
  violation по предметной детали вне своего scope. Проверка: controlled
  validator prompt/verdict inspection.

- **AC-INV-23.** System instruction `research-sufficiency` не содержит
  конкретных моделей или каталожных чисел. В fixture, где checker evidence не
  включает точных каталожных значений, research-only переход не получает
  violation только за отсутствие exact handoff; fixture с переданным handoff
  по-прежнему получает verdict только по research-границе, а не по совпадению
  со скрытым значением. Проверка: system-instruction inspection + controlled
  validator fixtures.

## Открытые procedural gaps

- **Gap-INV-01 — owner: Human.** В репозитории отсутствует валидный
  `.tasks/QUEUE`. Это блокирует создание queue-prefixed `TASK.md`,
  `DECISIONS.md`, `PROGRESS.md` и `QA.md`; queue не может быть угадана или
  создана в рамках этой задачи. Спецификация остаётся готовым SSOT до появления
  валидной очереди.

Подготовка этой спецификации не включает изменения исходников, тестов,
конфигурации, runtime-данных, Git-ветки или запуск разработки.
