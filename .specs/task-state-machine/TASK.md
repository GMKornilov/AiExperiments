# Контролируемые переходы задач — неочередное техническое задание

## Цель

Доставить поведение из [SPEC.md](SPEC.md): задача AI-бариста проходит только
разрешённый жизненный цикл, не перескакивает этапы, не выходит из уточнения без
явного подтверждения оборудования и не запрашивает feedback до подтверждённой
валидации результата.

## Background

Текущие stage и lifecycle status уже персистируются, однако текущий contract
допускает proposal с переходом через несколько stage. Это противоречит заданию
о контролируемом жизненном цикле. Данная задача меняет поведение переходов, а
не набор stage: `clarify_input`, `research_input_data`, `execution`,
`user_feedback` остаются единственными этапами.

## Detailed technical assignment

1. Обновить доменный контракт задачи в `backend/internal/domain/model`.
   Закрыть допустимые stage/status значения и проверять граф переходов от
   подтверждённого snapshot: только соседние прямые stage-переходы, а из
   `user_feedback` — только возврат в один из трёх предшествующих stage при
   неположительном feedback. Равный stage не считать transition: это изменение
   плана или следующего шага. Положительный feedback меняет только status
   `active → done`; done терминален.
2. В contract proposal и его строгом decoder-е обеспечить только данные для
   проверки перехода и работы по плану. `validation_result` и его summary не
   входят в LLM proposal: неизвестное поле этого имени отклоняется decoder-ом.
   Сервер, а не client, LLM-флаг или proposal, должен доказуемо связывать выход
   из `clarify_input` с явным подтверждением пользователем оборудования из
   текущего accepted input. Для информационной задачи агент явно предлагает
   значение «оборудование не требуется», а сервер допускает переход только
   после пользовательского подтверждения этого значения; отсутствие упоминания
   оборудования не достаточно. Прямой `clarify_input → execution` и любые jumps
   отклоняются.
3. Добавить публичное персистируемое поле `validation_result` в task state,
   read/API DTO и state-миграцию в `backend/internal/adapters/statejson`.
   Поддержать объект со `status` `not_validated`/`passed` и безопасным `summary`
   только у `passed`; server детерминированно создаёт `passed` и summary после
   успешного gate. Поднять версию disk schema. Legacy state до feedback
   восстанавливать в `not_validated`; legacy `user_feedback`/`done` — в
   `not_validated` с `legacy_unvalidated=true`, без backfill `passed`. Legacy
   done остаётся terminal; первый input legacy feedback, включая положительный,
   возвращает её в execution, активирует validation plan item и требует новый
   gate.
4. В `backend/internal/application/invariant` расширить обязательный
   deterministic transition/final-validation gate. Переход
   `execution → user_feedback` допускается, только когда plan completed и
   current candidate прошёл действующие mandatory semantic checks. Gate не
   должен добавлять provider-вызов сверх существующего pipeline. Invalid jump,
   неподтверждённое оборудование или неуспех gate передаются в repair lifecycle;
   после исчерпания repairs не меняется snapshot и возвращается безопасное
   следующее действие.
5. В `backend/internal/application/taskflow` сохранить validation result и
   весь конечный snapshot в существующем atomic commit. Pause, cancellation,
   technical error, validation failure и поздний ответ не должны публиковать
   частичный output, plan, transition или validation result. Resume продолжает
   ровно последний подтверждённый шаг для каждого stage, включая execution до
   или во время gate.
6. Расширить backend HTTP contract и BFF routes для task input, pause и resume,
   чтобы подтверждённые чтения и мутации возвращали validation result без
   внутренних reasons, prompts или provider payload. Обновить frontend types,
   chat client, workspace и task panel: показать успешный результат в
   `user_feedback`/`done`; не выдавать `not_validated` за ошибку; сохранить
   keyboard/screen-reader поведение и отсутствие horizontal overflow на 390 px
   и 1440 px.
7. Добавить structured logging каждой попытки transition и validation gate:
   correlation ID, result/category, вид stage/status transition и duration.
   Логи не содержат secrets, prompt, raw user/assistant text или validation
   summary.

## Затронутые модули и тестовые слои

- Domain и unit: `backend/internal/domain/model` — enum, proposal, graph,
  plan/validation invariants и table-driven negative cases.
- Decode, persistence и migration: `backend/internal/adapters/extractjson` и
  `backend/internal/adapters/statejson` — строгий wire contract, JSON DTO,
  восстановление старого state и round-trip `validation_result`.
- Orchestration и invariants: `backend/internal/application/taskflow` и
  `backend/internal/application/invariant` — repair, atomic commit, pause/
  resume, provider-call budget и safe outcomes.
- HTTP/API: `backend/internal/httpapi`, а также Next.js BFF routes в
  `frontend/src/app/api/projects/[projectId]/chats/[chatId]/tasks` — read and
  mutation contracts without private diagnostic data.
- UI: `frontend/src/features/barista/model/types.ts`, chat client, workspace,
  task panel and its styles.
- Automated verification: Go unit/integration/API/persistence/log tests,
  frontend unit/typecheck/lint/build and `frontend/e2e/tasks.spec.mjs`.

## Acceptance criteria

Все **AC-TSM-01—AC-TSM-18** из [SPEC.md](SPEC.md) обязательны. Минимальный
набор доказательств дополнительно включает:

1. Таблицу допустимых и недопустимых переходов, включая
   `research_input_data → user_feedback`, `clarify_input → execution`,
   `user_feedback → user_feedback`, переход из done и равный stage как
   изменение плана без transition.
2. Controlled-provider trace, где LLM предлагает каждый недопустимый переход,
   а также ложное подтверждение оборудования и feedback без validation. Для
   каждого случая доказаны неизменность snapshot, ограниченный repair lifecycle
   и безопасный ответ после исчерпания repair.
3. Позитивный trace `clarify → research → execution → user_feedback → done`:
   переход из clarify следует только после явного подтверждения оборудования,
   а переход в feedback содержит persisted `validation_result=passed`.
   Отдельный controlled-provider trace информационной задачи доказывает, что
   явное «оборудование не требуется» плюс подтверждение пользователя разрешают
   `clarify → research`, а отсутствие такого подтверждения — нет.
4. Три feedback trace: возврат в clarify, research и execution с reset
   validation result; затем успешное повторное прохождение validation gate.
5. Pause/resume в каждом stage и в execution до/во время validation gate,
   включая refresh/restart и failed Resume. Последний подтверждённый snapshot
   и тот же повторяемый шаг доказаны storage spy и browser-сценарием.
6. API/BFF/UI proof, что validation result возвращается только владельцу
   browser-сеанса, отображается без внутренней причины и не создаёт horizontal
   overflow или keyboard regression на 390 px и 1440 px.
7. Полный регрессионный запуск Go, frontend lint/typecheck/unit/E2E и
   production build. Перед передачей реализации обязателен актуальный Docker
   Compose smoke test исходного пользовательского сценария и новых переходов.
8. Migration/restore trace для legacy state до feedback, active/paused legacy
   feedback и legacy done: нет backfill `passed`; UI помечает legacy результат,
   done остаётся terminal, а первый новый input feedback задачи повторно
   проводит результат через execution и gate.

Контролируемые provider-тесты не требуют реальных LLM-вызовов. Mandatory live
LLM acceptance smoke из `invariants` остаётся не более 12 вызовов validation
модели; суммарный real-LLM прогон до 100 вызовов разрешён правилами проекта,
свыше 100 требует отдельного согласия пользователя.

## Out of scope

- Новые stage, ручное редактирование или удаление задачи/плана.
- Новые deep links, межсеансовая синхронизация, параллельное выполнение шагов.
- Отдельный LLM-вызов, отдельная UI-панель или новая пользовательская настройка
  для validation gate.
- Изменение кофейных правил, памяти, профилей или семантики удаления вне
  необходимой интеграции с existing invariant pipeline.

## Процедурная граница

У задачи нет queue ID: `.tasks/QUEUE` отсутствует, поэтому workflow
`working-with-specs` не активирован. Этот co-located handoff следует текущей
проектной конвенции и не заменяет queue tracking. Включение queue tracking или
выбор иной системы трекинга остаётся решением владельца проекта; ключ очереди
не задан и не предполагается этим документом.

Подготовка этого задания не начинает реализацию и не изменяет исходники, тесты,
конфигурацию, runtime-данные или Git-ветку.
