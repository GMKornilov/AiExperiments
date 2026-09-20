# Рефакторинг backend: результат

Дата: 2026-09-18. Работа выполнена в главной сессии по запросу пользователя.
Очереди `.tasks/QUEUE` нет; спецификация архитектуры находится в
[backend-architecture](../.specs/backend-architecture/SPEC.md).
Исходный `go test -race ./...` проходил до изменений.

## Реализация

`cmd/api-server` собирает отдельные workspace, conversation и taskflow use cases.
Domain не содержит JSON tags или I/O; HTTP и disk DTO разделены.
Application зависит от портов, concrete wiring находится в `main`.
Монолитный `memory.Store` удалён. Общий contract suite выполняется с fake и
реальным JSON repository, включая fault injection и cancellation.

| Этап плана | Результат |
| --- | --- |
| 0–2: контракты, domain, repository | Typed state/errors/инварианты; strict versioned DTO; atomic replace; copy-on-write commit; ownership и restore fixtures |
| 3–4: workspace, HTTP, conversation | Узкие endpoint interfaces; отдельный CRUD; обычная беседа сохраняет pair при extractor failure и прежние facts |
| 5: title | Durable claim первого принятого ввода; независимый вызов, fallback/Unicode/restart; повторного вызова нет |
| 6: taskflow | Typed proposal из одного task completion; предметный план и допустимые переходы; output/pair/facts/task принимаются одним commit |
| 7: отмена и повторы | Durable operation ID/generation; pause побеждает late response; ручной retry с тем же ID; восстановление pending в paused |
| 8: cutover | Новый composition root; в графе API нет `internal/memory` и `internal/session`; старый HTTP handler компилируется только в legacy tests |

Полное удаление `session.Store` и `agent.Conversation` оставлено границе
отдельного retirement PR, предписанной этапом 8. Они нужны сохранённым
историческим regression tests и не вызываются активным API. Активные
`agent.Provider` и snapshots остаются основой OpenAI adapter. Решения по
типам, helpers и тестам перечислены в [инвентаре](backend-refactoring-inventory.md).
Изменения подготовлены в рабочем дереве; отдельных commits/PR не создавалось.

## Исправления G-01…G-08

| Gap | Реализация и проверка |
| --- | --- |
| G-01 | Title запускается до завершения main/extractor; тест с заблокированным main и последующим failure/retry |
| G-02 | Переход и план задаёт proposal; четыре стадии, same-stage, substantive clarification, feedback, стабильность completed пунктов |
| G-03 | Task failure не применяет правило частичного успеха обычной беседы; delayed extractor и failed final Save не публикуют output |
| G-04 | Оба порядка pause/commit проверены barriers; provider, игнорирующий cancellation, не может применить late result |
| G-05 | Additive `client_message_id` проходит client/BFF/API; lost input/Resume response не создаёт повторную пару |
| G-06 | Все mutations проходят через copy-on-write candidate; failed Save сохраняет ранее подтверждённое состояние |
| G-07 | Unknown/malformed current JSON блокирует restore без перезаписи; historical reset только распознанных v1/v2 с backup |
| G-08 | Strict facts decoder отвергает missing/null/extra/duplicate keys, неверные типы, blank/duplicate facts и trailing JSON |

Дополнительно воспроизведён отказ при повторном Resume после завершившегося
`done`: сохранённая операция теперь воспроизводит результат до проверки запрета
новой работы. Регрессионный тест сначала падал на fake и JSON, после исправления
проходит; новая операция для `done` по-прежнему отклоняется.

Docker browser smoke также выявил гонку первого открытия: одновременные
`projects`/`profiles` requests без cookie создавали разные сессии, поздний
`Set-Cookie` делал новый проект недоступным (`not_found`). Отдельный browser
тест воспроизвёл отсутствие cookie в profile request. Начальная загрузка теперь
последовательная, mutations доступны после её завершения.

## Карта проверок активных требований

| Требования | Проверки |
| --- | --- |
| BAR-01…04; MEM-01,10 | Active HTTP tests, ownership/copy contracts, failed main/manual retry, process restart, browser cookie isolation |
| BAR-05,06; MEM-08,09,11 | Browser memory/tasks на 390/1440, Enter/Shift+Enter, keyboard controls, clear/delete, delayed extractor; delete-inflight contract |
| BAR-07 | Independent title, delayed title, invalid/empty/long/Markdown/provider failure fallback, Unicode-60, pending restore без повторного вызова |
| BAR-08 | Rename API/browser/reload, validation и failed Save rollback |
| BAR-09 | Изолированный root Compose: runtime dotenv, private API, health dependency, named volume и пересоздание контейнера |
| BAR-10…12 | Clipboard с клавиатуры и admin known/unknown/deleted chat; journal/payload/redaction unit tests и browser component tests |
| MEM-02…05; PER-03,04 | `TestPromptWindowProfileAndExtractorPrivacy`: N=3, порядок profile/chat/project/global, все поля профиля, отдельный extractor input; controlled memory replacement |
| MEM-06,07 | Ordinary-vs-task failure contract, delayed extractor, strict facts shape matrix и valid empty arrays |
| MEM-12 | Current v3/v4/v5 round-trip; unknown/corrupt source unchanged; v1/v2 legacy index и directory backup |
| PER-01,02,05,06 | Profile API/browser defaults, Unicode/trim/case validation, failed deletion rollback, active fallback и restart |
| PER-07 | Длинные поля без горизонтального overflow на 390/1440, labels/roles, keyboard open/create/close |
| TSM-01…04 | Controlled router/candidate selection без преждевременного completion; proposal и один atomic step; same-stage и предметный grinder plan |
| TSM-05…08,15 | Pause-before/after commit, late result, main/extractor interruption на каждой стадии, Resume, positive/negative feedback, done guards, restart/cookie |
| TSM-09…11 | Browser delayed main/memory, измерение pending ≤100 ms, Resume после pause ≤1 s, lost-response manual retries с тем же ID |
| TSM-12…14 | Domain plan validation/completed stability; browser clarify/active/paused/feedback/done, `aria-current`, keyboard и persistence |

## Автономное продолжение agent-шагов (2026-09-19)

После первоначального рефакторинга изменён продуктовый контракт taskflow.
Валидный proposal с `expected_action: "agent: ..."` теперь запускает следующий
`task_step` автоматически в той же попытке. Новый вызов получает обновлённое
состояние задачи, план и предыдущие outputs как data. Цепочка заканчивается на
`user: ...`, `done`, ошибке либо лимите восьми вызовов. Memory extractor
вызывается один раз после цепочки; совокупный output, конечный task snapshot,
план и facts по-прежнему сохраняются одним commit. Пауза отбрасывает всю
неподтверждённую цепочку, Resume повторяет её с сохранённого пользовательского
ввода. BFF timeout для task input/Resume увеличен до 300 секунд.

Добавлены contract fixtures для трёх автономных task-вызовов с передачей двух
предыдущих outputs, одного memory update и единого commit, а также endless-agent
fixture: восьмой agent proposal возвращает `invalid_response` без memory call и
частичного состояния. Browser fixture проверяет pause во втором автономном
вызове и полное повторение цепочки после Resume.

Основные новые проверки:
[application contracts](../backend/internal/application/acceptance/contracts_test.go),
[import graph](../backend/internal/application/acceptance/architecture_test.go),
[repository](../backend/internal/adapters/statejson/repository_test.go),
[task browser suite](../frontend/e2e/tasks.spec.mjs).
Проверки LLM используют контролируемый fixture: они доказывают orchestration,
prompt/response contracts и UI, но не оценивают качество реальной модели.
Проверки доступности покрывают DOM roles/labels, клавиатуру и layout;
ручной прогон отдельного screen reader не выполнялся.

## Проверка Pause/Resume по реальному сбою (2026-09-20)

По структурированным логам пользовательского сценария Pause был подтверждён за
4 ms, а активный DeepSeek `task_step` отменён с `cancelled` через 5,354 s.
Resume действительно отправил два новых `task_step`: 4,402 s и 46,383 s.
Второй ответ содержал готовый рецепт и валидный конечный план, но перешёл
`research_input_data → user_feedback`; прежняя проверка разрешала только один
этап за proposal и отклонила ответ после 50,8 s.

Domain contract теперь принимает валидное продвижение через несколько этапов,
но по-прежнему запрещает обратный переход, кроме корректирующего
`user_feedback → execution`. Prompt явно описывает это правило. В application
contract добавлен полный сценарий cancel → paused snapshot → Resume → два
автономных вызова → один memory update → один commit. Browser fixture повторяет
форму реального ответа DeepSeek во втором автономном вызове и проверяет, что
ошибка Resume сохраняет `paused` после refresh, а следующий Resume завершается
успешно. HTTP-наблюдаемость сохраняет точную категорию `invalid_response`, а не
обобщает её до `provider`.

После исправления выполнен отдельный live-прогон через настроенный DeepSeek API
в основном stack с временными project/chat. Pause подтвердился за 5 ms и
отменил активный `task_step` через 1,007 s с категорией `cancelled`; отменённый
результат не был сохранён. Resume выполнил новый `task_step` за 9,836 s и
memory extractor за 6,511 s, завершился успешно за 16,362 s, добавил ровно одну
пару сообщений и перевёл задачу из `clarify_input` в `execution`. Временный
project удалён. С учётом title и memory extractor прогон сделал шесть реальных
LLM-вызовов.

## Итоговый gate

- `gofmt`, `git diff --check` — пройдены.
- `GOCACHE=$PWD/.gocache go test -race ./...` — пройден.
- `GOCACHE=$PWD/.gocache go vet ./...` — пройден.
- Frontend `npm run lint`, `npm run typecheck`, `npm test` — пройдены, 57 tests.
- Production `next build` в актуальном Docker image — пройден.
- Browser suite — 23 tests пройдены, включая pause во втором автономном
  LLM-вызове, forward-переход из реального DeepSeek-сценария и regression
  failed Resume после refresh; suite выполнился за 48.0 s.
- Финальный воспроизводимый Compose smoke после последнего исправления — пройден,
  exit code 0: browser/API, final Resume replay, recreation, exact snapshot,
  cookie isolation, SIGKILL/Resume и logs/state/image redaction.

Запуск из корня: `BARISTA_SMOKE_PORT=13031 ./frontend/e2e/compose-smoke.sh`.
Скрипт создаёт уникальный Compose project, временные config/dotenv, provider
в той же Docker-сети и отдельный named volume. Проверяет browser/API,
точное восстановление после recreation, SIGKILL in-flight и Resume без
повторного title; проверяет redaction логов, persisted state и image environment.
Cleanup адресуется только созданному project/volume; пользовательский stack
не изменяется. Артефакты остаются во временном каталоге, путь печатает скрипт.
Финальный project: `barista-refactor-smoke-48048`; после проверки он и его
volume удалены. Артефакты: `${TMPDIR}/barista-refactor-smoke.mYw1fN`.

Policy storage: v3 нормализуется в v4, v5 сохраняет версию; неизвестные поля и
версии отвергаются до записи. Перед recovery/normalization сохраняется точный
исходный JSON. Rollback: остановить процесс, восстановить соответствующий backup
и запустить прежнюю сборку. Реальная пользовательская история не мигрировалась;
restore/reset проверки выполнялись на отдельных fixtures.
