# План рефакторинга backend под SOLID и активные спецификации

## Статус и границы

**Статус на 2026-09-18: активный backend переведён на новую архитектуру.**
Реализация выполнена по прямому запросу пользователя в главной сессии.
Результаты и границы проверок — в [отчёте](backend-refactoring-progress.md),
решения по legacy — в [инвентаре](backend-refactoring-inventory.md).
Полное удаление оставшихся исторических session/agent реализаций относится к
отдельному retirement PR этапа 8. Очереди `.tasks/QUEUE` в репозитории нет,
поэтому `working-with-specs` и task-артефакты неприменимы.

Цель — разложить активный backend на небольшие, проверяемые части, не меняя
внешний HTTP-контракт, browser-session ownership, конфигурационный контракт,
формат актуального persisted state или семантику API, кроме отдельных
спецификационных несоответствий из раздела «Наблюдения и кандидаты gaps». Рефакторинг считается
завершённым лишь когда все active AC четырёх спецификаций подтверждены, а не
когда старые тесты стали зелёными.

Не входят в план: новые пользовательские сценарии, новый SDK/провайдер,
streaming, multi-instance/distributed locking, смена БД, миграция исторических
данных за пределами прямо предписанного reset из AC-MEM-12, изменение публичных
маршрутов или форматов без backward-compatible обоснования. Повторные попытки
остаются только ручными; скрытых retries upstream-вызовов не добавлять.

Источники обязательного поведения: [агент-бариста](../.specs/barista-agent/SPEC.md),
[слои памяти](../.specs/memory-layers/SPEC.md),
[персонализация](../.specs/personalization/SPEC.md) и
[автомат задач](../.specs/task-state-machine/SPEC.md). Владение и приоритет:
`memory-layers` authoritative для проектов, памяти, main prompt/extractor и
очистки; `personalization` — для профилей; `barista-agent` — для общего чата,
title и базовых ошибок; `task-state-machine` — для задач. При конфликте следуем
явному приоритету этих документов. [Deprecated README](../.specs/deprecated/README.md)
— только историческая справка, не источник активного контракта.

### Проверенные факты исходной точки

Этот раздел описывает состояние до рефакторинга; ссылки на удалённые
`memory/store.go` и `httpapi/handler.go` обозначают исходные реализации.

* [`cmd/api-server/main.go`](../backend/cmd/api-server/main.go) поднимает
  [`memory.Open`](../backend/internal/memory/store.go), затем
  [`httpapi.NewMemory`](../backend/internal/httpapi/memory_handler.go); активные
  маршруты обслуживает `memoryHandler`.
* [`internal/memory/store.go`](../backend/internal/memory/store.go) (~1602
  строк) одновременно содержит публичные
  DTO, CRUD проектов/чатов/профилей, task FSM, prompt/extractor, JSON
  persistence, `sync.Mutex`, cancellation, title и копирование состояния.
* Старые [`httpapi/handler.go`](../backend/internal/httpapi/handler.go),
  [`session.Store`](../backend/internal/session/store.go) и
  [`agent.Conversation`](../backend/internal/agent/agent.go) не
  подключены в `main`, но `SnapshotLoader`, `decode` и `method` из старого
  handler-а используются активным контуром.
* Текущий storage — один JSON-файл с атомарной записью temp+sync+rename.
  `go test -race ./...` проходил в предыдущем review; это полезный сигнал о
  гонках, но не доказательство соответствия спецификациям.
* Playwright сейчас выбирает только `memory.spec.mjs` и `profiles.spec.mjs`;
  это не покрытие task state machine.

Ниже «факт» означает наблюдение исходников/тестов. Причины и поведение каждого
gap надо воспроизвести controlled fixture до исправления; нельзя считать
догадку или зелёный unit-test доказательством.

## Измеримые критерии архитектуры

1. `cmd/api-server` остаётся composition root: flags/config, создание adapter-ов,
   запуск/корректная остановка. В нём нет бизнес-правил, JSON DTO или route
   dispatch.
2. HTTP-слой только разбирает/валидирует transport, берёт session/correlation ID,
   вызывает use case и маппит известные безопасные ошибки. Он не строит prompt,
   не меняет state и не вызывает LLM напрямую.
3. Проверка import-графа подтверждает: domain не импортирует `net/http`, `os`,
   `encoding/json`, config или LLM transport; application не импортирует
   concrete adapters; wiring concrete зависимостей живёт только в `main`.
4. Каждый use case зависит максимум от нужных ему узких портов, объявленных у
   потребителя. Никаких `Repository` «на всё», service locator или интерфейсов
   «на будущее». Замена LLM/storage в test не требует сети/файловой системы.
5. У каждого внешнего LLM-вызова есть `context`, цель (`chat`, `title`,
   `memory_extractor`, позднее `task_step`), timeout из snapshot и безопасная
   категория ошибки. Вызов выполняется без state mutex.
6. Каждый успешный бизнес-commit — один сохранённый снимок. До него наружу не
   выдаются новые response, task transition, plan или memory snapshots.
7. Актуальные JSON snapshots и HTTP contract fixtures до/после механических
   этапов идентичны семантически. До первой записи fixture-набор определяет
   policy для unknown fields/version (retention или безопасный reject);
   round-trip проверяет выбранную policy, не удаляя данные неявно.
8. Один общий contract suite проходит и для repository fake, и для JSON adapter:
   одинаковы ownership, commit/restore, storage failure и cancellation outcome.
   Новая policy/provider подключается composition-ом без правки core use case.
   Эти критерии проверяют Liskov/DIP/Open-Closed по поведению, а не числу
   каталогов, типов или строк.

## Практическая целевая структура

```
backend/
  cmd/api-server/                    # только wiring и lifecycle
  internal/
    domain/
      chat/                           # Chat, Message, title/memory status, инварианты
      project/                        # Project и ownership
      profile/                        # built-in/custom profile и правила
      task/                           # Task, Plan, валидатор/переходы, без LLM
      memory/                         # typed FactsSnapshot и его инварианты
    application/
      workspace/                      # projects/chats/profiles/read/clear
      conversation/                   # обычная send/retry/title orchestration
      taskflow/                       # route/step/pause/resume и commit protocol
    adapters/
      statejson/                      # versioned JSON repository, decode/atomic save/restore
      openai/                         # concrete CompletionClient через internal/llm
      extractjson/                     # raw JSON decoder на infrastructure boundary
    httpapi/                           # активный handler, DTO и безопасные HTTP errors
    observability/                    # журнал/структурированные записи
    config/                            # YAML и env
```

Имена каталогов ориентировочны: допускается объединить очень малый domain-пакет
с его единственным application-потребителем, но не вернуть монолитный Store.
`internal/agent` и `internal/llm` сначала остаются адаптерной реализацией,
чтобы не смешивать механический перенос с переписыванием клиента.
`workspace`, `conversation` и `taskflow` — разные оркестраторы; запрещено
перенести все прежние обязанности в один `ApplicationService`.

### Направление зависимостей и порты

`httpapi → application → domain`; `adapters → application/domain`; composition
root связывает concrete implementations. DTO HTTP и disk DTO не являются domain
entities и не должны утекать в use cases.

У потребителей объявить только такие порты:

| Потребитель | Порт | Минимальная ответственность |
| --- | --- | --- |
| `application/workspace` | `StateReader`, `StateCommitter` | загрузить владельца/проект и сохранить новый validated state |
| `application/conversation` | `ConversationState`, `CompletionClient`, `TitleStarter`, `Clock`, `IDSource` | snapshot/commit turn, один completion, title lifecycle |
| `application/taskflow` | `TaskState`, `CompletionClient`, `Clock`, `IDSource` | lease/cancel, task proposal, атомарный task commit |
| `httpapi` | use-case interfaces по endpoint-группе | не зависеть от concrete Store |

Где два первых state-порта оказываются одинаковыми, их можно реализовать одним
`statejson.Repository`, но application не получает его расширенный API. `Clock`
и `IDSource` нужны только там, где проверяется порядок/идемпотентность; не
создавать абстракцию для `strings.TrimSpace`. Ошибки домена типизированы
(`validation`, `not_found`, `busy`); сбой filesystem/adapter — application/port
error, а его safe HTTP mapping остаётся единственным в `httpapi`.

### Источник решения для task proposal

Основание — [раздел «Принятое допущение» task-spec](../.specs/task-state-machine/SPEC.md):
plan формируется и уточняется текущим агентским процессом как часть обычного
результата task step; отдельный модельный вызов, внешний JSON API и ручное
редактирование не требуются. Поэтому typed proposal (understanding/questions,
`current_step`, `expected_action`, transition, plan snapshot) извлекается
внутренним decoder-ом из **того же** completion текущего task step, валидируется
domain task-пакетом и не меняет HTTP response. До реализации проверить fixture,
что выбранный internal representation не добавляет LLM call, endpoint или
самостоятельный протокол генерации плана. Это трактовка действующей спецификации,
не отдельный approval blocker; вопрос продукту возникает только при реальном
конфликте с ней.

## Неизменяемые контрактные правила

* Существующие `/healthz`, `/api/admin/logs`, `/api/profiles` и
  `/api/projects/...` сохраняют методы, status и безопасные error payloads.
  Добавление поля допускается только additive и после contract fixture.
* `X-Session-ID`, `X-Request-ID`, cookie/BFF ownership, session isolation,
  observability и redaction сохраняются. Raw credentials, prompts, user text,
  facts и profile text не попадают в обычные структурированные логи.
* Persisted актуальные версии v3/v4/v5 читаются и перезаписываются без потери
  подтверждённых данных. Malformed/unknown version — storage/restore error и
  сохранение исходного файла; **не** «legacy reset». Исключение — явный,
  version-marked historical reset, требуемый AC-MEM-12: он применяется только к
  установленной legacy-модели и тестируется отдельно, не к актуальным данным.
* Любая migration/reset сначала делает read-only inventory и backup исходного
  JSON, имеет dry-run/fixture, записывает новый файл лишь после полной
  валидации. Rollback — остановить процесс, восстановить backup, запустить
  предыдущую verified сборку; не запускать новый код поверх partially written
  state. `replace` остаётся атомарным на одном filesystem.

## Наблюдения и кандидаты spec-vs-code gaps

Это не часть behavior-preserving переноса. Наблюдение исходника ещё не всегда
доказывает defect: перед fix нужен controlled reproduction/contract fixture.
Каждый подтверждённый gap исправляется отдельным commit; нельзя закреплять
текущую ошибку characterization-тестом или ослаблять спецификацию ради зелёного
теста.

| ID | Факт в коде | Требуемый результат |
| --- | --- | --- |
| G-01 | В [`send`](../backend/internal/memory/store.go) title запускается только после успешной сохранённой пары. | AC-BAR-07: после первого принятого user input стартует ровно один независимый title-call, не ожидая main/extractor/save; status/итог durable. |
| G-02 | [`advanceTask`/`initializePlan`/`alignPlanWithTask`](../backend/internal/memory/store.go) жёстко продвигают этап и генерируют два шаблонных пункта; `clarificationConfirmed` ищет фразы. | AC-TSM-02/03/12/13: proposal процесса определяет вопросы, следующий шаг/переход и предметный plan; ответ на все заданные вопросы может завершить clarify; stage и plan независимы. |
| G-03 | [`TaskInput`](../backend/internal/memory/store.go) сначала commits pair/facts через `send`, затем task commit; при memory failure пара сохраняется. | AC-TSM-04/06/09/11/15: до подтверждённого атомарного task commit не показывать task output/transition/plan; определить и проверить границу с обычным AC-MEM-06, не смешивая контракт task step с обычной беседой. |
| G-04 | Pause ordering и restart in-flight не имеют controlled contract fixtures на оба порядка. | Это недостаток доказательств, а не подтверждённый defect: воспроизвести AC-TSM-05/06/15, затем решить, нужен ли fix. |
| G-05 | [`TaskInput`](../backend/internal/memory/store.go) генерирует server task client ID; [BFF](../frontend/src/lib/server/barista.ts) допускает только `text,candidate_task_id`. Lost response/manual retry не имеет request identity и может создать дубль. | AC-TSM-11: спроектировать минимальный additive operation/client-message ID через BFF/client/backend; не dedup по text. Старое body остаётся поддержанным до совместимого rollout. |
| G-06 | [`RenameProject`/`ClearGlobal`/`ClearProject`](../backend/internal/memory/store.go) меняют live state до `saveLocked`, без rollback при save error. | Storage failure не меняет подтверждённый state; построить copy-on-write candidate и публиковать только после durable save. |
| G-07 | [`Open`](../backend/internal/memory/store.go) трактует malformed/unknown version как legacy reset. | Не стирать актуальные/неизвестные данные; только распознанный historical version может пройти документированный AC-MEM-12 reset. |
| G-08 | [`extract`](../backend/internal/memory/store.go) декодирует nil values: `{}`, отсутствующий ключ и `null` не отвергаются достаточно явно. | AC-MEM-06/07: только object ровно с обоими ключами и массивами; отвергать top-level `null`, missing/null keys, extra keys, duplicates, blanks и trailing JSON. |

## Границы конкурентности, commit и cancellation

Ограничение этого плана — один процесс; mutex/lease не является distributed
lock. Внешние запросы никогда не держат state lock.

1. Под lock: проверить owner, project/chat/task, idempotency key и pending;
   собрать immutable input snapshot, записать/сохранить допустимый pending/lease
   marker и его generation. Release lock.
2. Без lock: main/task completion, затем extractor; каждый вызов получает
   derived context и не имеет автоматического retry. Title запускается отдельно
   от первого принятого input, с отдельным context/timeout и durable
   `pending→success|fallback`; его ошибка не меняет основной turn.
3. Под lock: повторно загрузить state и сопоставить owner, entity, operation ID
   и generation. Если deletion/pause/newer commit победили — отбросить late
   result. Собрать копию candidate, проверить domain invariants, одной записью
   сохранить JSON; только после успеха заменить in-memory published snapshot.
4. При cancellation/pause: сначала durable paused snapshot с последним
   подтверждённым step/plan, затем cancel in-flight context. Late LLM/extractor
   не имеет права коммитить. На startup любой durable in-flight marker
   нормализуется в paused/retryable состояние по AC-TSM-06, без частичного
   response.

Обычный chat turn сохраняет пару после main success: при extractor failure — с
прежними snapshots и memory error (AC-MEM-06); при pair save failure — ничего
не публикует. Task turn — более строгая ветка: output, transition, plan и
memory changes попадают в один candidate commit только после успешного memory
update. Это явное различие документировать в code/tests, а не обходить вызовом
обычного `Send`.

## Последовательность небольших изменений

Каждый этап — отдельный commit/PR boundary. Перед началом следующего этапа
выполнить его выходные проверки и сохранить fixtures. Если проверка не прошла,
остановиться на текущем этапе: не маскировать регрессию последующим переносом.

### 0. Зафиксировать baseline и воспроизвести gaps

**Вход:** чисто прочитанный active contract; исходники не меняются.

* Собрать HTTP contract fixtures для routes, disk JSON fixtures v3/v4/v5,
  malformed/unknown version и наблюдаемые journal payloads/redaction.
* Добавить controlled completion double с barrier/order log для main, extractor,
  title, task proposal/step, network/timeout/storage faults и cancel.
* Проверить observations G-01…G-08 controlled fixtures. Characterization тесты маркировать как
  `legacy-behavior`, не включать их в acceptance gate для bug-fix и удалить/
  переписать вместе с исправлением.
* Зафиксировать baseline `go test -race ./...`, `go vet ./...`; не выдавать это
  за spec compliance. Инвентаризировать active и legacy endpoints/tests/DTO.

**Выход:** доказательства gap, инвентарь, fixtures; пользовательское поведение
не менялось.

### 1. Механически вынести typed domain value objects и валидаторы

**Зависит от:** 0.

* Перенести без изменения поведения closed enums, копирование, title/profile/
  typed facts/plan validation в domain-пакеты; тесты должны сравнивать прежние
  DTO. Domain принимает уже типизированный `FactsSnapshot`, не raw JSON.
* Подтвердить отсутствие import cycle и запрет I/O в domain статической
  проверкой/код-review.

**Выход:** `memory/store.go` ещё фасад, но инварианты изолированы. Rollback —
revert одного переноса, fixtures не меняются.

### 1a. Отдельный bug-fix G-08: strict extractor boundary

**Зависит от:** 1.

* В `adapters/extractjson` (infrastructure boundary) декодировать raw response
  и различать object, missing, `null` и array; передавать в domain только
  валидный typed snapshot. Не добавлять endpoint/prompt/модельный вызов.
* Добавить acceptance fixtures для G-08: `{}`, top-level `null`, отсутствующий/
  `null` каждый массив, лишние keys, не-массив/не-строка, дубли/пустые элементы
  и trailing JSON; оба valid empty array очищают слой.

**Выход:** исправление G-08 отдельным commit, подтверждающим AC-MEM-06/07.

### 2. Механически ввести statejson repository и переходный фасад

**Зависит от:** 0–1.

* Вынести load/decode/version decision, clone/candidate, atomic save, close и
  restore в `adapters/statejson`; сохранить названия JSON-полей, sorting и
  timestamp semantics. До write утвердить через fixtures policy retention/reject
  для unknown fields/version, без изменения runtime поведения в этом commit.
* Оставить `memory.Store` временным фасадом с прежними exported method
  signatures; он делегирует repository/application, поэтому handler и main
  на этом этапе не меняются.

**Выход:** disk round-trip и restart fixtures проходят, active JSON совместим.
Recovery проверяется kill/reopen fixture; временный фасад сохранён.

### 2a. Отдельный bug-fix G-06: publish только после durable save

**Зависит от:** 2.

* Перевести `RenameProject`, `ClearGlobal`, `ClearProject` и затем каждый
  оставшийся mutator на copy-on-write `Commit`: disk save candidate успешен до
  swap published state. Проверить forced storage error и неизменность read API.

**Выход:** G-06 закрыт отдельным commit; storage остаётся application/port
error, не domain filesystem error.

### 2b. Отдельный bug-fix G-07: restore/version safety

**Зависит от:** 2.

* Сделать malformed/unknown version fail-closed с сохранением исходного файла.
  Только явно распознанная legacy-модель получает version-marked reset
  AC-MEM-12; v3/v4/v5 не считаются legacy.

**Выход:** G-07 закрыт отдельным commit с fixtures current/unknown/corrupt и
исторического reset.

### 3. Выделить workspace use cases и тонкий active HTTP handler

**Зависит от:** 2.

* Перенести CRUD/select/delete project/chat, profile и memory clear в
  `application/workspace`; handler получает маленькие endpoint-oriented
  interfaces и остаётся единственным местом HTTP mapping.
* Перенести общие `decode`, `method`, SnapshotLoader в нейтральные owned
  helpers/config только после поиска всех consumers; сохранить их contract
  tests. Не переносить старый handler целиком.
* Прогнать ownership/validation/storage and log-redaction fixtures.

**Выход:** active HTTP public behavior неизменён, `memory.Store` больше не
содержит CRUD/persistence. Rollback — сохранить фасад и старые route tests.

### 4. Механически перенести обычную conversation orchestration

**Зависит от:** 2–3.

* `application/conversation` собирает prompt, выполняет main/extractor и
  единый chat+memory commit через порты. `adapters/openai` инкапсулирует
  concrete `agent.Provider`/`llm` и сохраняет categories/traces.
* Сохранить текущие lock/pending/delete semantics и retry по существующему
  client message ID, переместив их без product change. Отдельно добавить
  controlled fixture lifecycle удаления chat/project во время in-flight и
  сопоставить с AC-MEM-09; если fixture покажет расхождение, завести отдельный
  bug-fix, а не включать его в mechanical commit.
* Механически сохранить AC-MEM-04/05/06/07 ordering; тест с delayed extractor
  доказывает, что response не наблюдаем до commit.

**Выход:** same HTTP/disk fixtures и controlled normal-chat сценарии проходят.

### 5. Отдельно исправить title lifecycle

**Зависит от:** 4; отдельный bug-fix commit для G-01.

* Определить «принятие первого user input» в domain state, durable title claim
  до main/extractor, затем стартовать ровно одну background title operation.
* Не позволять retry, second message, refresh/restart создать второй call;
  deletion отменяет/игнорирует late title. Проверить fallback, Unicode-60,
  plain text, independent response latency и persistence `title_status`.

**Выход:** AC-BAR-07 проходит controlled provider + restart fixture без
ослабления формулировки AC.

### 6. Выделить task domain и proposal-driven taskflow

**Зависит от:** 1–4; отдельные commits для G-02 и G-03.

* Заменить phrase heuristics и stage/template plan на валидируемый результат
  текущего task-agent process: understanding/questions, `current_step`,
  `expected_action`, proposed transition и plan snapshot. Это не новый SDK или
  пользовательский API: использовать существующий completion transport и
  strict internal decoder/schema/validator из раздела «Источник решения».
* Domain validator разрешает четыре stage, запрещает skip/backward кроме
  `user_feedback→execution`, сохраняет completed id/title, допускает empty plan
  только в clarify и проверяет ровно один current когда он обязателен.
* Task use case формирует единый candidate: task output + pair + memory result
  + plan + transition. При provider/extractor/storage failure оставляет
  последний confirmed task snapshot. Следующий step не запускает сам.

**Выход:** AC-TSM-01…04, 07, 12…14 проверены controlled provider/persistence;
plan содержит предметный пункт («Узнать информацию о кофемолке»), не stage.

### 7. Идемпотентность, pause race и restart

**Зависит от:** 6; отдельные commits G-04 и G-05.

* Принять минимальное transport решение для operation ID: additive
  `client_message_id` у task input (или эквивалентный explicit operation ID),
  который BFF allowlist, client и backend передают/сохраняют. Старый body
  читается совместимо; клиент генерирует ID один раз до запроса и повторяет
  после lost response. Нельзя dedup по text или менять body молча.
* Добавить task-specific durable lease/generation; cancellation ordering и
  restart normalisation реализовать по одному commit semantics выше.
* Проверить simultaneous input/pause, late success, lost response, ordinary
  failure retry и restart между каждым внешним шагом.

**Выход:** AC-TSM-05/06/08/09/10/11/15 проходят browser, API и persistence
fixtures; у намеренной pause нет error/retry alert.

### 8. Cutover active wiring и retirement legacy — только после инвентаризации

**Зависит от:** 0–7 и полного active acceptance gate.

* Переключить [`cmd/api-server/main.go`](../backend/cmd/api-server/main.go) на
  concrete новые use cases/adapters и active `httpapi`; удалить переходный
  `memory.Store` только после подтверждения отсутствия всех production и test
  consumers. Проверить import-граф из измеримых целей и общий repository
  contract suite для fake и `statejson` adapter.
* Составить таблицу каждого export/helper/test старых `httpapi.Handler`,
  `session.Store`, `agent.Conversation`: active consumer, эквивалент в новом
  контуре, полезный контрактный тест, решение keep/move/delete.
* Сначала перенести полезные тесты к активному контракту. Только когда `rg`
  подтверждает отсутствие production consumer и соответствующие fixtures
  проходят, удалить legacy code в отдельном PR.
* Deprecated specs не являются источником поведения и не дают права сохранять
  устаревшие endpoints.

**Выход:** `main` реально использует новую композицию, нет dead active-looking
code и нет потери полезного теста; deletion обратим Git revert, не затрагивает
persisted state.

## Матрица требований и проверок

| Спецификация / точные AC | Обязательная проверка после соответствующего этапа |
| --- | --- |
| AC-BAR-01, AC-BAR-02 | Browser + API: два чата одного проекта изолированы; success создаёт одну pair, pending блокирует повтор. |
| AC-BAR-03, AC-BAR-04 | Controlled provider failure/manual retry; persistence restart и другая cookie. |
| AC-BAR-05, AC-BAR-06 | Browser accessibility 390/1440, keyboard Enter/Shift+Enter/pending. |
| AC-BAR-07 | Controlled title ordering/count/fallback/Unicode + persistence restart (этап 5). |
| AC-BAR-08 | PATCH validation/storage-failure rollback + persistence/browser. |
| AC-BAR-09 | `docker compose config` с временным dotenv и isolated live container smoke: env mount, health dependency, host exposure, named volume recreation. |
| AC-BAR-10, AC-BAR-11, AC-BAR-12 | Unit + browser clipboard; admin known/deleted/unknown chat; rendered escaped newline/tab without mutation payload. |
| AC-MEM-01 | API/browser project select, empty/new chat и isolation. |
| AC-MEM-02, AC-MEM-03 | Controlled prompt inspection at N=3; exact profile > chat > project > global priority. |
| AC-MEM-04, AC-MEM-05 | Controlled main/extractor count/order and equipment/beans-only replacement fixture. |
| AC-MEM-06, AC-MEM-07 | Fault injection keeps pair/old snapshots; parser rejects `{}`, top-level `null`, missing/null each key, extra keys, non-array/non-string, blank/duplicate elements and trailing JSON; valid empty arrays clear layer. |
| AC-MEM-08, AC-MEM-09 | Browser panel/status/confirmed clear; delete chat vs project/global semantics and cancelled inflight request. |
| AC-MEM-10, AC-MEM-11 | Restart/cookie isolation, persisted/log inspection and 390/1440 accessibility. |
| AC-MEM-12 | Versioned legacy-reset fixture удаляет только historical chats/state; v3/v4/v5 and unknown/corrupt fixtures are preserved/fail closed as applicable. |
| AC-PER-01, AC-PER-02 | Browser/API new defaults; custom validation including Unicode-61, trim/case uniqueness, no state change on failure. |
| AC-PER-03, AC-PER-04 | Controlled prompt/response confirms all three active-profile fields and required precedence/safety. |
| AC-PER-05, AC-PER-06 | API/browser confirmation/storage rollback/default fallback; restart/cookie and persistence/log privacy. |
| AC-PER-07 | Browser keyboard/screen-reader/390/1440 long unbounded fields. |
| AC-TSM-01 | Controlled classifier/task router: one/no/two matches, candidates and no premature call. |
| AC-TSM-02, AC-TSM-03 | State-machine fixture: clarify questions, substantive answers, four stages, legal transitions only. |
| AC-TSM-04, AC-TSM-16 | Controlled multi-call + persistence: один step на LLM-вызов, автоматическая последовательность agent-шагов, лимит восьми вызовов и единый atomic memory/task commit. |
| AC-TSM-05, AC-TSM-06 | Blocking provider/fault injection/restart in every stage: cancellation, no partial state, resume same step. |
| AC-TSM-07 | Controlled positive/non-positive feedback; done rejects pause/resume. |
| AC-TSM-08 | Persistence/cookie isolation plus browser a11y at both breakpoints. |
| AC-TSM-09, AC-TSM-10 | Browser timing with delayed memory/server: no early output/transition; pending visible once within 100 ms; controls within 1 s after BFF pause/resume. |
| AC-TSM-11 | Lost-response/network/timeout/server failure fixture uses same operation ID and creates no duplicate. |
| AC-TSM-12, AC-TSM-13, AC-TSM-14 | Controlled proposal + validation/persistence plan fixture, including coffee-grinder item, completed stability and screen-reader panel states. |
| AC-TSM-15 | Controlled order race: pause-before-commit vs commit-before-pause, one output at most and neutral paused UI. |

Новые TSM browser files должны быть явно включены в Playwright config; текущий
`testMatch` не является достаточным gate. API tests не подменяют browser timing
и accessibility проверки, а browser tests не подменяют restart/fault fixtures.

## Общий acceptance gate и smoke

После каждого этапа: `gofmt`, targeted package tests, затем `go test -race ./...`
и `go vet ./...`. Перед передачей пользователю каждого завершённого инкремента
дополнительно выполнить frontend `lint`, `typecheck`, `test`, production `build`
и отдельный smoke в запущенном приложении. Итоговый инкремент проходит полный
smoke: create/select project and chats, normal send+extractor, title, profile,
task step/pause/resume/retry, restart восстановления и `/admin`.

Smoke запускается в изолированном Compose project с отдельными temporary config,
dotenv и named volume: он не использует и не останавливает основной пользовательский
stack, а cleanup адресуется только этому project/volume. Controlled non-secret
provider должен быть контейнером в той же тестовой Compose сети (service DNS),
либо иметь корректно доступный container address; fixture на `127.0.0.1` хоста
не считается достаточной. Проверить health, логи без secrets и persistence
изолированного volume; `down -v` допустим только для созданного smoke-project.

Воспроизводимый smoke добавлен в `frontend/e2e/compose-smoke.sh` и запускается
против актуальных Docker images. Фактический результат итогового прогона,
contract suite и browser gate зафиксирован в [отчёте](backend-refactoring-progress.md).
